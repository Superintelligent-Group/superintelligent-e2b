//go:build linux

package networkusage_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/e2b-dev/infra/packages/orchestrator/pkg/sandbox/networkusage"
	"github.com/e2b-dev/infra/packages/shared/pkg/storage"
	"github.com/stretchr/testify/require"
)

// Local simulation only: no credential, retention or role-separation assertion.
// Each successful creation retains exact bytes and a receipt in separate files;
// no test deletes or replaces those files. Writer and reader have disjoint APIs.
type retainedBackend struct {
	mu          sync.Mutex
	dir         string
	destination storage.CustodyDestination
	objects     map[string]storage.CustodyReceipt
	used        int64
	ops         int
}

const retainedBytes = 64 << 20
const retainedObjects = 64
const retainedOperations = 1024

func newRetainedBackend(dir string) (*retainedBackend, networkusage.ProtectedDeliveryConfig, error) {
	dest := storage.CustodyDestination{AccountID: "123456789012", Region: "us-east-1", Bucket: "sup922-local-evidence", Prefix: "network-usage/v1/sup922", MaxObjectBytes: 4 << 20}
	config := networkusage.ProtectedDeliveryConfig{
		Custody:           storage.ProtectedCustodyConfig{Destination: dest, ProducerRoleARN: "arn:aws:iam::123456789012:role/local-fixture", ProducerRoleID: "AROA12345678901234567", PolicySHA256: strings.Repeat("a", 64), RetentionDays: 30},
		MaxInventoryBytes: 4 << 20, MaxInventoryEntries: 128, IntervalSeconds: 3600, PassLimit: 100,
		Closing: networkusage.ClosingOptions{MaxBytes: 4 << 20, MaxEntries: 128, PartBytes: 32768, MaxParts: 16},
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		return nil, config, err
	}
	dest.ProducerUserID = "AROA12345678901234567:i-0123456789abcdef0"
	b := &retainedBackend{dir: dir, destination: dest, objects: make(map[string]storage.CustodyReceipt)}
	return b, config, nil
}

func exactHash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func (b *retainedBackend) objectPath(key string) string {
	return filepath.Join(b.dir, exactHash([]byte(key))+".object")
}

func writeExclusive(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	return errors.Join(err, f.Sync(), f.Close())
}

