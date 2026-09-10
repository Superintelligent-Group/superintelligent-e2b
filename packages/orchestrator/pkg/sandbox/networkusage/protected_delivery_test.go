package networkusage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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

type inventoryStore struct {
	mu                       sync.Mutex
	destination              storage.CustodyDestination
	receipts                 map[string]storage.CustodyReceipt
	lost, badVersion         bool
	entered, unblock         chan struct{}
	createCount, verifyCount int
}

type endpointInventoryStore struct {
	endpoint    string
	destination storage.CustodyDestination
}

func (s *endpointInventoryStore) Destination() storage.CustodyDestination { return s.destination }
func (s *endpointInventoryStore) CreateExact(ctx context.Context, c storage.CustodyClaim, data []byte) (storage.CustodyReceipt, error) {
	return s.request(ctx, c, data, "")
}
func (s *endpointInventoryStore) VerifyExact(ctx context.Context, c storage.CustodyClaim) (storage.CustodyReceipt, error) {
	return s.request(ctx, c, nil, "exact-version-1")
}
func (s *endpointInventoryStore) VerifyVersion(ctx context.Context, c storage.CustodyClaim, v string) (storage.CustodyReceipt, error) {
	return s.request(ctx, c, nil, v)
}

type endpointClaim struct {
	Claim   storage.CustodyClaim
	Data    []byte
	Version string
}

func (s *endpointInventoryStore) request(ctx context.Context, c storage.CustodyClaim, data []byte, v string) (storage.CustodyReceipt, error) {
	var result storage.CustodyReceipt
	body, err := json.Marshal(endpointClaim{c, data, v})
	if err != nil {
		return result, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return result, err
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return result, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return result, errors.New("custody endpoint rejected exact object")
	}
	err = json.NewDecoder(response.Body).Decode(&result)
	return result, err
}
func TestProtectedOwnedSchedulerWithLostHTTPCustodyResponse(t *testing.T) {
	config, remote := inventoryFixture()
	config.IntervalSeconds = 1
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input endpointClaim
		if json.NewDecoder(r.Body).Decode(&input) != nil {
			w.WriteHeader(400)
			return
		}
		var receipt storage.CustodyReceipt
		var err error
		if input.Version != "" {
			receipt, err = remote.VerifyVersion(r.Context(), input.Claim, input.Version)
		} else {
			receipt, err = remote.CreateExact(r.Context(), input.Claim, input.Data)
		}
		if err != nil {
			w.WriteHeader(409)
			return
		}
		lost := false
		once.Do(func() { lost = true })
		if lost {
			connection, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				connection.Close()
			}
			return
		}
		json.NewEncoder(w).Encode(receipt)
	}))
	defer server.Close()
	store := &endpointInventoryStore{server.URL, remote.destination}
	dir := t.TempDir()
	s, err := openProtectedService(dir, serviceOptions(), config, store)
	require.NoError(t, err)
	closedInventoryCollector(t, s, "one")
	closedInventoryCollector(t, s, "two")
	require.Eventually(t, func() bool { return s.DeliveryError() != nil }, 3*time.Second, 10*time.Millisecond)
	require.NoError(t, s.Err(), "lost HTTP response remains retryable")
	require.NoError(t, s.Close(t.Context()))
	require.Len(t, inventories(t, dir), 2)
}

func TestProtectedDeliveryConfigurationRejectsAmbiguityAndDefaults(t *testing.T) {
	config, _ := inventoryFixture()
	raw, err := json.Marshal(config)
	require.NoError(t, err)
	parsed, err := ParseProtectedDeliveryConfig(string(raw))
	require.NoError(t, err)
	require.Equal(t, config, parsed)
	for _, bad := range []string{"{}", "null", string(raw) + " {}", strings.Replace(string(raw), `"PassLimit":100`, `"PassLimit":100,"PassLimit":1`, 1), strings.Replace(string(raw), `"RetentionDays":30`, `"RetentionDays":0`, 1), strings.Replace(string(raw), `"PassLimit":100`, `"unknown":1,"PassLimit":100`, 1)} {
		_, err := ParseProtectedDeliveryConfig(bad)
		require.Error(t, err, bad)
	}
}

