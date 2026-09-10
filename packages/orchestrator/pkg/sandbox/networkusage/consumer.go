package networkusage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/e2b-dev/infra/packages/shared/pkg/storage"
)

const ConsumerVersion = "sig.network-evidence-consumer.v1"

// Set only by the build linker, e.g. -X <package>.consumerBuildRevision=<commit>.
// It is explicitly a build declaration, not a signed source attestation.
var consumerBuildRevision string

func ConsumerBuildIdentity() (VerifierIdentity, error) {
	if consumerBuildRevision != "" {
		if len(consumerBuildRevision) != 40 {
			return VerifierIdentity{}, errors.New("invalid embedded source declaration")
		}
		if _, err := hex.DecodeString(consumerBuildRevision); err != nil {
			return VerifierIdentity{}, err
		}
		return VerifierIdentity{Version: ConsumerVersion, SourceRevision: consumerBuildRevision, Provenance: "linker_embedded_source_declaration"}, nil
	}
	info, _ := debug.ReadBuildInfo()
	return consumerBuildIdentity(info)
}

type ConsumerOptions struct {
	MaxClaimBytes, MaxObjects, MaxRecordBytes, MaxDevices, MaxOutputEntries          int
	MaxObjectBytes, MaxTotalBytes, MaxMetadataBytes, MaxReceiptBytes, MaxOutputBytes int64
	Timeout                                                                          time.Duration
}

func (o ConsumerOptions) validate() error {
	if o.MaxClaimBytes < 512 || o.MaxClaimBytes > 65536 || o.MaxObjects < 3 || o.MaxObjects > 4096 || o.MaxRecordBytes < 1024 || o.MaxRecordBytes > 2*1024*1024 || o.MaxDevices < 1 || o.MaxDevices > 128 || o.MaxObjectBytes < 1024 || o.MaxObjectBytes > 64*1024*1024 || o.MaxTotalBytes < o.MaxObjectBytes || o.MaxTotalBytes > 1024*1024*1024 || o.MaxMetadataBytes < 8192 || o.MaxMetadataBytes > 32*1024*1024 || o.MaxReceiptBytes < 8192 || o.MaxReceiptBytes > 32*1024*1024 || o.MaxOutputBytes < o.MaxReceiptBytes || o.MaxOutputEntries < 3 || o.MaxOutputEntries > 100000 || o.Timeout < time.Millisecond || o.Timeout > 10*time.Minute {
		return errors.New("explicit bounded consumer budgets required")
	}
	return nil
}

type VerifierIdentity struct{ Version, SourceRevision, Provenance string }

// Build metadata identifies the compiled verifier; this is not a signature or
// an attestation that the caller's deployment was approved.
func consumerBuildIdentity(info *debug.BuildInfo) (VerifierIdentity, error) {
	identity := VerifierIdentity{Version: ConsumerVersion, Provenance: "go_build_vcs_metadata"}
	modified := ""
	if info != nil {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				identity.SourceRevision = setting.Value
			case "vcs.modified":
				modified = setting.Value
			}
		}
	}
	if len(identity.SourceRevision) != 40 || modified != "false" {
		return identity, errors.New("clean verifier VCS build provenance required")
	}
	if _, err := hex.DecodeString(identity.SourceRevision); err != nil {
		return identity, err
	}
	return identity, nil
}

type evidenceVersionReader interface {
	Destination() storage.CustodyDestination
	ReadVersion(context.Context, storage.CustodyClaim, string) ([]byte, storage.CustodyReceipt, error)
}
type NonMonetaryReceipt struct {
	Schema                   string
	Verifier                 VerifierIdentity
	RootClaim                closingClaim
	RootIdentitySHA256       string
	RootSlotSHA256           string
	Objects                  []storage.CustodyReceipt
	Workload                 WorkloadBinding
	Unidentified             bool
	RawBaseline, RawTerminal *CorrelationReference
	Prefix                   VerifiedPrefix
	Cutoff                   string
	SealOutcome              string
	Gaps                     []string
	Complete                 bool
	SessionMapping           string
}
type consumerTraversal struct {
	ctx             context.Context
	reader          evidenceVersionReader
	options         ConsumerOptions
	bytes, metadata int64
	objects         []storage.CustodyReceipt
	seen            map[string]bool
}

