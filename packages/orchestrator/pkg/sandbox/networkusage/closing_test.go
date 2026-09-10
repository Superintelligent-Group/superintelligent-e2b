package networkusage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/e2b-dev/infra/packages/shared/pkg/storage"
	"github.com/stretchr/testify/require"
)

type closingStore struct {
	failManifest    bool
	failFinalVerify bool
	*inventoryStore
	dataMu           sync.Mutex
	objects          map[string][]byte
	blocked, entered chan struct{}
}

func (s *closingStore) VerifyVersion(ctx context.Context, claim storage.CustodyClaim, version string) (storage.CustodyReceipt, error) {
	if s.failFinalVerify && strings.HasSuffix(claim.Name, "-final") {
		return storage.CustodyReceipt{}, context.DeadlineExceeded
	}
	return s.inventoryStore.VerifyVersion(ctx, claim, version)
}

func (s *closingStore) CreateExact(ctx context.Context, c storage.CustodyClaim, data []byte) (storage.CustodyReceipt, error) {
	if strings.HasPrefix(c.Name, "manifest-") && s.blocked != nil {
		select {
		case s.entered <- struct{}{}:
		default:
		}
		select {
		case <-s.blocked:
		case <-ctx.Done():
			return storage.CustodyReceipt{}, ctx.Err()
		}
	}
	if closingObjectClaim(c.Name, data) != c {
		return storage.CustodyReceipt{}, errors.New("incorrect supplied bytes")
	}
	s.dataMu.Lock()
	s.objects[c.Name] = append([]byte(nil), data...)
	s.dataMu.Unlock()
	receipt, err := s.inventoryStore.CreateExact(ctx, c, data)
	if err == nil && s.failManifest && strings.HasPrefix(c.Name, "manifest-") {
		return receipt, context.DeadlineExceeded
	}
	return receipt, err
}

func TestClosingExactBaselineTerminalSurviveOutOfOrderRawDelivery(t *testing.T) {
	config, store := closingFixture()
	dir := t.TempDir()
	options := serviceOptions()
	options.SegmentBytes = 16384
	service, e := openProtectedService(dir, options, config, store)
	require.NoError(t, e)
	c, e := service.NewCollector("one", inventoryWorkload("one"))
	require.NoError(t, e)
	frames, receipts := producerFixtures(t)
	for i := range frames {
		frames[i] = bytes.ReplaceAll(frames[i], []byte("sup909_local_boot"), []byte(c.Incarnation()))
	}
	for i := range receipts {
		receipts[i] = bytes.ReplaceAll(receipts[i], []byte("sup909_local_boot"), []byte(c.Incarnation()))
	}
	traffic(t, c, frames, receipts)
	mustBegin(t, c, "terminal", TerminalScope)
	mustObserve(t, c.ObserveReceipt(receipts[2]))
	mustObserve(t, c.ObserveFrame(frames[3]))
	require.NoError(t, c.Close())
	require.NoError(t, service.Close(t.Context()))
	claims := readClosingClaims(t, dir)
	require.Len(t, claims, 1)
	body, e := store.readVersion(claims[0].Receipt.Claim, claims[0].Receipt.VersionID)
	require.NoError(t, e)
	var manifest closingManifest
	require.NoError(t, json.Unmarshal(body, &manifest))
	require.Empty(t, manifest.Plan.Gaps)
	require.NotNil(t, manifest.Plan.Intent.Baseline)
	require.NotNil(t, manifest.Plan.Intent.Terminal)
	require.Equal(t, TerminalCutoff, manifest.Plan.Intent.Terminal.Cutoff)
	require.False(t, manifest.Complete)
	var previous uint64
	count := 0
	for _, partReceipt := range manifest.Parts {
		data, e := store.readVersion(partReceipt.Claim, partReceipt.VersionID)
		require.NoError(t, e)
		var part closingPart
		require.NoError(t, json.Unmarshal(data, &part))
		for _, inv := range part.Inventory {
			if count > 0 {
				require.Equal(t, previous+1, inv.FirstSequence)
			}
			previous = inv.LastSequence
			count++
		}
	}
	require.Greater(t, count, 1)
}

