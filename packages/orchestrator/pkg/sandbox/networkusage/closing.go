package networkusage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/e2b-dev/infra/packages/shared/pkg/storage"
)

// ClosingOptions is deliberately separate from raw/inventory budgets. Retained
// final claims also consume this budget; retention never silently becomes deletion.
type ClosingOptions struct {
	MaxBytes                        int64
	MaxEntries, PartBytes, MaxParts int
}

func (o ClosingOptions) enabled() bool { return o != (ClosingOptions{}) }
func (o ClosingOptions) validate() error {
	if !o.enabled() {
		return nil
	}
	if o.MaxBytes < 32768 || o.MaxEntries < 8 || o.MaxEntries > 100000 || o.PartBytes < 8192 || o.PartBytes > 1024*1024 || o.MaxParts < 1 || o.MaxParts > 128 {
		return errors.New("explicit bounded closing budgets required")
	}
	return nil
}

// CorrelationReference binds a correlation record to its exact frame, without
// retaining an unbounded history of ordinary sampling requests in local controls.
type CorrelationReference struct {
	Header                             ProducerHeader
	Scope                              RequestScope
	Cutoff                             string
	CorrelationSequence, FrameSequence uint64
	FrameSHA256                        string
	FrameBytes                         int
	ReceiptSHA256                      string
	ReceiptBytes                       int
}

func correlationReference(r Record) *CorrelationReference {
	p := r.Producer
	if r.Kind != "producer_correlation" || p == nil || p.Header == nil || (p.Scope != BaselineScope && p.Scope != TerminalScope) {
		return nil
	}
	header := *p.Header
	if header.RequestID != nil {
		id := *header.RequestID
		header.RequestID = &id
	}
	if header.RequestSequence != nil {
		sequence := *header.RequestSequence
		header.RequestSequence = &sequence
	}
	return &CorrelationReference{Header: header, Scope: p.Scope, Cutoff: p.Cutoff, CorrelationSequence: r.Sequence, FrameSequence: p.FrameSequence, FrameSHA256: p.FrameSHA256, FrameBytes: p.FrameBytes, ReceiptSHA256: p.SHA256, ReceiptBytes: p.Bytes}
}

type collectorRegistration struct {
	Unidentified       bool
	Schema             int
	JournalIncarnation string
	Workload           WorkloadBinding
	Complete           bool
}
type collectorCloseIntent struct {
	Registration       collectorRegistration
	LastSequence       uint64
	Baseline, Terminal *CorrelationReference
	Uncertainty        string
	Complete           bool
}
type collectorSeal struct {
	LastSequence uint64
	Outcome      string
	Complete     bool
}
type closingPart struct {
	Schema             int
	JournalIncarnation string
	Index              int
	Inventory          []CustodyInventory
	Complete           bool
}
type closingPlan struct {
	Gaps     []string
	Intent   collectorCloseIntent
	Seal     collectorSeal
	Parts    []storage.CustodyClaim
	Complete bool
}
type closingManifest struct {
	Schema   int
	Plan     closingPlan
	Parts    []storage.CustodyReceipt
	Complete bool
}
type closingClaim struct {
	Schema             int
	JournalIncarnation string
	Receipt            storage.CustodyReceipt
	Complete           bool
}

type closingLedger struct {
	syncDirectory func() error
	syncFile      func(*os.File) error
	mu            sync.Mutex
	delivery      *protectedDelivery
	dir           string
	options       ClosingOptions
	used          int64
	entries       int
}

