package networkusage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/e2b-dev/infra/packages/shared/pkg/storage"
	"github.com/stretchr/testify/require"
)

type consumerReader struct {
	store  *closingStore
	reads  atomic.Int32
	fail   bool
	before func(context.Context, storage.CustodyClaim) error
}

func (r *consumerReader) Destination() storage.CustodyDestination { return r.store.Destination() }
func (r *consumerReader) ReadVersion(ctx context.Context, claim storage.CustodyClaim, version string) ([]byte, storage.CustodyReceipt, error) {
	r.reads.Add(1)
	if r.before != nil {
		if err := r.before(ctx, claim); err != nil {
			return nil, storage.CustodyReceipt{}, err
		}
	}
	if r.fail {
		return nil, storage.CustodyReceipt{}, errors.New("read unavailable")
	}
	data, err := r.store.readVersion(claim, version)
	if err != nil {
		return nil, storage.CustodyReceipt{}, err
	}
	receipt, err := r.store.VerifyVersion(ctx, claim, version)
	return data, receipt, err
}
func consumerTestOptions() ConsumerOptions {
	return ConsumerOptions{MaxClaimBytes: 8192, MaxObjects: 64, MaxRecordBytes: 2 * 1024 * 1024, MaxDevices: 8, MaxObjectBytes: 128 * 1024, MaxTotalBytes: 8 * 1024 * 1024, MaxMetadataBytes: 4 * 1024 * 1024, MaxReceiptBytes: 4 * 1024 * 1024, MaxOutputBytes: 16 * 1024 * 1024, MaxOutputEntries: 64, Timeout: 5 * time.Second}
}
func consumerTestIdentity() VerifierIdentity {
	return VerifierIdentity{Version: ConsumerVersion, SourceRevision: strings.Repeat("a", 40), Provenance: "test_fixture_declaration"}
}
func consumerActualChain(t *testing.T, terminal bool) ([]byte, *consumerReader) {
	t.Helper()
	config, store := closingFixture()
	dir := t.TempDir()
	options := serviceOptions()
	options.SegmentBytes = 16384
	s, err := openProtectedService(dir, options, config, store)
	require.NoError(t, err)
	c, err := s.NewCollector("consumer", inventoryWorkload("consumer"))
	require.NoError(t, err)
	frames, receipts := producerFixtures(t)
	for i := range frames {
		frames[i] = bytes.ReplaceAll(frames[i], []byte("sup909_local_boot"), []byte(c.Incarnation()))
	}
	for i := range receipts {
		receipts[i] = bytes.ReplaceAll(receipts[i], []byte("sup909_local_boot"), []byte(c.Incarnation()))
	}
	traffic(t, c, frames, receipts)
	if terminal {
		mustBegin(t, c, "terminal", TerminalScope)
		mustObserve(t, c.ObserveReceipt(receipts[2]))
		mustObserve(t, c.ObserveFrame(frames[3]))
	}
	require.NoError(t, c.Close())
	require.NoError(t, s.Close(t.Context()))
	claims := readClosingClaims(t, dir)
	require.Len(t, claims, 1)
	raw, err := json.Marshal(claims[0])
	require.NoError(t, err)
	return raw, &consumerReader{store: store}
}
func TestConsumerActualClosingChainDurableReplay(t *testing.T) {
	root, reader := consumerActualChain(t, true)
	dir := t.TempDir()
	c, err := newEvidenceConsumer(dir, consumerTestOptions(), reader, consumerTestIdentity())
	require.NoError(t, err)
	result, err := c.Consume(t.Context(), root)
	require.NoError(t, err)
	require.False(t, result.Complete)
	require.Equal(t, "unresolved", result.SessionMapping)
	require.Empty(t, result.Gaps)
	require.NotNil(t, result.Prefix.LowerBaseline)
	require.NotNil(t, result.Prefix.UpperTerminal)
	require.Equal(t, TerminalCutoff, result.Cutoff)
	require.NotEmpty(t, result.Prefix.Devices)
	require.Greater(t, len(result.Objects), 3)
	original, err := json.Marshal(result)
	require.NoError(t, err)
	result.Prefix.Devices[0].TXBytes = "tampered alias"
	reads := reader.reads.Load()
	reader.fail = true
	replayed, err := c.Consume(t.Context(), root)
	require.NoError(t, err)
	require.Equal(t, reads, reader.reads.Load())
	replayedRaw, err := json.Marshal(replayed)
	require.NoError(t, err)
	require.Equal(t, original, replayedRaw)
	require.NoError(t, c.Close(t.Context()))
	c, err = newEvidenceConsumer(dir, consumerTestOptions(), reader, consumerTestIdentity())
	require.NoError(t, err)
	replayed, err = c.Consume(t.Context(), root)
	require.NoError(t, err)
	require.Equal(t, "unresolved", replayed.SessionMapping)
	require.NoError(t, c.Close(t.Context()))
}
func TestConsumerMissingTerminalPreservesPrefixUncertainty(t *testing.T) {
	root, reader := consumerActualChain(t, false)
	c, err := newEvidenceConsumer(t.TempDir(), consumerTestOptions(), reader, consumerTestIdentity())
	require.NoError(t, err)
	result, err := c.Consume(t.Context(), root)
	require.NoError(t, err)
	require.Nil(t, result.Prefix.UpperTerminal)
	require.Contains(t, result.Gaps, "verified_upper_fence_missing")
	require.False(t, result.Complete)
	require.NoError(t, c.Close(t.Context()))
}
func TestConsumerOutputMutationAndSourceConflict(t *testing.T) {
	for _, kind := range []string{"totals", "objects", "fence", "source", "root"} {
		t.Run(kind, func(t *testing.T) {
			root, reader := consumerActualChain(t, true)
			dir := t.TempDir()
			identity := consumerTestIdentity()
			c, err := newEvidenceConsumer(dir, consumerTestOptions(), reader, identity)
			require.NoError(t, err)
			result, err := c.Consume(t.Context(), root)
			require.NoError(t, err)
			require.NoError(t, c.Close(t.Context()))
			if kind == "source" {
				identity.SourceRevision = strings.Repeat("b", 40)
			} else if kind == "root" {
				var claim closingClaim
				require.NoError(t, json.Unmarshal(root, &claim))
				claim.Receipt.VersionID = "different-exact-version"
				root, err = json.Marshal(claim)
				require.NoError(t, err)
			} else {
				switch kind {
				case "totals":
					result.Prefix.Devices[0].TXBytes = "999999"
				case "objects":
					result.Objects[0].VersionID = "substituted"
				case "fence":
					result.Prefix.UpperTerminal.FrameBytes++
				}
				data, err := json.Marshal(result)
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(filepath.Join(dir, result.RootSlotSHA256+".receipt"), data, 0600))
			}
			c, err = newEvidenceConsumer(dir, consumerTestOptions(), reader, identity)
			if kind == "source" || kind == "root" {
				require.NoError(t, err)
				result, err = c.Consume(t.Context(), root)
				require.ErrorContains(t, err, "conflict")
				require.Nil(t, result)
				require.NoError(t, c.Close(t.Context()))
			} else {
				require.ErrorContains(t, err, "conflict")
			}
		})
	}
}

