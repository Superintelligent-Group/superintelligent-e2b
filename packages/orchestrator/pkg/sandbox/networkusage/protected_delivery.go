package networkusage

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/e2b-dev/infra/packages/shared/pkg/storage"
)

// WorkloadBinding is host-supplied context, not a provider allocation or billing
// identity. TeamID is expressly best-effort metadata in Factory.RuntimeMetadata.
type WorkloadBinding struct {
	SandboxID, ExecutionID, TemplateID, BuildID, TeamID, LifecycleID string
	ProducerSHA256, ProducerIncarnation                              string
}

func (w WorkloadBinding) validate(sandbox string) error {
	for _, value := range []string{w.SandboxID, w.ExecutionID, w.TemplateID, w.BuildID, w.TeamID, w.LifecycleID, w.ProducerIncarnation} {
		if len(value) > 256 || !utf8.ValidString(value) {
			return errors.New("invalid workload context")
		}
	}
	if w.SandboxID != sandbox || w.ExecutionID == "" || w.LifecycleID == "" || len(w.ProducerSHA256) != 64 {
		return errors.New("missing workload context")
	}
	_, err := hex.DecodeString(w.ProducerSHA256)
	return err
}

// All values are explicit operator configuration; zero values enable nothing.
type ProtectedDeliveryConfig struct {
	Closing                                         ClosingOptions
	Custody                                         storage.ProtectedCustodyConfig
	MaxInventoryBytes                               int64
	MaxInventoryEntries, IntervalSeconds, PassLimit int
}

func ParseProtectedDeliveryConfig(raw string) (ProtectedDeliveryConfig, error) {
	var config ProtectedDeliveryConfig
	if len(raw) > 8192 {
		return config, errors.New("oversized protected delivery configuration")
	}
	if err := checkJSON([]byte(raw)); err != nil {
		return config, err
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return config, err
	}
	return config, config.Validate()
}

func (c ProtectedDeliveryConfig) Validate() error {
	if err := c.Closing.validate(); err != nil {
		return err
	}
	if err := c.Custody.Validate(); err != nil {
		return err
	}
	if c.Closing.enabled() && int64(c.Closing.PartBytes) > c.Custody.Destination.MaxObjectBytes {
		return errors.New("closing control limit exceeds protected object limit")
	}
	if c.MaxInventoryBytes < 8192 || c.MaxInventoryEntries < 1 || c.MaxInventoryEntries > 100000 || c.IntervalSeconds < 1 || c.IntervalSeconds > 3600 || c.PassLimit < 1 || c.PassLimit > 100 {
		return errors.New("explicit bounded delivery and inventory budgets required")
	}
	return nil
}

type protectedStore interface {
	storage.ImmutableCustodyStore
	Destination() storage.CustodyDestination
	VerifyVersion(context.Context, storage.CustodyClaim, string) (storage.CustodyReceipt, error)
}

// Retryability belongs to the remote operation, never merely to an error type:
// local fsync can also return ETIMEDOUT (a net.Error) after receipt rename.
type remoteDeliveryError struct{ error }

func (e remoteDeliveryError) Unwrap() error { return e.error }

type custodyBinding struct {
	Schema      int
	Config      ProtectedDeliveryConfig
	Destination storage.CustodyDestination
}

// CustodyInventory is immutable membership for SUP-914, including incomplete or
// unidentified segments. Never infer lifecycle order from randomized filenames.
type CustodyInventory struct {
	SummaryVersion              int
	Baseline, Terminal          *CorrelationReference
	Schema                      int
	Receipt                     storage.CustodyReceipt
	JournalIncarnation          string
	Workload                    *WorkloadBinding
	FirstSequence, LastSequence uint64
	Incomplete, Unidentified    bool
}
type protectedDelivery struct {
	closing       *closingLedger
	syncDirectory func() error // injectable directory durability boundary
	service       *Service
	store         protectedStore
	config        ProtectedDeliveryConfig
	dir           string
	used          int64
	count         int
	stop, done    chan struct{}
	mu            sync.Mutex
	lastError     error
}