func TestClosingRestartAfterLostManifestResponseAndRawDeletion(t *testing.T) {
	config, store := closingFixture()
	store.failManifest = true
	dir := t.TempDir()
	service, e := openProtectedService(dir, serviceOptions(), config, store)
	require.NoError(t, e)
	closedInventoryCollector(t, service, "one")
	require.Error(t, service.Close(t.Context()))
	require.NoError(t, service.Err())
	require.Len(t, inventories(t, dir), 1)
	raw, e := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	require.NoError(t, e)
	require.Empty(t, raw)
	plans, e := filepath.Glob(filepath.Join(dir, ".custody", "closing", "*.plan"))
	require.NoError(t, e)
	require.Len(t, plans, 1)
	frozen, e := os.ReadFile(plans[0])
	require.NoError(t, e)
	store.failManifest = false
	service, e = openProtectedService(dir, serviceOptions(), config, store)
	require.NoError(t, e)
	require.NoError(t, service.Close(t.Context()))
	require.Empty(t, inventories(t, dir))
	claims := readClosingClaims(t, dir)
	require.Len(t, claims, 1)
	data, e := store.readVersion(claims[0].Receipt.Claim, claims[0].Receipt.VersionID)
	require.NoError(t, e)
	var manifest closingManifest
	require.NoError(t, json.Unmarshal(data, &manifest))
	var original closingPlan
	require.NoError(t, json.Unmarshal(frozen, &original))
	require.Equal(t, original, manifest.Plan)
}

func TestClosingManifestBudgetFailureRetainsRawInventory(t *testing.T) {
	config, store := closingFixture()
	config.Closing.MaxEntries = 8
	dir := t.TempDir()
	service, e := openProtectedService(dir, serviceOptions(), config, store)
	require.NoError(t, e)
	for _, id := range []string{"a", "b"} {
		closedInventoryCollector(t, service, id)
	}
	require.ErrorIs(t, service.Close(t.Context()), ErrSpoolBudget)
	require.Len(t, inventories(t, dir), 2)
	require.Empty(t, readClosingClaims(t, dir))
	require.Error(t, service.Err())
}

func TestClosingRejectsChangedConfigAndCorruptClaimOnRecovery(t *testing.T) {
	config, store := closingFixture()
	dir := t.TempDir()
	service, e := openProtectedService(dir, serviceOptions(), config, store)
	require.NoError(t, e)
	closedInventoryCollector(t, service, "a")
	require.NoError(t, service.Close(t.Context()))
	changed := config
	changed.Closing.MaxEntries++
	_, e = openProtectedService(dir, serviceOptions(), changed, store)
	require.ErrorContains(t, e, "binding changed")
	claims, e := filepath.Glob(filepath.Join(dir, ".custody", "closing", "*.claim"))
	require.NoError(t, e)
	require.Len(t, claims, 1)
	data, e := os.ReadFile(claims[0])
	require.NoError(t, e)
	require.NoError(t, os.WriteFile(claims[0], bytes.Replace(data, []byte(`"Complete":false`), []byte(`"Complete":true`), 1), 0600))
	_, e = openProtectedService(dir, serviceOptions(), config, store)
	require.ErrorContains(t, e, "terminal claim")
}
func (s *closingStore) readVersion(c storage.CustodyClaim, version string) ([]byte, error) {
	receipt, e := s.VerifyVersion(context.Background(), c, version)
	if e != nil {
		return nil, e
	}
	s.dataMu.Lock()
	defer s.dataMu.Unlock()
	data := s.objects[c.Name]
	if closingObjectClaim(c.Name, data) != receipt.Claim {
		return nil, errors.New("retained body mismatch")
	}
	return append([]byte(nil), data...), nil
}
func closingFixture() (ProtectedDeliveryConfig, *closingStore) {
	config, store := inventoryFixture()
	config.Closing = ClosingOptions{MaxBytes: 4 << 20, MaxEntries: 128, PartBytes: 32768, MaxParts: 16}
	return config, &closingStore{inventoryStore: store, objects: map[string][]byte{}}
}
func readClosingClaims(t *testing.T, dir string) []closingClaim {
	t.Helper()
	names, e := filepath.Glob(filepath.Join(dir, ".custody", "closing", "*.claim"))
	require.NoError(t, e)
	var claims []closingClaim
	for _, name := range names {
		data, e := os.ReadFile(name)
		require.NoError(t, e)
		var claim closingClaim
		require.NoError(t, json.Unmarshal(data, &claim))
		claims = append(claims, claim)
	}
	return claims
}