func openClosingLedger(d *protectedDelivery) (*closingLedger, error) {
	l := &closingLedger{delivery: d, dir: filepath.Join(d.dir, "closing"), options: d.config.Closing}
	if err := os.Mkdir(l.dir, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	info, err := os.Lstat(l.dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("invalid closing directory")
	}
	if err = d.syncControlDir(); err != nil {
		return nil, err
	}
	f, err := os.Open(l.dir)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(l.options.MaxEntries + 2)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(entries) > l.options.MaxEntries+1 {
		return nil, ErrSpoolBudget
	}
	for _, entry := range entries {
		name := entry.Name()
		if !closingControlName(name) {
			return nil, errors.New("unexpected closing control")
		}
		data, err := l.read(name)
		if err != nil {
			return nil, err
		}
		if strings.HasSuffix(name, ".tmp") {
			if err = os.Remove(filepath.Join(l.dir, name)); err != nil {
				return nil, err
			}
			continue
		}
		if err = l.validateControl(name); err != nil {
			return nil, err
		}
		l.used += int64(len(data))
		l.entries++
	}
	if l.used > l.options.MaxBytes || l.entries > l.options.MaxEntries {
		return nil, ErrSpoolBudget
	}
	if err = l.syncDir(); err != nil {
		return nil, err
	}
	return l, nil
}
func closingControlName(name string) bool {
	if strings.ContainsAny(name, "/\\") || len(name) > 120 {
		return false
	}
	name = strings.TrimSuffix(name, ".tmp")
	parts := strings.Split(name, ".")
	if len(parts) != 2 || !tokenOK(parts[0]) {
		return false
	}
	switch parts[1] {
	case "open", "intent", "seal", "plan", "final", "claim":
		return true
	}
	if strings.HasPrefix(parts[1], "part-") {
		var index int
		var tail string
		n, _ := fmt.Sscanf(parts[1], "part-%d%s", &index, &tail)
		return n == 1 && index >= 0 && index < 128
	}
	if strings.HasPrefix(parts[1], "receipt-") {
		var index int
		var tail string
		n, _ := fmt.Sscanf(parts[1], "receipt-%d%s", &index, &tail)
		return n == 1 && index >= 0 && index < 128
	}
	return false
}
func (l *closingLedger) read(name string) ([]byte, error) {
	f, err := openSpoolControl(filepath.Join(l.dir, name), os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, int64(l.options.PartBytes)+1))
	if len(b) > l.options.PartBytes {
		return nil, ErrSpoolBudget
	}
	return b, err
}
func (l *closingLedger) syncDir() error {
	if l.syncDirectory != nil {
		return l.syncDirectory()
	}
	f, e := os.Open(l.dir)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}

// Called only under l.mu. All local failures are absorbing at the Service edge;
// none are wrapped in the remote retry marker.
func (l *closingLedger) persist(name string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return l.persistBytes(name, data)
}
func (l *closingLedger) persistBytes(name string, data []byte) (err error) {
	if failure := l.delivery.service.Err(); failure != nil {
		return failure
	}
	// Latch under the ledger mutex, including registration failures. No sibling
	// may reserve another temporary write after a failed pre-rename operation.
	defer func() {
		if err != nil {
			l.delivery.service.Fail(err)
		}
	}()
	if !closingControlName(name) {
		return errors.New("invalid closing name")
	}
	prior, err := l.read(name)
	if err == nil {
		if !bytes.Equal(prior, data) {
			return errors.New("conflicting closing replay")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(data) > l.options.PartBytes || int64(len(data)) > l.options.MaxBytes-l.used || l.entries >= l.options.MaxEntries {
		return ErrSpoolBudget
	}
	f, err := openSpoolControl(filepath.Join(l.dir, name+".tmp"), os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return err
	}
	n, e := f.Write(data)
	if e == nil && n != len(data) {
		e = io.ErrShortWrite
	}
	syncFile := l.syncFile
	if syncFile == nil {
		syncFile = func(file *os.File) error { return file.Sync() }
	}
	err = errors.Join(e, syncFile(f), f.Close())
	if err != nil {
		return err
	}
	if err = os.Rename(filepath.Join(l.dir, name+".tmp"), filepath.Join(l.dir, name)); err != nil {
		return err
	}
	if err = l.syncDir(); err != nil {
		return err
	}
	l.used += int64(len(data))
	l.entries++
	return nil
}
func (l *closingLedger) register(j *Journal, w WorkloadBinding) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	registration := collectorRegistration{Schema: 1, JournalIncarnation: j.last.Incarnation, Workload: w}
	if err := l.persist(registration.JournalIncarnation+".open", registration); err != nil {
		return err
	}
	j.beforeSeal = func(last Record, baseline, terminal *CorrelationReference) error {
		l.mu.Lock()
		defer l.mu.Unlock()
		reason := ""
		if !last.Valid {
			reason = "invalid_journal"
		}
		if terminal == nil {
			reason = "terminal_not_observed"
		}
		err := l.persist(registration.JournalIncarnation+".intent", collectorCloseIntent{Registration: registration, LastSequence: last.Sequence, Baseline: baseline, Terminal: terminal, Uncertainty: reason})
		if err != nil {
			l.delivery.service.Fail(err)
		}
		return err
	}
	j.afterSeal = func(last Record, sealError error) error {
		l.mu.Lock()
		defer l.mu.Unlock()
		outcome := "sealed"
		if sealError != nil {
			outcome = "seal_failed"
		}
		err := l.persist(registration.JournalIncarnation+".seal", collectorSeal{LastSequence: last.Sequence, Outcome: outcome})
		if err != nil {
			l.delivery.service.Fail(err)
		}
		return err
	}
	return nil
}
func closingObjectClaim(name string, data []byte) storage.CustodyClaim {
	h := sha256.Sum256(data)
	return storage.CustodyClaim{Name: name, SHA256: hex.EncodeToString(h[:]), Bytes: int64(len(data))}
}