func TestConsumerBudgetsAndCancellationBeforeAcknowledgment(t *testing.T) {
	for _, kind := range []string{"input", "metadata_before_read", "objects", "total_bytes", "canceled", "deadline"} {
		t.Run(kind, func(t *testing.T) {
			root, reader := consumerActualChain(t, true)
			options := consumerTestOptions()
			ctx := t.Context()
			switch kind {
			case "input":
				options.MaxClaimBytes = 512
			case "metadata_before_read":
				options.MaxMetadataBytes = 8192
				var claim closingClaim
				require.NoError(t, json.Unmarshal(root, &claim))
				claim.Receipt.Claim.Bytes = 8193
				var err error
				root, err = json.Marshal(claim)
				require.NoError(t, err)
			case "objects":
				options.MaxObjects = 3
			case "total_bytes":
				var largest int64
				for _, receipt := range reader.store.receipts {
					if receipt.Claim.Bytes > largest {
						largest = receipt.Claim.Bytes
					}
				}
				options.MaxObjectBytes = largest
				options.MaxTotalBytes = largest
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "deadline":
				options.Timeout = 5 * time.Millisecond
				reader.before = func(ctx context.Context, _ storage.CustodyClaim) error { <-ctx.Done(); return ctx.Err() }
			}
			dir := t.TempDir()
			c, err := newEvidenceConsumer(dir, options, reader, consumerTestIdentity())
			require.NoError(t, err)
			result, err := c.Consume(ctx, root)
			require.Error(t, err)
			require.Nil(t, result)
			if kind == "input" || kind == "metadata_before_read" || kind == "canceled" {
				require.Zero(t, reader.reads.Load())
			}
			if kind == "objects" {
				require.LessOrEqual(t, reader.reads.Load(), int32(3))
			}
			claims, err := filepath.Glob(filepath.Join(dir, "*.claim"))
			require.NoError(t, err)
			require.Empty(t, claims)
			require.NoError(t, c.Close(t.Context()))
		})
	}
}