func OpenProtectedService(ctx context.Context, dir string, options SpoolOptions, config ProtectedDeliveryConfig) (*Service, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.Custody.Destination.MaxObjectBytes < options.SegmentBytes {
		return nil, errors.New("custody object limit must hold a raw segment")
	}
	store, err := storage.NewProtectedAWSCustody(ctx, config.Custody)
	if err != nil {
		return nil, err
	}
	return openProtectedService(dir, options, config, store)
}
func openProtectedService(dir string, options SpoolOptions, config ProtectedDeliveryConfig, store protectedStore) (*Service, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	spool, err := openSpool(dir, options, true)
	if err != nil {
		return nil, err
	}
	s := serviceWithSpool(spool, options)
	d := &protectedDelivery{service: s, store: store, config: config, dir: filepath.Join(dir, ".custody"), stop: make(chan struct{}), done: make(chan struct{})}
	if err = d.recover(); err != nil {
		return nil, errors.Join(err, spool.Close())
	}
	if config.Closing.enabled() {
		d.closing, err = openClosingLedger(d)
		if err != nil {
			return nil, errors.Join(err, spool.Close())
		}
		if err = d.closing.recoverOwners(); err != nil {
			return nil, errors.Join(err, spool.Close())
		}
	}
	s.delivery = d
	go d.run()
	return s, nil
}

func (d *protectedDelivery) recover() error {
	// Reject legacy backlog before creating a protected-mode marker; an
	// unsuccessful activation must not prevent its legacy owner from reopening.
	if _, err := os.Lstat(d.dir); errors.Is(err, os.ErrNotExist) && d.service.spool.count != 0 {
		return errors.New("cannot adopt unbound evidence into protected producer namespace")
	}
	if err := os.Mkdir(d.dir, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(d.dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.Join(errors.New("invalid custody control directory"), err)
	}
	if err = d.service.spool.syncDir(); err != nil {
		return err
	}
	f, err := os.Open(d.dir)
	if err != nil {
		return err
	}
	defer f.Close()
	entries, err := f.ReadDir(d.config.MaxInventoryEntries + 4)
	if err != nil && err != io.EOF {
		return err
	}
	allowance := 2
	if d.config.Closing.enabled() {
		allowance++
	}
	if len(entries) > d.config.MaxInventoryEntries+allowance {
		return ErrSpoolBudget
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == "closing" && d.config.Closing.enabled() {
			continue
		}
		base := strings.TrimSuffix(name, ".tmp")
		if base != "binding.json" && !inventoryName(base) {
			return errors.New("unexpected custody control file")
		}
		data, err := d.readControl(name)
		if err != nil {
			return err
		}
		if strings.HasSuffix(name, ".tmp") {
			// A temporary file cannot authorize raw reclaim. The unchanged raw
			// candidate will retry upload/verification under its stable key.
			if err = os.Remove(filepath.Join(d.dir, name)); err != nil {
				return err
			}
			continue
		}
		d.used += int64(len(data))
		d.count++
		if d.used > d.config.MaxInventoryBytes || d.count > d.config.MaxInventoryEntries+1 {
			return ErrSpoolBudget
		}
		if name != "binding.json" {
			var inv CustodyInventory
			if err = json.Unmarshal(data, &inv); err != nil {
				return err
			}
			if err = d.validateInventory(name, inv); err != nil {
				return err
			}
		}
	}
	if err = d.syncControlDir(); err != nil {
		return err
	}
	want := custodyBinding{Schema: 1, Config: d.config, Destination: d.store.Destination()}
	data, err := d.readControl("binding.json")
	if errors.Is(err, os.ErrNotExist) {
		if d.service.spool.count != 0 || d.count != 0 {
			return errors.New("cannot adopt unbound evidence into protected producer namespace")
		}
		return d.persist("binding.json", want)
	}
	if err != nil {
		return err
	}
	var got custodyBinding
	if err = json.Unmarshal(data, &got); err != nil {
		return err
	}
	if got != want {
		return errors.New("protected spool binding changed; explicit migration required")
	}
	return nil
}
func inventoryName(name string) bool {
	return strings.HasSuffix(name, ".receipt") && segmentName(strings.TrimSuffix(name, ".receipt")) && !strings.Contains(name, ".active")
}
func (d *protectedDelivery) readControl(name string) ([]byte, error) {
	f, err := openSpoolControl(filepath.Join(d.dir, name), os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 8193))
	if len(data) > 8192 {
		return nil, errors.New("oversized custody control record")
	}
	return data, err
}
func (d *protectedDelivery) syncControlDir() error {
	if d.syncDirectory != nil {
		return d.syncDirectory()
	}
	f, err := os.Open(d.dir)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}