func TestClosingServiceRetainsFinalVersionAndRetiresOnlyInventory(t *testing.T) {
	config, store := closingFixture()
	dir := t.TempDir()
	service, e := openProtectedService(dir, serviceOptions(), config, store)
	require.NoError(t, e)
	collector, e := service.NewCollector("one", inventoryWorkload("one"))
	require.NoError(t, e)
	id := collector.journal.last.Incarnation
	require.FileExists(t, filepath.Join(dir, ".custody", "closing", id+".open"))
	require.NoError(t, collector.Close())
	require.FileExists(t, filepath.Join(dir, ".custody", "closing", id+".intent"))
	require.FileExists(t, filepath.Join(dir, ".custody", "closing", id+".seal"))
	require.NoError(t, service.Close(t.Context()))
	require.Empty(t, inventories(t, dir))
	claims := readClosingClaims(t, dir)
	require.Len(t, claims, 1)
	require.False(t, claims[0].Complete)
	body, e := store.readVersion(claims[0].Receipt.Claim, claims[0].Receipt.VersionID)
	require.NoError(t, e)
	var manifest closingManifest
	require.NoError(t, json.Unmarshal(body, &manifest))
	require.False(t, manifest.Complete)
	require.Contains(t, manifest.Plan.Gaps, "baseline_or_terminal_reference_missing")
	require.Equal(t, "sealed", manifest.Plan.Seal.Outcome)
	for _, receipt := range manifest.Parts {
		data, e := store.readVersion(receipt.Claim, receipt.VersionID)
		require.NoError(t, e)
		var part closingPart
		require.NoError(t, json.Unmarshal(data, &part))
		require.False(t, part.Complete)
		for _, inv := range part.Inventory {
			_, e = store.readVersion(inv.Receipt.Claim, inv.Receipt.VersionID)
			require.NoError(t, e)
		}
	}
	reopened, e := openProtectedService(dir, serviceOptions(), config, store)
	require.NoError(t, e)
	require.NoError(t, reopened.Close(t.Context()))
	require.Equal(t, claims, readClosingClaims(t, dir))
}

func TestClosingRemoteLatencyDoesNotBlockSiblingSeal(t *testing.T) {
	config, store := closingFixture()
	store.blocked = make(chan struct{})
	store.entered = make(chan struct{}, 1)
	service, e := openProtectedService(t.TempDir(), serviceOptions(), config, store)
	require.NoError(t, e)
	a, e := service.NewCollector("a", inventoryWorkload("a"))
	require.NoError(t, e)
	b, e := service.NewCollector("b", inventoryWorkload("b"))
	require.NoError(t, e)
	require.NoError(t, a.Close())
	result := make(chan error, 1)
	go func() { result <- service.delivery.pass(context.Background()) }()
	select {
	case <-store.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("manifest upload not reached")
	}
	closed := make(chan error, 1)
	go func() { closed <- b.Close() }()
	select {
	case e := <-closed:
		require.NoError(t, e)
	case <-time.After(time.Second):
		t.Fatal("sibling seal blocked by remote upload")
	}
	close(store.blocked)
	require.ErrorIs(t, <-result, ErrClosingPending)
	require.NoError(t, service.Close(t.Context()))
}

type closingBlockedFile struct {
	durableFile
	entered, unblock chan struct{}
}