func TestConsumerOutputFailureRecoveryAndCanceledSync(t *testing.T) {
	for _, kind := range []string{"binding_sync", "receipt_sync", "claim_directory_sync", "canceled_receipt_sync"} {
		t.Run(kind, func(t *testing.T) {
			root, reader := consumerActualChain(t, true)
			dir := t.TempDir()
			c, err := newEvidenceConsumer(dir, consumerTestOptions(), reader, consumerTestIdentity())
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			c.syncFile = func(file *os.File) error {
				calls++
				if (kind == "binding_sync" && calls == 1) || (kind == "receipt_sync" && calls == 2) {
					return syscall.ETIMEDOUT
				}
				if kind == "canceled_receipt_sync" && calls == 2 {
					cancel()
				}
				return file.Sync()
			}
			if kind == "claim_directory_sync" {
				c.syncDirectory = func() error {
					claims, e := filepath.Glob(filepath.Join(dir, "*.claim"))
					if e != nil {
						return e
					}
					if len(claims) > 0 {
						return syscall.ETIMEDOUT
					}
					file, e := os.Open(dir)
					if e != nil {
						return e
					}
					defer file.Close()
					return file.Sync()
				}
			}
			result, err := c.Consume(ctx, root)
			require.Error(t, err)
			require.Nil(t, result)
			if kind == "canceled_receipt_sync" {
				require.NoError(t, c.Close(t.Context()))
			} else {
				prior := calls
				result, err = c.Consume(t.Context(), root)
				require.Error(t, err)
				require.Nil(t, result)
				require.Equal(t, prior, calls)
				require.ErrorIs(t, c.Close(t.Context()), syscall.ETIMEDOUT)
			}
			c, err = newEvidenceConsumer(dir, consumerTestOptions(), reader, consumerTestIdentity())
			require.NoError(t, err)
			result, err = c.Consume(t.Context(), root)
			require.NoError(t, err)
			require.False(t, result.Complete)
			require.NoError(t, c.Close(t.Context()))
		})
	}
}

func TestConsumerStrictRootJSONAndExactVersion(t *testing.T) {
	root, reader := consumerActualChain(t, true)
	for _, kind := range []string{"missing_false", "duplicate", "null", "latest", "float", "invalid_utf8", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			input := bytes.Clone(root)
			switch kind {
			case "missing_false":
				input = bytes.Replace(input, []byte(`,"Complete":false`), nil, 1)
			case "duplicate":
				input = bytes.Replace(input, []byte(`"Schema":1`), []byte(`"Schema":1,"Schema":1`), 1)
			case "null":
				input = bytes.Replace(input, []byte(`"Schema":1`), []byte(`"Schema":null`), 1)
			case "latest":
				input = bytes.Replace(input, []byte(`"VersionID":"exact-version-1"`), []byte(`"VersionID":"latest"`), 1)
			case "float":
				input = bytes.Replace(input, []byte(`"Schema":1`), []byte(`"Schema":1.0`), 1)
			case "invalid_utf8":
				input = append(input, 0xff)
			case "unknown":
				input = append([]byte(`{"unknown":0,`), input[1:]...)
			}
			c, err := newEvidenceConsumer(t.TempDir(), consumerTestOptions(), reader, consumerTestIdentity())
			require.NoError(t, err)
			before := reader.reads.Load()
			result, err := c.Consume(t.Context(), input)
			require.Error(t, err)
			require.Nil(t, result)
			require.Equal(t, before, reader.reads.Load())
			require.NoError(t, c.Close(t.Context()))
		})
	}
}

func TestConsumerEmbeddedBuildDeclaration(t *testing.T) {
	if os.Getenv("SUP919_EMBEDDED_PROVENANCE") != "1" {
		t.Skip("exact archive linker declaration gate not enabled")
	}
	identity, err := ConsumerBuildIdentity()
	require.NoError(t, err)
	require.Equal(t, os.Getenv("SUP919_EXPECTED_SOURCE"), identity.SourceRevision)
	require.Equal(t, "linker_embedded_source_declaration", identity.Provenance)
	require.Equal(t, ConsumerVersion, identity.Version)
}