func (d *protectedDelivery) persist(name string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(data) > 8192 || int64(len(data)) > d.config.MaxInventoryBytes-d.used || d.count >= d.config.MaxInventoryEntries+1 {
		return ErrSpoolBudget
	}
	tmp := filepath.Join(d.dir, name+".tmp")
	f, err := openSpoolControl(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return err
	}
	n, writeErr := f.Write(data)
	if writeErr == nil && n != len(data) {
		writeErr = io.ErrShortWrite
	}
	err = errors.Join(writeErr, f.Sync(), f.Close())
	if err != nil {
		return err
	}
	if err = os.Rename(tmp, filepath.Join(d.dir, name)); err != nil {
		return err
	}
	if err = d.syncControlDir(); err != nil {
		return err
	}
	d.used += int64(len(data))
	d.count++
	return nil
}
func (d *protectedDelivery) validateInventory(name string, inv CustodyInventory) error {
	if inv.Schema != 1 || name != inv.Receipt.Claim.Name+".receipt" || !inv.Receipt.Matches(d.store.Destination(), inv.Receipt.Claim) || inv.Receipt.VersionID == "" || inv.Receipt.VersionID == "null" {
		return errors.New("invalid protected inventory identity/version")
	}
	if inv.SummaryVersion < 0 || inv.SummaryVersion > 1 || inv.LastSequence < inv.FirstSequence {
		return errors.New("invalid inventory summary")
	}
	if err := validateFencePair(inv.Baseline, inv.Terminal); err != nil {
		return err
	}
	return nil
}
func describeSegment(data []byte, incomplete bool) (CustodyInventory, error) {
	inv := CustodyInventory{Schema: 1, SummaryVersion: 1, Incomplete: incomplete}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		inv.Incomplete = true
	}
	scan := bufio.NewScanner(bytes.NewReader(data))
	scan.Buffer(make([]byte, 4096), 2*1024*1024)
	seen := false
	for scan.Scan() {
		var r Record
		if err := json.Unmarshal(scan.Bytes(), &r); err != nil {
			inv.Incomplete = true
			inv.Unidentified = true
			break
		}
		if seen && (inv.LastSequence == ^uint64(0) || r.Sequence != inv.LastSequence+1) {
			inv.Incomplete = true
		}
		if !seen {
			inv.JournalIncarnation = r.Incarnation
			inv.FirstSequence = r.Sequence
			seen = true
		}
		if inv.JournalIncarnation != r.Incarnation {
			return inv, errors.New("segment contains mixed journal identities")
		}
		inv.LastSequence = r.Sequence
		if reference := correlationReference(r); reference != nil {
			target := &inv.Baseline
			if reference.Scope == TerminalScope {
				target = &inv.Terminal
			}
			if *target != nil {
				return inv, errors.New("duplicate bounded correlation summary")
			}
			*target = reference
		}
		if !r.Valid {
			inv.Incomplete = true
		}
		if r.Workload != nil {
			if inv.Workload != nil && *inv.Workload != *r.Workload {
				return inv, errors.New("segment contains mixed workload identities")
			}
			w := *r.Workload
			inv.Workload = &w
		}
	}
	if err := scan.Err(); err != nil {
		return inv, err
	}
	inv.Unidentified = inv.Unidentified || !seen || inv.Workload == nil
	return inv, nil
}
func (d *protectedDelivery) pass(ctx context.Context) error {
	// Shared failure is absorbing for this owner. In particular, a renamed
	// receipt whose directory sync failed must not authorize same-owner replay.
	if err := d.service.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	segments, err := d.service.spool.deliveryCandidates(ctx, d.config.PassLimit, d.store.Destination().MaxObjectBytes)
	if err != nil {
		return err
	}
	for _, segment := range segments {
		if err := d.service.Err(); err != nil {
			return err
		}
		data, err := d.service.spool.readSegment(ctx, segment, d.store.Destination().MaxObjectBytes)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(data)
		claim := storage.CustodyClaim{Name: segment.Name, SHA256: hex.EncodeToString(hash[:]), Bytes: int64(len(data))}
		name := segment.Name + ".receipt"
		prior, err := d.readControl(name)
		if err == nil {
			var inv CustodyInventory
			if err = json.Unmarshal(prior, &inv); err != nil {
				return err
			}
			if err = d.validateInventory(name, inv); err != nil {
				return err
			}
			if inv.Receipt.Claim != claim {
				return errors.New("raw segment conflicts with durable custody inventory")
			}
			verified, err := d.store.VerifyVersion(ctx, claim, inv.Receipt.VersionID)
			if err != nil {
				return remoteDeliveryError{err}
			}
			if verified != inv.Receipt {
				return errors.New("replayed protected version differs from inventory")
			}
		} else if errors.Is(err, os.ErrNotExist) {
			inv, err := describeSegment(data, segment.Incomplete)
			if err != nil {
				return err
			}
			receipt, err := d.store.CreateExact(ctx, claim, data)
			if err != nil {
				return remoteDeliveryError{err}
			}
			inv.Receipt = receipt
			if err = d.validateInventory(name, inv); err != nil {
				return err
			}
			if err = d.persist(name, inv); err != nil {
				return err
			}
		} else {
			return err
		}
		if err := d.service.Err(); err != nil {
			return err
		}
		if err = d.service.spool.acknowledge(ctx, segment.Name, claim.SHA256); err != nil {
			return err
		}
	}
	if d.closing != nil {
		return d.closing.pass(ctx)
	}
	return nil
}
func transientDelivery(err error) bool {
	if err == ErrClosingPending {
		return true
	}
	if onlyCooperativeCancellation(err) {
		return true
	}
	var remote remoteDeliveryError
	if !errors.As(err, &remote) {
		return false
	}
	err = remote.error
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var n net.Error
	if errors.As(err, &n) {
		return true
	}
	var response *smithyhttp.ResponseError
	if errors.As(err, &response) {
		return response.HTTPStatusCode() >= 500 || response.HTTPStatusCode() == 429 || response.HTTPStatusCode() == 409
	}
	var api smithy.APIError
	if errors.As(err, &api) {
		return api.ErrorCode() == "SlowDown" || api.ErrorCode() == "RequestTimeout"
	}
	return false
}
func (d *protectedDelivery) attempt() {
	err := d.pass(context.Background())
	d.recordAttempt(err)
}
func (d *protectedDelivery) recordAttempt(err error) {
	d.mu.Lock()
	d.lastError = err
	d.mu.Unlock()
	if err != nil && !transientDelivery(err) {
		d.service.Fail(fmt.Errorf("protected delivery: %w", err))
	}
}
func (d *protectedDelivery) run() {
	defer close(d.done)
	base := time.Duration(d.config.IntervalSeconds) * time.Second
	delay := base
	timer := time.NewTimer(delay)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			d.attempt()
			if d.service.DeliveryError() != nil && d.service.DeliveryError() != ErrClosingPending {
				delay = min(delay*2, time.Hour)
			} else {
				delay = base
			}
			timer.Reset(delay)
		case <-d.stop:
			d.attempt()
			return
		}
	}
}
func (d *protectedDelivery) finish() { close(d.stop); <-d.done }

// DeliveryError reports current remote uncertainty without poisoning sibling
// sessions for a transient outage. Retained backlog remains bounded locally.
func (s *Service) DeliveryError() error {
	if s.delivery == nil {
		return nil
	}
	s.delivery.mu.Lock()
	defer s.delivery.mu.Unlock()
	return s.delivery.lastError
}