func (f *inventoryStore) Destination() storage.CustodyDestination { return f.destination }
func (f *inventoryStore) CreateExact(ctx context.Context, c storage.CustodyClaim, data []byte) (storage.CustodyReceipt, error) {
	if f.entered != nil {
		select {
		case f.entered <- struct{}{}:
		default:
		}
		select {
		case <-f.unblock:
		case <-ctx.Done():
			return storage.CustodyReceipt{}, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCount++
	key, err := f.destination.ObjectKey(c)
	if err != nil {
		return storage.CustodyReceipt{}, err
	}
	r := storage.CustodyReceipt{Destination: f.destination, Claim: c, Key: key, VersionID: "exact-version-1"}
	if old, ok := f.receipts[c.Name]; ok && old != r {
		return storage.CustodyReceipt{}, errors.New("remote object conflict")
	}
	f.receipts[c.Name] = r
	if f.lost {
		f.lost = false
		return storage.CustodyReceipt{}, context.DeadlineExceeded
	}
	if f.badVersion {
		r.VersionID = "null"
	}
	return r, nil
}
func (f *inventoryStore) VerifyExact(ctx context.Context, c storage.CustodyClaim) (storage.CustodyReceipt, error) {
	return f.VerifyVersion(ctx, c, "exact-version-1")
}
func (f *inventoryStore) VerifyVersion(_ context.Context, c storage.CustodyClaim, version string) (storage.CustodyReceipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.verifyCount++
	r, ok := f.receipts[c.Name]
	if !ok || r.Claim != c || r.VersionID != version {
		return r, errors.New("wrong exact version")
	}
	return r, nil
}
func inventoryFixture() (ProtectedDeliveryConfig, *inventoryStore) {
	dest := storage.CustodyDestination{AccountID: "123456789012", Region: "us-east-1", Bucket: "test-evidence-bucket", Prefix: "network-usage/v1/test", MaxObjectBytes: 128 * 1024}
	config := ProtectedDeliveryConfig{Custody: storage.ProtectedCustodyConfig{Destination: dest, ProducerRoleARN: "arn:aws:iam::123456789012:role/client", ProducerRoleID: "AROA12345678901234567", PolicySHA256: strings.Repeat("a", 64), RetentionDays: 30}, MaxInventoryBytes: 1024 * 1024, MaxInventoryEntries: 64, IntervalSeconds: 3600, PassLimit: 100}
	dest.ProducerUserID = "AROA12345678901234567:i-0123456789abcdef0"
	return config, &inventoryStore{destination: dest, receipts: map[string]storage.CustodyReceipt{}}
}
func inventoryWorkload(id string) WorkloadBinding {
	return WorkloadBinding{SandboxID: id, ExecutionID: "execution-" + id, LifecycleID: "lifecycle-" + id, TemplateID: "template", ProducerSHA256: strings.Repeat("b", 64)}
}
func closedInventoryCollector(t *testing.T, s *Service, id string) {
	t.Helper()
	c, err := s.NewCollector(id, inventoryWorkload(id))
	require.NoError(t, err)
	require.NoError(t, c.Close())
}
func inventories(t *testing.T, dir string) []CustodyInventory {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, ".custody"))
	require.NoError(t, err)
	var result []CustodyInventory
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".receipt") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, ".custody", entry.Name()))
		require.NoError(t, err)
		var inv CustodyInventory
		require.NoError(t, json.Unmarshal(data, &inv))
		result = append(result, inv)
	}
	return result
}