func (f *closingBlockedFile) Close() error {
	close(f.entered)
	<-f.unblock
	return f.durableFile.Close()
}
func TestClosingIntentFailureBroadcastPrecedesBlockedSeal(t *testing.T) {
	config, store := closingFixture()
	service, e := openProtectedService(t.TempDir(), serviceOptions(), config, store)
	require.NoError(t, e)
	collector, e := service.NewCollector("a", inventoryWorkload("a"))
	require.NoError(t, e)
	file := collector.journal.file.(*serviceFile)
	blocked := &closingBlockedFile{durableFile: file.durableFile, entered: make(chan struct{}), unblock: make(chan struct{})}
	file.durableFile = blocked
	service.delivery.closing.syncDirectory = func() error { return syscall.ETIMEDOUT }
	done := make(chan error, 1)
	go func() { done <- collector.Close() }()
	select {
	case <-blocked.entered:
	case <-time.After(time.Second):
		t.Fatal("seal not reached")
	}
	select {
	case <-service.Failed():
	default:
		t.Fatal("known closing persistence failure was not broadcast before blocked IO")
	}
	require.Equal(t, 1, service.active)
	close(blocked.unblock)
	require.Error(t, <-done)
	require.Error(t, service.Close(t.Context()))
}
func TestClosingCanceledServiceJoinRetainsOwnership(t *testing.T) {
	config, store := closingFixture()
	dir := t.TempDir()
	service, e := openProtectedService(dir, serviceOptions(), config, store)
	require.NoError(t, e)
	collector, e := service.NewCollector("a", inventoryWorkload("a"))
	require.NoError(t, e)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, service.Close(ctx), context.Canceled)
	_, e = openProtectedService(dir, serviceOptions(), config, store)
	require.Error(t, e)
	require.NoError(t, collector.Close())
	require.NoError(t, service.Close(t.Context()))
	require.Len(t, readClosingClaims(t, dir), 1)
}

func TestClosingSummaryRetainsInternalSequenceGapAndPartialTail(t *testing.T) {
	workload := inventoryWorkload("one")
	for _, partial := range []bool{false, true} {
		var data []byte
		for _, sequence := range []uint64{0, 1, 3, 4} {
			raw, e := json.Marshal(Record{Incarnation: "journal", Sequence: sequence, Valid: true, Workload: &workload})
			require.NoError(t, e)
			data = append(append(data, raw...), '\n')
		}
		if partial {
			data = data[:len(data)-1]
		}
		inv, e := describeSegment(data, false)
		require.NoError(t, e)
		require.True(t, inv.Incomplete)
		require.Equal(t, uint64(0), inv.FirstSequence)
		require.Equal(t, uint64(4), inv.LastSequence)
		plan := closingPlan{Intent: collectorCloseIntent{Registration: collectorRegistration{Schema: 1, JournalIncarnation: "journal", Workload: workload}}, Seal: collectorSeal{Outcome: "sealed", LastSequence: 4}}
		require.NoError(t, (&closingLedger{}).checkMembership("journal", &plan, []CustodyInventory{inv}))
		require.Contains(t, plan.Gaps, "incomplete_or_unidentified_segment")
	}
}

func TestClosingRecoveryNeverInventsSuccessfulSeal(t *testing.T) {
	for _, lostIntent := range []bool{false, true} {
		t.Run(map[bool]string{false: "intent_only", true: "registration_only"}[lostIntent], func(t *testing.T) {
			config, store := closingFixture()
			store.lost = true // remote response loss retains all local raw controls
			dir := t.TempDir()
			s, e := openProtectedService(dir, serviceOptions(), config, store)
			require.NoError(t, e)
			c, e := s.NewCollector("a", inventoryWorkload("a"))
			require.NoError(t, e)
			id := c.journal.last.Incarnation
			require.NoError(t, c.Close())
			require.Error(t, s.Close(t.Context()))
			// Crash-image fixture: only registration (and optionally intent) made
			// the durability boundary. Raw bytes do not authorize a successful seal.
			require.NoError(t, os.Remove(filepath.Join(dir, ".custody", "closing", id+".seal")))
			if lostIntent {
				require.NoError(t, os.Remove(filepath.Join(dir, ".custody", "closing", id+".intent")))
			}
			store.lost = false
			s, e = openProtectedService(dir, serviceOptions(), config, store)
			require.NoError(t, e)
			require.NoError(t, s.Close(t.Context()))
			claims := readClosingClaims(t, dir)
			require.Len(t, claims, 1)
			body, e := store.readVersion(claims[0].Receipt.Claim, claims[0].Receipt.VersionID)
			require.NoError(t, e)
			var manifest closingManifest
			require.NoError(t, json.Unmarshal(body, &manifest))
			require.Equal(t, "owner_crashed_seal_unknown", manifest.Plan.Seal.Outcome)
			require.Contains(t, manifest.Plan.Gaps, "seal_not_confirmed")
			if lostIntent {
				require.Equal(t, "owner_crashed_without_close_intent", manifest.Plan.Intent.Uncertainty)
			}
		})
	}
}