func (t *consumerTraversal) read(expected storage.CustodyReceipt, metadata bool) ([]byte, error) {
	if err := t.ctx.Err(); err != nil {
		return nil, err
	}
	d := t.reader.Destination()
	if expected.Destination != d || expected.VersionID == "" || len(expected.VersionID) > 1024 || len(expected.Key) > 2048 || expected.VersionID == "null" || expected.VersionID == "latest" || !expected.Matches(d, expected.Claim) {
		return nil, errors.New("exact configured object identity required")
	}
	if expected.Claim.Bytes < 0 || expected.Claim.Bytes > t.options.MaxObjectBytes || expected.Claim.Bytes > t.options.MaxTotalBytes-t.bytes || len(t.objects) >= t.options.MaxObjects {
		return nil, errors.New("traversal object/byte budget")
	}
	if t.seen[expected.Key] {
		return nil, errors.New("duplicate retained object key")
	}
	if metadata {
		if expected.Claim.Bytes > t.options.MaxMetadataBytes-t.metadata {
			return nil, errors.New("metadata payload budget before read")
		}
		t.metadata += expected.Claim.Bytes
	}
	if err := t.chargeMetadata(expected); err != nil {
		return nil, err
	}
	t.seen[expected.Key] = true
	t.bytes += expected.Claim.Bytes
	data, got, err := t.reader.ReadVersion(t.ctx, expected.Claim, expected.VersionID)
	if err != nil {
		return nil, err
	}
	if err = t.ctx.Err(); err != nil {
		return nil, err
	}
	hash := sha256.Sum256(data)
	if got != expected || int64(len(data)) != expected.Claim.Bytes || hex.EncodeToString(hash[:]) != expected.Claim.SHA256 {
		return nil, errors.New("exact retained bytes/receipt mismatch")
	}
	t.objects = append(t.objects, got)
	return data, nil
}
func (t *consumerTraversal) chargeMetadata(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if int64(len(data)) > t.options.MaxMetadataBytes-t.metadata {
		return errors.New("traversal metadata budget")
	}
	t.metadata += int64(len(data))
	return nil
}