func TestProtectedServiceInventorySurvivesRawReclaimAndRestart(t *testing.T) {
	config, store := inventoryFixture()
	dir := t.TempDir()
	s, err := openProtectedService(dir, serviceOptions(), config, store)
	require.NoError(t, err)
	_, err = s.NewCollector("unbound")
	require.ErrorContains(t, err, "workload binding")
	a, err := s.NewCollector("a", inventoryWorkload("a"))
	require.NoError(t, err)
	b, err := s.NewCollector("b", inventoryWorkload("b"))
	require.NoError(t, err)
	require.NoError(t, a.Close())
	require.NoError(t, b.Close())
	// The actual Service close runs its owned final delivery pass before unlock.
	require.NoError(t, s.Close(t.Context()))
	got := inventories(t, dir)
	require.Len(t, got, 2)
	for _, inv := range got {
		require.NotNil(t, inv.Workload)
		require.False(t, inv.Unidentified)
		require.NotEmpty(t, inv.Workload.ProducerIncarnation)
		require.Equal(t, "exact-version-1", inv.Receipt.VersionID)
		_, err := os.Stat(filepath.Join(dir, inv.Receipt.Claim.Name))
		require.ErrorIs(t, err, os.ErrNotExist)
	}
	s, err = openProtectedService(dir, serviceOptions(), config, store)
	require.NoError(t, err)
	require.NoError(t, s.Close(t.Context()))
	require.Equal(t, got, inventories(t, dir))
	_, err = OpenService(dir, serviceOptions())
	require.Error(t, err, "legacy owner cannot silently ignore protected inventory")
	store.destination.ProducerUserID = "AROA12345678901234567:i-11111111111111111"
	_, err = openProtectedService(dir, serviceOptions(), config, store)
	require.Error(t, err, "replacement instance must not relabel old evidence")
}

func TestProtectedInventoryCrashBoundaries(t *testing.T) {
	for _, boundary := range []string{"lost-upload", "receipt-directory-sync", "before-unlink", "after-unlink"} {
		t.Run(boundary, func(t *testing.T) {
			config, store := inventoryFixture()
			dir := t.TempDir()
			s, err := openProtectedService(dir, serviceOptions(), config, store)
			require.NoError(t, err)
			closedInventoryCollector(t, s, "work")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch boundary {
			case "lost-upload":
				store.lost = true
			case "receipt-directory-sync":
				s.delivery.syncDirectory = func() error { return errors.New("directory sync failed") }
			case "before-unlink":
				s.delivery.syncDirectory = func() error {
					f, err := os.Open(s.delivery.dir)
					if err != nil {
						return err
					}
					err = errors.Join(f.Sync(), f.Close())
					cancel()
					return err
				}
			case "after-unlink":
				s.spool.syncDirectory = func() error { return errors.New("raw unlink directory sync failed") }
			}
			err = s.delivery.pass(ctx)
			require.Error(t, err)
			if boundary == "lost-upload" {
				require.Empty(t, inventories(t, dir))
			} else {
				require.Len(t, inventories(t, dir), 1)
			}
			segments, segErr := s.spool.Segments()
			require.NoError(t, segErr)
			if boundary == "after-unlink" {
				require.Empty(t, segments)
			} else {
				require.Len(t, segments, 1)
			}
			// Simulate losing this owner's state: latch it so shutdown cannot retry
			// the interrupted pass; reopen with the same immutable remote objects.
			s.Fail(errors.New("owner lost"))
			require.Error(t, s.Close(t.Context()))
			s, err = openProtectedService(dir, serviceOptions(), config, store)
			require.NoError(t, err)
			require.NoError(t, s.Close(t.Context()))
			require.Len(t, inventories(t, dir), 1)
			if boundary == "before-unlink" || boundary == "receipt-directory-sync" {
				require.Equal(t, 1, store.verifyCount, "recovery rechecks exact protected version")
			}
		})
	}
}