// Rewrite a fake exact-version chain after changing source records, so attacks
// must be caught by semantic replay rather than merely a stale parent hash.
func consumerRewriteChain(t *testing.T, rootJSON []byte, reader *consumerReader, mutate func([]Record) []Record, editFinal func(*closingManifest), rawEdit ...func([]byte) []byte) []byte {
	t.Helper()
	var root closingClaim
	require.NoError(t, json.Unmarshal(rootJSON, &root))
	var final closingManifest
	require.NoError(t, json.Unmarshal(reader.store.objects[root.Receipt.Claim.Name], &final))
	var inventory []CustodyInventory
	var parts []closingPart
	for _, receipt := range final.Parts {
		var part closingPart
		require.NoError(t, json.Unmarshal(reader.store.objects[receipt.Claim.Name], &part))
		parts = append(parts, part)
	}
	set := func(name string, data []byte) storage.CustodyReceipt {
		claim := closingObjectClaim(name, data)
		key, err := reader.Destination().ObjectKey(claim)
		require.NoError(t, err)
		receipt := storage.CustodyReceipt{Destination: reader.Destination(), Claim: claim, Key: key, VersionID: "rewritten-exact-version"}
		reader.store.receipts[name] = receipt
		reader.store.objects[name] = bytes.Clone(data)
		return receipt
	}
	for p := range parts {
		for i, inv := range parts[p].Inventory {
			var records []Record
			raw := reader.store.objects[inv.Receipt.Claim.Name]
			partial, err := consumerRecordLines(raw, 2*1024*1024, func(r Record) error { records = append(records, r); return nil })
			require.NoError(t, err)
			require.False(t, partial)
			records = mutate(records)
			var rewritten []byte
			for _, record := range records {
				line, err := json.Marshal(record)
				require.NoError(t, err)
				rewritten = append(append(rewritten, line...), '\n')
			}
			for _, edit := range rawEdit {
				rewritten = edit(rewritten)
			}
			described, err := describeSegment(rewritten, false)
			require.NoError(t, err)
			described.Receipt = set(inv.Receipt.Claim.Name, rewritten)
			parts[p].Inventory[i] = described
			inventory = append(inventory, described)
		}
	}
	final.Plan.Intent.Baseline = nil
	final.Plan.Intent.Terminal = nil
	for _, inv := range inventory {
		if inv.Baseline != nil {
			final.Plan.Intent.Baseline = inv.Baseline
		}
		if inv.Terminal != nil {
			final.Plan.Intent.Terminal = inv.Terminal
		}
	}
	final.Plan.Gaps = nil
	require.NoError(t, checkClosingMembership(root.JournalIncarnation, &final.Plan, inventory))
	for i, part := range parts {
		data, err := json.Marshal(part)
		require.NoError(t, err)
		receipt := set(final.Parts[i].Claim.Name, data)
		final.Parts[i] = receipt
		final.Plan.Parts[i] = receipt.Claim
	}
	if editFinal != nil {
		editFinal(&final)
	}
	data, err := json.Marshal(final)
	require.NoError(t, err)
	root.Receipt = set(root.Receipt.Claim.Name, data)
	result, err := json.Marshal(root)
	require.NoError(t, err)
	return result
}

func TestConsumerRejectsSemanticallySubstitutedRawChain(t *testing.T) {
	for _, kind := range []string{"record_counter", "correlation_frame", "loss", "request_sequence", "close_intent"} {
		t.Run(kind, func(t *testing.T) {
			root, reader := consumerActualChain(t, true)
			changed := false
			root = consumerRewriteChain(t, root, reader, func(records []Record) []Record {
				for i := range records {
					r := &records[i]
					if changed {
						continue
					}
					switch kind {
					case "record_counter":
						if r.Kind == "producer_frame" && r.DeltaTxBytes > 0 {
							r.DeltaTxBytes++
							changed = true
						}
					case "correlation_frame":
						if r.Kind == "producer_correlation" {
							r.Producer.FrameSHA256 = strings.Repeat("c", 64)
							changed = true
						}
					case "request_sequence":
						if r.Kind == "producer_request" {
							r.Producer.Request.Sequence++
							changed = true
						}
					case "loss":
						if r.Kind == "producer_frame" {
							var envelope map[string]json.RawMessage
							require.NoError(t, json.Unmarshal(r.Producer.Raw, &envelope))
							var header ProducerHeader
							require.NoError(t, json.Unmarshal(envelope["measurement"], &header))
							header.Loss = true
							envelope["measurement"], _ = json.Marshal(header)
							r.Producer.Raw, _ = json.Marshal(envelope)
							r.Producer.Raw = append(r.Producer.Raw, '\n')
							r.Producer.Header = &header
							r.Producer.Bytes = len(r.Producer.Raw)
							r.Producer.SHA256 = closingObjectClaim("frame", r.Producer.Raw).SHA256
							changed = true
						}
					}
				}
				return records
			}, func(final *closingManifest) {
				if kind == "close_intent" {
					final.Plan.Intent.LastSequence++
					changed = true
				}
			})
			require.True(t, changed)
			c, err := newEvidenceConsumer(t.TempDir(), consumerTestOptions(), reader, consumerTestIdentity())
			require.NoError(t, err)
			result, err := c.Consume(t.Context(), root)
			require.Error(t, err)
			require.Nil(t, result)
			require.NoError(t, c.Close(t.Context()))
		})
	}
}