func verifyConsumerChain(ctx context.Context, reader evidenceVersionReader, options ConsumerOptions, identity VerifierIdentity, root closingClaim) (NonMonetaryReceipt, error) {
	result := NonMonetaryReceipt{Schema: "sig.network-nonmonetary-evidence.v1", Verifier: identity, RootClaim: root, SessionMapping: "unresolved"}
	canonical, err := json.Marshal(root)
	if err != nil {
		return result, err
	}
	digest := sha256.Sum256(canonical)
	result.RootIdentitySHA256 = hex.EncodeToString(digest[:])
	result.RootSlotSHA256 = consumerSlot(root)
	t := &consumerTraversal{ctx: ctx, reader: reader, options: options, seen: map[string]bool{}}
	if root.Schema != 1 || root.Complete || !tokenOK(root.JournalIncarnation) || root.Receipt.Claim.Name != "manifest-"+root.JournalIncarnation+"-final" {
		return result, errors.New("invalid terminal claim")
	}
	raw, err := t.read(root.Receipt, true)
	if err != nil {
		return result, err
	}
	var final closingManifest
	if err = decodeConsumerJSON(raw, &final); err != nil {
		return result, err
	}
	if final.Schema != 1 || final.Complete || final.Plan.Complete || len(final.Parts) < 1 || len(final.Parts) > 128 || len(final.Parts) != len(final.Plan.Parts) {
		return result, errors.New("unsupported final manifest")
	}
	if err = validateIntent(root.JournalIncarnation, final.Plan.Intent); err != nil {
		return result, err
	}
	if err = validateSeal(final.Plan.Seal); err != nil {
		return result, err
	}
	if len(final.Plan.Gaps) > 8 {
		return result, errors.New("manifest gap count budget")
	}
	for _, gap := range final.Plan.Gaps {
		if len(gap) > 80 {
			return result, errors.New("manifest gap string budget")
		}
	}
	id := root.JournalIncarnation
	var inventory []CustodyInventory
	for i, receipt := range final.Parts {
		claim := final.Plan.Parts[i]
		if claim != receipt.Claim || claim.Name != fmt.Sprintf("manifest-%s-part-%d", id, i) {
			return result, errors.New("indexed part identity substitution")
		}
		raw, err = t.read(receipt, true)
		if err != nil {
			return result, err
		}
		var part closingPart
		if err = decodeConsumerJSON(raw, &part); err != nil {
			return result, err
		}
		if part.Schema != 1 || part.Complete || part.JournalIncarnation != id || part.Index != i || len(part.Inventory) > options.MaxObjects-len(inventory) {
			return result, errors.New("part schema/owner/membership budget")
		}
		for _, inv := range part.Inventory {
			if inv.Schema != 1 || inv.SummaryVersion != 1 || inv.JournalIncarnation != id || inv.LastSequence < inv.FirstSequence || !inventoryName(inv.Receipt.Claim.Name+".receipt") {
				return result, errors.New("unsupported raw inventory")
			}
			if err = validateFencePair(inv.Baseline, inv.Terminal); err != nil {
				return result, err
			}
			if inv.Workload != nil {
				if err = inv.Workload.validate(inv.Workload.SandboxID); err != nil {
					return result, err
				}
				if !tokenOK(inv.Workload.ProducerIncarnation) {
					return result, errors.New("invalid raw workload producer")
				}
			}
			if len(inventory) > 0 && inv.FirstSequence <= inventory[len(inventory)-1].LastSequence {
				return result, errors.New("raw membership duplicate/overlap/order")
			}
			inventory = append(inventory, inv)
		}
	}
	plan := final.Plan
	plan.Gaps = nil
	if err = checkClosingMembership(id, &plan, inventory); err != nil {
		return result, err
	}
	if !equalClosingJSON(plan.Gaps, final.Plan.Gaps) {
		return result, errors.New("frozen manifest gaps differ from membership")
	}
	result.Workload = final.Plan.Intent.Registration.Workload
	result.Unidentified = final.Plan.Intent.Registration.Unidentified
	replay := newConsumerReplay(id, result.Workload, options.MaxDevices)
	for _, gap := range plan.Gaps {
		replay.gaps[gap] = true
	}
	if result.Unidentified {
		replay.gap("unidentified_registration")
	}
	for _, inv := range inventory {
		raw, err = t.read(inv.Receipt, false)
		if err != nil {
			return result, err
		}
		if err = preflightConsumerRecords(ctx, raw, options.MaxRecordBytes); err != nil {
			return result, err
		}
		described, err := describeSegment(raw, strings.HasSuffix(inv.Receipt.Claim.Name, ".incomplete"))
		if err != nil {
			return result, err
		}
		described.Receipt = inv.Receipt
		if !equalClosingJSON(described, inv) {
			return result, errors.New("raw bytes differ from retained segment summary")
		}
		if result.Unidentified {
			continue
		} // no invented admitted workload or prefix
		partial, err := consumerRecordLines(raw, options.MaxRecordBytes, func(record Record) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			return replay.record(record)
		})
		if err != nil {
			return result, err
		}
		if partial {
			replay.gap("partial_raw_tail")
		}
	}
	if !result.Unidentified {
		if final.Plan.Intent.Baseline != nil && !equalClosingJSON(final.Plan.Intent.Baseline, replay.baseline) {
			return result, errors.New("baseline summary is not the original correlation")
		}
		if final.Plan.Intent.Terminal != nil && !equalClosingJSON(final.Plan.Intent.Terminal, replay.terminal) {
			return result, errors.New("terminal summary is not the original correlation")
		}
	}
	result.RawBaseline = replay.baseline
	result.RawTerminal = replay.terminal
	result.SealOutcome = final.Plan.Seal.Outcome
	if final.Plan.Seal.Outcome == "sealed" {
		if !replay.closed {
			replay.gaps["sealed_record_missing"] = true
		}
		if replay.closedPreKnown && final.Plan.Intent.LastSequence != replay.closedPreSequence {
			return result, errors.New("close intent is not the pre-closed sequence")
		}
	}
	result.Prefix = replay.finish()
	if result.Prefix.LowerBaseline == nil {
		replay.gaps["verified_lower_fence_missing"] = true
	}
	if result.Prefix.UpperTerminal == nil {
		replay.gaps["verified_upper_fence_missing"] = true
	} else {
		result.Cutoff = TerminalCutoff
	}
	if final.Plan.Intent.Uncertainty != "" {
		replay.gaps["close_intent_uncertain"] = true
	}
	for gap := range replay.gaps {
		result.Gaps = append(result.Gaps, gap)
	}
	sort.Strings(result.Gaps)
	result.Objects = t.objects
	return result, ctx.Err()
}