func TestProtectedInventoryBudgetAndInvalidVersionStopAdmission(t *testing.T) {
	for _, fault := range []string{"entries", "bytes", "version"} {
		t.Run(fault, func(t *testing.T) {
			config, store := inventoryFixture()
			if fault == "entries" {
				config.MaxInventoryEntries = 1
			}
			dir := t.TempDir()
			s, err := openProtectedService(dir, serviceOptions(), config, store)
			require.NoError(t, err)
			closedInventoryCollector(t, s, "one")
			closedInventoryCollector(t, s, "two")
			if fault == "bytes" {
				s.delivery.used = config.MaxInventoryBytes - 1
			}
			if fault == "version" {
				store.badVersion = true
			}
			s.delivery.attempt()
			require.Error(t, s.Err())
			_, err = s.NewCollector("late", inventoryWorkload("late"))
			require.Error(t, err)
			segments, err := s.spool.Segments()
			require.NoError(t, err)
			require.NotEmpty(t, segments, "unreceipted raw evidence must survive")
			require.Error(t, s.Close(t.Context()))
		})
	}
}

func TestProtectedDeliverySlowCloseRetainsOwnerAndWaitsForWorker(t *testing.T) {
	config, store := inventoryFixture()
	store.entered = make(chan struct{}, 1)
	store.unblock = make(chan struct{})
	dir := t.TempDir()
	s, err := openProtectedService(dir, serviceOptions(), config, store)
	require.NoError(t, err)
	c, err := s.NewCollector("work", inventoryWorkload("work"))
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, s.Close(ctx), context.Canceled)
	_, err = OpenSpool(dir, serviceOptions())
	require.Error(t, err)
	require.NoError(t, c.Close())
	result := make(chan error, 1)
	go func() { result <- s.Close(context.Background()) }()
	select {
	case <-store.entered:
	case <-time.After(time.Second):
		t.Fatal("final pass did not start")
	}
	require.ErrorIs(t, s.Close(ctx), context.Canceled)
	_, err = OpenSpool(dir, serviceOptions())
	require.Error(t, err)
	close(store.unblock)
	require.NoError(t, <-result)
	require.Len(t, inventories(t, dir), 1)
}

func TestProtectedScheduledLostResponseDoesNotPoisonSibling(t *testing.T) {
	config, store := inventoryFixture()
	config.IntervalSeconds = 1
	store.lost = true
	dir := t.TempDir()
	s, err := openProtectedService(dir, serviceOptions(), config, store)
	require.NoError(t, err)
	a, err := s.NewCollector("one", inventoryWorkload("one"))
	require.NoError(t, err)
	b, err := s.NewCollector("two", inventoryWorkload("two"))
	require.NoError(t, err)
	require.NoError(t, a.Close())
	require.Eventually(t, func() bool { return errors.Is(s.DeliveryError(), context.DeadlineExceeded) }, 3*time.Second, 10*time.Millisecond)
	require.NoError(t, s.Err())
	require.NoError(t, b.Close())
	require.NoError(t, s.Close(t.Context()))
	require.Len(t, inventories(t, dir), 2)
}

func TestProtectedRejectsUnboundOldSpoolAndRecoversTemporaryReceipt(t *testing.T) {
	config, store := inventoryFixture()
	dir := t.TempDir()
	legacy, err := OpenService(dir, serviceOptions())
	require.NoError(t, err)
	closedInventoryCollector(t, legacy, "old")
	require.NoError(t, legacy.Close(t.Context()))
	_, err = openProtectedService(dir, serviceOptions(), config, store)
	require.ErrorContains(t, err, "unbound evidence")
	legacy, err = OpenService(dir, serviceOptions())
	require.NoError(t, err, "rejected activation must preserve legacy recovery")
	require.NoError(t, legacy.Close(t.Context()))
	dir = t.TempDir()
	s, err := openProtectedService(dir, serviceOptions(), config, store)
	require.NoError(t, err)
	require.NoError(t, s.Close(t.Context()))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".custody", strings.Repeat("a", 32)+".jsonl.receipt.tmp"), []byte("partial"), 0600))
	s, err = openProtectedService(dir, serviceOptions(), config, store)
	require.NoError(t, err)
	require.NoError(t, s.Close(t.Context()))
	require.Empty(t, inventories(t, dir))
}