func TestConsumerRecordLimitPrecedesSummaryAndUnidentifiedReplay(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "identified", true: "unidentified"}[unknown], func(t *testing.T) {
			root, reader := consumerActualChain(t, true)
			if unknown {
				root = consumerRewriteChain(t, root, reader, func(records []Record) []Record { return records }, func(final *closingManifest) {
					final.Plan.Intent.Registration.Unidentified = true
					final.Plan.Intent.Registration.Workload = WorkloadBinding{}
				})
			}
			options := consumerTestOptions()
			options.MaxRecordBytes = 1024
			c, err := newEvidenceConsumer(t.TempDir(), options, reader, consumerTestIdentity())
			require.NoError(t, err)
			result, err := c.Consume(t.Context(), root)
			require.ErrorContains(t, err, "record budget")
			require.Nil(t, result)
			require.NoError(t, c.Close(t.Context()))
		})
	}
	partial := bytes.Repeat([]byte("x"), 1025)
	require.ErrorContains(t, preflightConsumerRecords(t.Context(), partial, 1024), "record budget")
	_, err := consumerRecordLines(partial, 1024, func(Record) error { t.Fatal("must not decode oversized tail"); return nil })
	require.ErrorContains(t, err, "record budget")
}

func TestConsumerGapStopsTotalsAndDoesNotPromoteLaterFence(t *testing.T) {
	root, reader := consumerActualChain(t, true)
	var lastVerified uint64
	var expected map[string][2]uint64
	removed := false
	root = consumerRewriteChain(t, root, reader, func(records []Record) []Record {
		var out []Record
		for _, r := range records {
			if r.Kind == "producer_frame" && r.Producer.Header.RequestID == nil {
				lastVerified = r.Sequence
				frame, err := DecodeProducerFrame(r.Producer.Raw)
				require.NoError(t, err)
				expected, err = consumerDeviceDeltas(frame, 8)
				require.NoError(t, err)
			}
			if r.Kind == "producer_request" && r.Producer.Scope == SampleScope {
				removed = true
				continue
			}
			out = append(out, r)
		}
		return out
	}, nil)
	require.True(t, removed)
	c, err := newEvidenceConsumer(t.TempDir(), consumerTestOptions(), reader, consumerTestIdentity())
	require.NoError(t, err)
	result, err := c.Consume(t.Context(), root)
	require.NoError(t, err)
	require.Equal(t, lastVerified, *result.Prefix.LastSequence)
	require.Nil(t, result.Prefix.UpperTerminal)
	require.NotNil(t, result.RawTerminal)
	require.Contains(t, result.Gaps, "journal_sequence_gap")
	for _, device := range result.Prefix.Devices {
		require.Equal(t, fmt.Sprint(expected[device.Device][0]), device.TXBytes)
		require.Equal(t, fmt.Sprint(expected[device.Device][1]), device.RXBytes)
	}
	require.NoError(t, c.Close(t.Context()))
}