// Called under b.mu. Append-only, bounded operation evidence, with no payloads.
func (b *retainedBackend) record(op string, claim storage.CustodyClaim, version string) error {
	if b.ops >= retainedOperations {
		return errors.New("local operation budget")
	}
	b.ops++
	data, err := json.Marshal(struct {
		Operation string
		Claim     storage.CustodyClaim
		Version   string
	}{op, claim, version})
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(b.dir, "operations.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	return errors.Join(err, f.Sync(), f.Close())
}

type retainedWriter struct{ backend *retainedBackend }

func (w *retainedWriter) Destination() storage.CustodyDestination { return w.backend.destination }
func (w *retainedWriter) CreateExact(ctx context.Context, claim storage.CustodyClaim, data []byte) (storage.CustodyReceipt, error) {
	b := w.backend
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return storage.CustodyReceipt{}, err
	}
	key, err := b.destination.ObjectKey(claim)
	if err != nil {
		return storage.CustodyReceipt{}, err
	}
	if int64(len(data)) != claim.Bytes || exactHash(data) != claim.SHA256 {
		return storage.CustodyReceipt{}, errors.New("writer bytes mismatch")
	}
	if err := b.record("create", claim, ""); err != nil {
		return storage.CustodyReceipt{}, err
	}
	if prior, ok := b.objects[key]; ok {
		if prior.Claim != claim {
			return storage.CustodyReceipt{}, errors.New("immutable object conflict")
		}
		return prior, nil
	}
	if len(b.objects) >= retainedObjects || claim.Bytes > retainedBytes-b.used {
		return storage.CustodyReceipt{}, errors.New("local retained budget")
	}
	receipt := storage.CustodyReceipt{Destination: b.destination, Claim: claim, Key: key, VersionID: fmt.Sprintf("local-version-%03d", len(b.objects)+1)}
	if err := writeExclusive(b.objectPath(key), data); err != nil {
		return storage.CustodyReceipt{}, err
	}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		return storage.CustodyReceipt{}, err
	}
	if err := writeExclusive(b.objectPath(key)+".receipt", encoded); err != nil {
		return storage.CustodyReceipt{}, err
	}
	b.objects[key] = receipt
	b.used += claim.Bytes
	return receipt, nil
}
func (w *retainedWriter) VerifyExact(ctx context.Context, claim storage.CustodyClaim) (storage.CustodyReceipt, error) {
	return w.verify(ctx, claim, "")
}
func (w *retainedWriter) VerifyVersion(ctx context.Context, claim storage.CustodyClaim, version string) (storage.CustodyReceipt, error) {
	return w.verify(ctx, claim, version)
}
func (w *retainedWriter) verify(ctx context.Context, claim storage.CustodyClaim, version string) (storage.CustodyReceipt, error) {
	b := w.backend
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return storage.CustodyReceipt{}, err
	}
	key, err := b.destination.ObjectKey(claim)
	if err != nil {
		return storage.CustodyReceipt{}, err
	}
	if err := b.record("verify", claim, version); err != nil {
		return storage.CustodyReceipt{}, err
	}
	r, ok := b.objects[key]
	if !ok || r.Claim != claim || (version != "" && r.VersionID != version) {
		return storage.CustodyReceipt{}, errors.New("writer exact version missing")
	}
	data, err := os.ReadFile(b.objectPath(key))
	if err != nil {
		return storage.CustodyReceipt{}, err
	}
	if int64(len(data)) != claim.Bytes || exactHash(data) != claim.SHA256 {
		return storage.CustodyReceipt{}, errors.New("writer retained bytes mismatch")
	}
	return r, nil
}

type retainedReader struct {
	backend     *retainedBackend
	reads       int // reader used sequentially by one acceptance traversal
	unavailable bool
}

func (r *retainedReader) Destination() storage.CustodyDestination { return r.backend.destination }
func (r *retainedReader) ReadVersion(ctx context.Context, claim storage.CustodyClaim, version string) ([]byte, storage.CustodyReceipt, error) {
	r.reads++
	b := r.backend
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.record("read", claim, version); err != nil {
		return nil, storage.CustodyReceipt{}, err
	}
	if err := ctx.Err(); err != nil {
		return nil, storage.CustodyReceipt{}, err
	}
	if r.unavailable {
		return nil, storage.CustodyReceipt{}, errors.New("reader deliberately unavailable")
	}
	if claim.Bytes < 0 || claim.Bytes > b.destination.MaxObjectBytes {
		return nil, storage.CustodyReceipt{}, errors.New("reader object budget")
	}
	key, err := b.destination.ObjectKey(claim)
	if err != nil {
		return nil, storage.CustodyReceipt{}, err
	}
	// Independent of writer verification and its in-memory receipt index.
	rawReceipt, err := os.ReadFile(b.objectPath(key) + ".receipt")
	if err != nil {
		return nil, storage.CustodyReceipt{}, err
	}
	var receipt storage.CustodyReceipt
	if err := json.Unmarshal(rawReceipt, &receipt); err != nil {
		return nil, storage.CustodyReceipt{}, err
	}
	if version == "" || receipt.VersionID != version || receipt.Destination != b.destination || receipt.Key != key || receipt.Claim != claim {
		return nil, storage.CustodyReceipt{}, errors.New("reader exact identity mismatch")
	}
	info, err := os.Stat(b.objectPath(key))
	if err != nil {
		return nil, storage.CustodyReceipt{}, err
	}
	if info.Size() != claim.Bytes {
		return nil, storage.CustodyReceipt{}, errors.New("reader retained length mismatch")
	}
	data, err := os.ReadFile(b.objectPath(key))
	if err != nil {
		return nil, storage.CustodyReceipt{}, err
	}
	if int64(len(data)) != claim.Bytes || exactHash(data) != claim.SHA256 {
		return nil, storage.CustodyReceipt{}, errors.New("reader retained hash mismatch")
	}
	return data, receipt, nil
}