func TestProtectedPersistentReceiptSyncFailureCloseCannotReclaim(t *testing.T) {
	config, store := inventoryFixture()
	dir := t.TempDir()
	s, err := openProtectedService(dir, serviceOptions(), config, store)
	require.NoError(t, err)
	closedInventoryCollector(t, s, "work")
	s.delivery.syncDirectory = func() error { return errors.New("persistent receipt dirsync failure") }
	s.delivery.attempt()
	require.ErrorContains(t, s.Err(), "receipt dirsync failure")
	require.Len(t, inventories(t, dir), 1, "rename is visible but not proof of durable inventory")
	require.Error(t, s.Close(t.Context()))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	raw := 0
	for _, e := range entries {
		if segmentName(e.Name()) {
			raw++
		}
	}
	require.Equal(t, 1, raw, "final pass must not reclaim through unsynced receipt")
	require.Equal(t, 0, store.verifyCount, "failed owner must not replay inventory")
}

func TestProtectedLocalTimeoutIsNotRemoteRetryPermission(t *testing.T) {
	for _, failure := range []error{syscall.ETIMEDOUT, context.DeadlineExceeded} {
		t.Run(failure.Error(), func(t *testing.T) {
			config, store := inventoryFixture()
			dir := t.TempDir()
			s, err := openProtectedService(dir, serviceOptions(), config, store)
			require.NoError(t, err)
			closedInventoryCollector(t, s, "work")
			s.delivery.syncDirectory = func() error { return failure }
			s.delivery.attempt()
			require.Error(t, s.Err(), "local timeout must absorb owner")
			s.delivery.attempt()
			require.Error(t, s.Close(t.Context()))
			inv := inventories(t, dir)
			require.Len(t, inv, 1)
			_, err = os.Stat(filepath.Join(dir, inv[0].Receipt.Claim.Name))
			require.NoError(t, err, "raw reclaimed after uncommitted local timeout")
			require.Equal(t, 0, store.verifyCount)
		})
	}
}

func TestProtectedPassCancellationAfterCustodyRetainsAndRetries(t *testing.T) {
	config, store := inventoryFixture()
	dir := t.TempDir()
	s, err := openProtectedService(dir, serviceOptions(), config, store)
	require.NoError(t, err)
	closedInventoryCollector(t, s, "work")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s.delivery.syncDirectory = func() error {
		f, err := os.Open(s.delivery.dir)
		if err != nil {
			return err
		}
		err = errors.Join(f.Sync(), f.Close())
		cancel()
		return err
	}
	err = s.delivery.pass(ctx)
	require.ErrorIs(t, err, context.Canceled)
	s.delivery.recordAttempt(err)
	require.NoError(t, s.Err(), "safe pass cancellation must not drain host")
	inv := inventories(t, dir)
	require.Len(t, inv, 1)
	_, err = os.Stat(filepath.Join(dir, inv[0].Receipt.Claim.Name))
	require.NoError(t, err)
	s.delivery.syncDirectory = nil
	require.NoError(t, s.Close(t.Context()))
	require.Equal(t, 1, store.verifyCount)
	_, err = os.Stat(filepath.Join(dir, inv[0].Receipt.Claim.Name))
	require.ErrorIs(t, err, os.ErrNotExist)
	// A failed close alongside cooperative cancellation still means local IO
	// uncertainty. The cancellation marker must not hide an additional error.
	require.False(t, transientDelivery(errors.Join(cooperativeCancellation{context.DeadlineExceeded}, syscall.ETIMEDOUT)))
}