func TestConsumerPartialTailAndMissingClosedRecordRemainUncertain(t *testing.T) {
	root, reader := consumerActualChain(t, true)
	edited := false
	root = consumerRewriteChain(t, root, reader, func(records []Record) []Record { return records }, nil, func(raw []byte) []byte {
		if bytes.Contains(raw, []byte(`"kind":"closed"`)) {
			start := bytes.LastIndexByte(raw[:len(raw)-1], '\n') + 1
			edited = true
			return raw[:start+12]
		}
		return raw
	})
	require.True(t, edited)
	c, err := newEvidenceConsumer(t.TempDir(), consumerTestOptions(), reader, consumerTestIdentity())
	require.NoError(t, err)
	result, err := c.Consume(t.Context(), root)
	require.NoError(t, err)
	require.False(t, result.Complete)
	require.Contains(t, result.Gaps, "partial_raw_tail")
	require.Contains(t, result.Gaps, "sealed_record_missing")
	require.NoError(t, c.Close(t.Context()))
}

func TestConsumerDuplicateAndReorderedSourceRecords(t *testing.T) {
	for _, kind := range []string{"duplicate", "reordered", "duplicate_json"} {
		t.Run(kind, func(t *testing.T) {
			root, reader := consumerActualChain(t, true)
			changed := false
			root = consumerRewriteChain(t, root, reader, func(records []Record) []Record {
				if !changed && len(records) > 3 {
					if kind == "duplicate" {
						records = append(records[:2], append([]Record{records[1]}, records[2:]...)...)
						changed = true
					}
					if kind == "reordered" {
						records[1], records[2] = records[2], records[1]
						changed = true
					}
				}
				return records
			}, nil, func(raw []byte) []byte {
				if kind == "duplicate_json" && !changed {
					changed = true
					return bytes.Replace(raw, []byte(`"schemaVersion":1`), []byte(`"schemaVersion":1,"schemaVersion":1`), 1)
				}
				return raw
			})
			require.True(t, changed)
			c, err := newEvidenceConsumer(t.TempDir(), consumerTestOptions(), reader, consumerTestIdentity())
			require.NoError(t, err)
			result, err := c.Consume(t.Context(), root)
			require.Error(t, err)
			require.Nil(t, result)
			require.NoError(t, c.Close(t.Context()))
		})
	}
}

func TestConsumerExactIntegersAndDeviceArithmetic(t *testing.T) {
	base := Record{SchemaVersion: 1, Sequence: math.MaxUint64, Valid: true}
	raw, err := json.Marshal(base)
	require.NoError(t, err)
	var parsed Record
	require.NoError(t, decodeConsumerJSON(raw, &parsed))
	require.Equal(t, uint64(math.MaxUint64), parsed.Sequence)
	for _, bad := range []string{"18446744073709551616", "9007199254740993.0", "01", "+1"} {
		input := bytes.Replace(raw, []byte(`"18446744073709551615"`), []byte(`"`+bad+`"`), 1)
		require.Error(t, decodeConsumerJSON(input, &parsed))
	}
	frame := ProducerFrame{DeviceMetricsPresent: true, NetworkPresent: true, Tx: math.MaxUint64, Metrics: []byte(`{"net_a":{"tx_bytes_count":18446744073709551615,"rx_bytes_count":0},"net_b":{"tx_bytes_count":1,"rx_bytes_count":0}}`)}
	_, err = consumerDeviceDeltas(frame, 8)
	require.ErrorContains(t, err, "overflow")
	frame.Tx = 7
	frame.Metrics = []byte(`{"net_a":{"tx_bytes_count":4,"rx_bytes_count":0},"net_b":{"tx_bytes_count":3,"rx_bytes_count":0}}`)
	deltas, err := consumerDeviceDeltas(frame, 8)
	require.NoError(t, err)
	require.Equal(t, uint64(4), deltas["net_a"][0])
	require.Equal(t, uint64(3), deltas["net_b"][0])
	_, err = consumerDeviceDeltas(frame, 1)
	require.ErrorContains(t, err, "budget")
	frame.Tx = 14
	_, err = consumerDeviceDeltas(frame, 8)
	require.ErrorContains(t, err, "aggregate differs")
}

func TestConsumerCanceledCloseRetainsOutputOwnership(t *testing.T) {
	_, store := closingFixture()
	reader := &consumerReader{store: store}
	dir := t.TempDir()
	c, err := newEvidenceConsumer(dir, consumerTestOptions(), reader, consumerTestIdentity())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, c.Close(ctx), context.Canceled)
	_, err = newEvidenceConsumer(dir, consumerTestOptions(), reader, consumerTestIdentity())
	require.Error(t, err)
	require.NoError(t, c.Close(t.Context()))
	c, err = newEvidenceConsumer(dir, consumerTestOptions(), reader, consumerTestIdentity())
	require.NoError(t, err)
	require.NoError(t, c.Close(t.Context()))
}