// Fault only the read response; preserve the immutable successful-run evidence.
type faultyReader struct {
	*retainedReader
	target storage.CustodyReceipt
	mode   string
	hit    bool
}

func (r *faultyReader) ReadVersion(ctx context.Context, claim storage.CustodyClaim, version string) ([]byte, storage.CustodyReceipt, error) {
	data, got, err := r.retainedReader.ReadVersion(ctx, claim, version)
	if err != nil {
		return nil, storage.CustodyReceipt{}, err
	}
	if claim == r.target.Claim && version == r.target.VersionID {
		r.hit = true
		if r.mode == "missing" {
			return nil, storage.CustodyReceipt{}, os.ErrNotExist
		}
		if r.mode == "mismatch" && len(data) > 0 {
			data[0] ^= 1
		}
	}
	return data, got, nil
}

func runtimeConsumerOptions() networkusage.ConsumerOptions {
	return networkusage.ConsumerOptions{MaxClaimBytes: 8192, MaxObjects: retainedObjects, MaxRecordBytes: 2 << 20, MaxDevices: 8, MaxObjectBytes: 4 << 20, MaxTotalBytes: retainedBytes, MaxMetadataBytes: 4 << 20, MaxReceiptBytes: 4 << 20, MaxOutputBytes: 16 << 20, MaxOutputEntries: 64, Timeout: 10 * time.Second}
}