func TestClosingFinalUploadWithoutExactVerificationCannotRetire(t *testing.T) {
	config, store := closingFixture()
	store.failFinalVerify = true
	dir := t.TempDir()
	s, e := openProtectedService(dir, serviceOptions(), config, store)
	require.NoError(t, e)
	closedInventoryCollector(t, s, "a")
	require.Error(t, s.Close(t.Context()))
	require.NoError(t, s.Err())
	require.Len(t, inventories(t, dir), 1)
	require.Empty(t, readClosingClaims(t, dir))
	store.failFinalVerify = false
	s, e = openProtectedService(dir, serviceOptions(), config, store)
	require.NoError(t, e)
	require.NoError(t, s.Close(t.Context()))
	require.Empty(t, inventories(t, dir))
	require.Len(t, readClosingClaims(t, dir), 1)
}

func TestClosingRestartAfterTerminalClaimAndRetirementSyncFailures(t *testing.T) {
	for _, stage := range []string{"claim", "inventory_retirement", "control_retirement"} {
		t.Run(stage, func(t *testing.T) {
			config, store := closingFixture()
			dir := t.TempDir()
			s, e := openProtectedService(dir, serviceOptions(), config, store)
			require.NoError(t, e)
			c, e := s.NewCollector("a", inventoryWorkload("a"))
			require.NoError(t, e)
			id := c.journal.last.Incarnation
			require.NoError(t, c.Close())
			claimPath := filepath.Join(dir, ".custody", "closing", id+".claim")
			calls := 0
			s.delivery.closing.syncDirectory = func() error {
				if _, err := os.Stat(claimPath); err == nil {
					calls++
					if stage == "claim" || (stage == "control_retirement" && calls > 1) {
						return syscall.ETIMEDOUT
					}
				}
				f, err := os.Open(filepath.Dir(claimPath))
				if err != nil {
					return err
				}
				defer f.Close()
				return f.Sync()
			}
			if stage == "inventory_retirement" {
				s.delivery.syncDirectory = func() error {
					if _, err := os.Stat(claimPath); err == nil {
						return syscall.ETIMEDOUT
					}
					f, err := os.Open(filepath.Join(dir, ".custody"))
					if err != nil {
						return err
					}
					defer f.Close()
					return f.Sync()
				}
			}
			require.ErrorIs(t, s.Close(t.Context()), syscall.ETIMEDOUT)
			require.Error(t, s.Err()) // local timeout is absorbing, never remote retry
			claims := readClosingClaims(t, dir)
			require.Len(t, claims, 1)
			if stage == "claim" {
				require.Len(t, inventories(t, dir), 1)
			}
			s, e = openProtectedService(dir, serviceOptions(), config, store)
			require.NoError(t, e)
			require.NoError(t, s.Close(t.Context()))
			require.Empty(t, inventories(t, dir))
			require.Equal(t, claims, readClosingClaims(t, dir))
			entries, e := os.ReadDir(filepath.Dir(claimPath))
			require.NoError(t, e)
			require.Len(t, entries, 1)
		})
	}
}

func TestClosingMultipleOwnersReportBoundedFinalBacklog(t *testing.T) {
	config, store := closingFixture()
	dir := t.TempDir()
	s, e := openProtectedService(dir, serviceOptions(), config, store)
	require.NoError(t, e)
	for _, id := range []string{"a", "b", "c"} {
		closedInventoryCollector(t, s, id)
	}
	for remaining := 2; remaining >= 0; remaining-- {
		err := s.Close(t.Context())
		if remaining > 0 {
			require.ErrorIs(t, err, ErrClosingPending)
		} else {
			require.NoError(t, err)
		}
		require.NoError(t, s.Err())
		require.Len(t, readClosingClaims(t, dir), 3-remaining)
		if remaining > 0 {
			s, e = openProtectedService(dir, serviceOptions(), config, store)
			require.NoError(t, e)
		}
	}
}