func verifyRuntimeConsumer(t *testing.T, ctx context.Context, base string, backend *retainedBackend, service *networkusage.Service, spoolOptions networkusage.SpoolOptions, config networkusage.ProtectedDeliveryConfig) []*networkusage.NonMonetaryReceipt {
	t.Helper()
	spoolDir := filepath.Join(base, "spool")
	// No owner was fabricated: both were registered by Process.startMetricsReader.
	// The scheduler performs at most one closing owner per pass. Its real Close
	// releases ownership even when reporting retryable remaining work.
	var passes []string
	for pass := 0; pass < 3; pass++ {
		err := service.Close(ctx)
		require.NoError(t, service.Err())
		if err == nil {
			passes = append(passes, "drained")
			break
		}
		require.ErrorIs(t, err, networkusage.ErrClosingPending)
		passes = append(passes, "closing_pending")
		require.Less(t, pass, 2, "bounded closing passes exhausted")
		service, err = networkusage.OpenProtectedServiceForTest(spoolDir, spoolOptions, config, &retainedWriter{backend})
		require.NoError(t, err)
		owned := service
		t.Cleanup(func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			err := owned.Close(cleanup)
			if err != nil && !errors.Is(err, networkusage.ErrClosingPending) {
				t.Error(err)
			}
		})
	}
	claims, err := filepath.Glob(filepath.Join(spoolDir, ".custody", "closing", "*.claim"))
	require.NoError(t, err)
	require.Len(t, claims, 2)
	// Protected service must actually acknowledge every raw segment and retire
	// custody membership. Keep the incompleteness marker and terminal claims.
	entries, err := os.ReadDir(spoolDir)
	require.NoError(t, err)
	for _, entry := range entries {
		require.True(t, strings.HasPrefix(entry.Name(), "."), "unreclaimed raw segment: %s", entry.Name())
	}
	inventories, err := filepath.Glob(filepath.Join(spoolDir, ".custody", "*.receipt"))
	require.NoError(t, err)
	require.Empty(t, inventories)
	consumerDir := filepath.Join(base, "consumer")
	require.NoError(t, os.Mkdir(consumerDir, 0700))
	reader := &retainedReader{backend: backend}
	consumer, err := networkusage.OpenEvidenceConsumerForTest(consumerDir, runtimeConsumerOptions(), reader)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, consumer.Close(context.Background())) })
	var receipts []*networkusage.NonMonetaryReceipt
	var originals [][]byte
	var encodedReceipts [][]byte
	seenWorkload, seenIncarnation := map[string]bool{}, map[string]bool{}
	for _, path := range claims {
		claim, err := os.ReadFile(path)
		require.NoError(t, err)
		originals = append(originals, claim)
		before := reader.reads
		receipt, err := consumer.Consume(ctx, claim)
		require.NoError(t, err)
		require.Equal(t, len(receipt.Objects), reader.reads-before, "every retained object must be read by exact version")
		require.False(t, receipt.Complete)
		require.False(t, receipt.Unidentified)
		require.Equal(t, "unresolved", receipt.SessionMapping)
		require.Equal(t, "sealed", receipt.SealOutcome)
		require.Equal(t, networkusage.TerminalCutoff, receipt.Cutoff)
		require.Empty(t, receipt.Gaps)
		require.NotNil(t, receipt.Prefix.LowerBaseline)
		require.NotNil(t, receipt.Prefix.UpperTerminal)
		require.Equal(t, receipt.RawBaseline, receipt.Prefix.LowerBaseline)
		require.Equal(t, receipt.RawTerminal, receipt.Prefix.UpperTerminal)
		require.NotNil(t, receipt.Prefix.FirstSequence)
		require.NotNil(t, receipt.Prefix.LastSequence)
		require.Less(t, *receipt.Prefix.FirstSequence, *receipt.Prefix.LastSequence)
		id := receipt.Workload.SandboxID
		require.Contains(t, []string{"sup922-create", "sup922-resume"}, id)
		require.False(t, seenWorkload[id])
		seenWorkload[id] = true
		require.Equal(t, "execution-"+id, receipt.Workload.ExecutionID)
		require.Equal(t, "lifecycle-"+id, receipt.Workload.LifecycleID)
		require.Equal(t, "sup922-local", receipt.Workload.TeamID)
		require.Equal(t, "sup922-local", receipt.Workload.TemplateID)
		require.Equal(t, "sup922-local-build", receipt.Workload.BuildID)
		require.Equal(t, "bfe0e5276dcdc8614316335887d7c8555eb6544d707b359070e420f67f33f9f7", receipt.Workload.ProducerSHA256)
		require.NotEmpty(t, receipt.Workload.ProducerIncarnation)
		require.False(t, seenIncarnation[receipt.Workload.ProducerIncarnation])
		seenIncarnation[receipt.Workload.ProducerIncarnation] = true
		require.NotEmpty(t, receipt.Prefix.Devices)
		for _, device := range receipt.Prefix.Devices {
			tx, err := strconv.ParseUint(device.TXBytes, 10, 64)
			require.NoError(t, err)
			require.Positive(t, tx)
			rx, err := strconv.ParseUint(device.RXBytes, 10, 64)
			require.NoError(t, err)
			require.Positive(t, rx)
		}
		rawCount := 0
		var rawTarget storage.CustodyReceipt
		for _, object := range receipt.Objects {
			if !strings.HasPrefix(object.Claim.Name, "manifest-") {
				rawCount++
				rawTarget = object
			}
		}
		require.Positive(t, rawCount, "consumer must traverse actual retained raw")
		// Both final-object and raw-object failures must fail before acknowledging
		// any output. No existing successful output can short-circuit these reads.
		for targetName, target := range map[string]storage.CustodyReceipt{"final": receipt.RootClaim.Receipt, "raw": rawTarget} {
			for _, mode := range []string{"missing", "mismatch"} {
				badDir := filepath.Join(base, id+"-"+targetName+"-"+mode)
				require.NoError(t, os.Mkdir(badDir, 0700))
				badReader := &faultyReader{retainedReader: &retainedReader{backend: backend}, target: target, mode: mode}
				bad, err := networkusage.OpenEvidenceConsumerForTest(badDir, runtimeConsumerOptions(), badReader)
				require.NoError(t, err)
				result, failure := bad.Consume(ctx, claim)
				require.NoError(t, bad.Close(ctx))
				require.Error(t, failure)
				require.Nil(t, result)
				require.True(t, badReader.hit)
				for _, suffix := range []string{"receipt", "claim"} {
					files, err := filepath.Glob(filepath.Join(badDir, "*."+suffix))
					require.NoError(t, err)
					require.Empty(t, files)
				}
				require.NoError(t, writeExclusive(filepath.Join(base, id+"-"+targetName+"-"+mode+".failure.txt"), []byte(failure.Error()+"\n")))
			}
		}
		encoded, err := json.Marshal(receipt)
		require.NoError(t, err)
		encodedReceipts = append(encodedReceipts, encoded)
		receipts = append(receipts, receipt)
	}
	require.NoError(t, consumer.Close(ctx))
	reader.unavailable = true
	beforeReplay := reader.reads
	reopened, err := networkusage.OpenEvidenceConsumerForTest(consumerDir, runtimeConsumerOptions(), reader)
	require.NoError(t, err)
	for i, claim := range originals {
		replay, err := reopened.Consume(ctx, claim)
		require.NoError(t, err)
		encoded, err := json.Marshal(replay)
		require.NoError(t, err)
		require.Equal(t, encodedReceipts[i], encoded)
		unchanged, err := os.ReadFile(claims[i])
		require.NoError(t, err)
		require.Equal(t, claim, unchanged)
	}
	require.NoError(t, reopened.Close(ctx))
	require.Equal(t, beforeReplay, reader.reads)
	checks, err := json.MarshalIndent(map[string]any{"closing_passes": passes, "claims": claims, "negative_reads": 8, "historical_replay_reads": reader.reads - beforeReplay, "raw_reclaimed": true, "inventory_retired": true}, "", "  ")
	require.NoError(t, err)
	require.NoError(t, writeExclusive(filepath.Join(base, "consumer-checks.json"), checks))
	return receipts
}

func TestRuntimeCustodyIndependentExactReader(t *testing.T) {
	b, _, err := newRetainedBackend(filepath.Join(t.TempDir(), "retained"))
	require.NoError(t, err)
	w := &retainedWriter{b}
	r := &retainedReader{backend: b}
	data := []byte("exact retained bytes\n")
	claim := storage.CustodyClaim{Name: "raw-test", SHA256: exactHash(data), Bytes: int64(len(data))}
	got, err := w.CreateExact(t.Context(), claim, data)
	require.NoError(t, err)
	// Erase only the writer index: reader uses the retained receipt and bytes.
	b.objects = make(map[string]storage.CustodyReceipt)
	read, receipt, err := r.ReadVersion(t.Context(), claim, got.VersionID)
	require.NoError(t, err)
	require.Equal(t, data, read)
	require.Equal(t, got, receipt)
	_, _, err = r.ReadVersion(t.Context(), claim, "wrong-version")
	require.Error(t, err)
	claim.SHA256 = strings.Repeat("a", 64)
	_, _, err = r.ReadVersion(t.Context(), claim, got.VersionID)
	require.Error(t, err)
}

func TestRuntimeCustodyRejectsOverwriteAndBudget(t *testing.T) {
	b, _, err := newRetainedBackend(filepath.Join(t.TempDir(), "retained"))
	require.NoError(t, err)
	w := &retainedWriter{b}
	data := []byte("one")
	claim := storage.CustodyClaim{Name: "raw-test", SHA256: exactHash(data), Bytes: int64(len(data))}
	receipt, err := w.CreateExact(t.Context(), claim, data)
	require.NoError(t, err)
	again, err := w.CreateExact(t.Context(), claim, data)
	require.NoError(t, err)
	require.Equal(t, receipt, again)
	changed := []byte("two")
	conflict := claim
	conflict.SHA256 = exactHash(changed)
	_, err = w.CreateExact(t.Context(), conflict, changed)
	require.ErrorContains(t, err, "immutable object conflict")
	b.used = retainedBytes
	claim.Name = "raw-next"
	_, err = w.CreateExact(t.Context(), claim, data)
	require.ErrorContains(t, err, "local retained budget")
	kept, err := os.ReadFile(b.objectPath(receipt.Key))
	require.NoError(t, err)
	require.Equal(t, data, kept)
}