func TestClosingUnregisteredRawBacklogCannotReportSuccessfulDrain(t *testing.T) {
	config, store := closingFixture()
	config.PassLimit = 1
	store.lost = true
	dir := t.TempDir()
	s, e := openProtectedService(dir, serviceOptions(), config, store)
	require.NoError(t, e)
	for _, id := range []string{"a", "b"} {
		closedInventoryCollector(t, s, id)
	}
	require.Error(t, s.Close(t.Context()))
	// Simulate the retained filesystem image of writers that died before their
	// registrations crossed the durability boundary. This does not admit them.
	controls, e := filepath.Glob(filepath.Join(dir, ".custody", "closing", "*"))
	require.NoError(t, e)
	for _, control := range controls {
		require.NoError(t, os.Remove(control))
	}
	s, e = openProtectedService(dir, serviceOptions(), config, store)
	require.NoError(t, e)
	require.ErrorIs(t, s.Close(t.Context()), ErrClosingPending)
	require.NoError(t, s.Err())
	require.Empty(t, readClosingClaims(t, dir))
	for i := 0; i < 2; i++ {
		s, e = openProtectedService(dir, serviceOptions(), config, store)
		require.NoError(t, e)
		err := s.Close(t.Context())
		if i == 0 {
			require.ErrorIs(t, err, ErrClosingPending)
		} else {
			require.NoError(t, err)
		}
	}
	claims := readClosingClaims(t, dir)
	require.Len(t, claims, 2)
	for _, claim := range claims {
		data, e := store.readVersion(claim.Receipt.Claim, claim.Receipt.VersionID)
		require.NoError(t, e)
		var manifest closingManifest
		require.NoError(t, json.Unmarshal(data, &manifest))
		require.True(t, manifest.Plan.Intent.Registration.Unidentified)
		require.Empty(t, manifest.Plan.Intent.Registration.Workload)
		require.Contains(t, manifest.Plan.Gaps, "seal_not_confirmed")
	}
}

func TestClosingPreRenameFailureStopsSiblingControlWrites(t *testing.T) {
	config, store := closingFixture()
	dir := t.TempDir()
	s, e := openProtectedService(dir, serviceOptions(), config, store)
	require.NoError(t, e)
	a, e := s.NewCollector("a", inventoryWorkload("a"))
	require.NoError(t, e)
	b, e := s.NewCollector("b", inventoryWorkload("b"))
	require.NoError(t, e)
	l := s.delivery.closing
	used, entries := l.used, l.entries
	calls := 0
	l.syncFile = func(*os.File) error { calls++; return syscall.ETIMEDOUT }
	require.ErrorIs(t, a.Close(), syscall.ETIMEDOUT)
	require.ErrorIs(t, b.Close(), syscall.ETIMEDOUT)
	require.Equal(t, 1, calls)
	require.Equal(t, used, l.used)
	require.Equal(t, entries, l.entries)
	names, e := os.ReadDir(l.dir)
	require.NoError(t, e)
	require.Len(t, names, entries+1) // only the failed intent temporary reservation
	require.FileExists(t, filepath.Join(l.dir, a.journal.last.Incarnation+".intent.tmp"))
	require.NoFileExists(t, filepath.Join(l.dir, b.journal.last.Incarnation+".intent.tmp"))
	require.Error(t, s.Close(t.Context()))
	// A new owner can recover the one recognized temporary and preserve unknown
	// close outcomes, rather than retrying writes under the failed old owner.
	s, e = openProtectedService(dir, serviceOptions(), config, store)
	require.NoError(t, e)
	require.ErrorIs(t, s.Close(t.Context()), ErrClosingPending)
}

func TestClosingUnparseableInventoryRemainsExplicitBacklog(t *testing.T) {
	config, store := closingFixture()
	dir := t.TempDir()
	s, e := openProtectedService(dir, serviceOptions(), config, store)
	require.NoError(t, e)
	// A crash-tail with no decodable first record has no defensible journal id.
	// Exercise the actual spool delivery path, not a fabricated inventory claim.
	s.spool.mu.Lock()
	s.spool.writers++
	s.spool.mu.Unlock()
	w := &spoolWriter{spool: s.spool}
	_, e = w.Write([]byte("{unparseable\n"))
	require.NoError(t, e)
	require.NoError(t, w.Close())
	require.ErrorIs(t, s.Close(t.Context()), ErrClosingPending)
	require.NoError(t, s.Err())
	require.Len(t, inventories(t, dir), 1)
	require.Empty(t, readClosingClaims(t, dir))
	s, e = openProtectedService(dir, serviceOptions(), config, store)
	require.NoError(t, e)
	require.ErrorIs(t, s.Close(t.Context()), ErrClosingPending)
	require.Len(t, inventories(t, dir), 1)
}
