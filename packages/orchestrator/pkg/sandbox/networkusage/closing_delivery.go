package networkusage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/e2b-dev/infra/packages/shared/pkg/storage"
)

var ErrClosingPending = errors.New("retained closing work remains after bounded pass")

func (l *closingLedger) decode(name string, value any) error {
	data, e := l.read(name)
	if e != nil {
		return e
	}
	if e = checkJSON(data); e != nil {
		return e
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	return decoder.Decode(value)
}
func (l *closingLedger) names() ([]string, error) {
	entries, e := boundedClosingDirectory(l.dir, l.options.MaxEntries+1)
	if e != nil {
		return nil, e
	}
	if len(entries) > l.options.MaxEntries+1 {
		return nil, ErrSpoolBudget
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}
func (l *closingLedger) recoverOwners() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	names, e := l.names()
	if e != nil {
		return e
	}
	for _, name := range names {
		if !strings.HasSuffix(name, ".open") {
			continue
		}
		id := strings.TrimSuffix(name, ".open")
		var registration collectorRegistration
		if e = l.decode(name, &registration); e != nil {
			return e
		}
		if e = validateRegistration(id, registration); e != nil {
			return e
		}
		var claim closingClaim
		if e = l.decode(id+".claim", &claim); e == nil {
			continue
		} else if !errors.Is(e, os.ErrNotExist) {
			return e
		}
		var intent collectorCloseIntent
		if e = l.decode(id+".intent", &intent); errors.Is(e, os.ErrNotExist) {
			intent = collectorCloseIntent{Registration: registration, Uncertainty: "owner_crashed_without_close_intent"}
			if e = l.persist(id+".intent", intent); e != nil {
				return e
			}
		} else if e != nil {
			return e
		}
		var seal collectorSeal
		if e = l.decode(id+".seal", &seal); errors.Is(e, os.ErrNotExist) {
			if e = l.persist(id+".seal", collectorSeal{Outcome: "owner_crashed_seal_unknown"}); e != nil {
				return e
			}
		} else if e != nil {
			return e
		}
	}
	return nil
}

func (l *closingLedger) inventory(id string) ([]CustodyInventory, error) {
	entries, e := boundedClosingDirectory(l.delivery.dir, l.delivery.config.MaxInventoryEntries+3)
	if e != nil {
		return nil, e
	}
	if len(entries) > l.delivery.config.MaxInventoryEntries+3 {
		return nil, ErrSpoolBudget
	}
	var result []CustodyInventory
	for _, entry := range entries {
		if !inventoryName(entry.Name()) {
			continue
		}
		data, e := l.delivery.readControl(entry.Name())
		if e != nil {
			return nil, e
		}
		var inv CustodyInventory
		if e = json.Unmarshal(data, &inv); e != nil {
			return nil, e
		}
		if e = l.delivery.validateInventory(entry.Name(), inv); e != nil {
			return nil, e
		}
		if inv.JournalIncarnation == id {
			result = append(result, inv)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].FirstSequence < result[j].FirstSequence })
	for i := 1; i < len(result); i++ {
		if result[i].FirstSequence <= result[i-1].LastSequence {
			return nil, errors.New("overlapping closing inventory")
		}
	}
	return result, nil
}
func (l *closingLedger) freeze(id string) (closingPlan, error) {
	var plan closingPlan
	if e := l.decode(id+".plan", &plan); e == nil {
		return plan, l.validatePlan(id, plan)
	} else if !errors.Is(e, os.ErrNotExist) {
		return plan, e
	}
	if e := l.decode(id+".intent", &plan.Intent); e != nil {
		return plan, e
	}
	if e := l.decode(id+".seal", &plan.Seal); e != nil {
		return plan, e
	}
	inventory, e := l.inventory(id)
	if e != nil {
		return plan, e
	}
	if e = l.checkMembership(id, &plan, inventory); e != nil {
		return plan, e
	}
	if len(inventory) == 0 && plan.Intent.Uncertainty == "" {
		plan.Intent.Uncertainty = "no_identified_inventory"
	}
	// Failed/legacy summaries are kept as explicit uncertainty, never invented.
	baseline, terminal := false, false
	for _, inv := range inventory {
		baseline = baseline || inv.Baseline != nil
		terminal = terminal || inv.Terminal != nil
	}
	if (!baseline || !terminal) && plan.Intent.Uncertainty == "" {
		plan.Intent.Uncertainty = "missing_baseline_or_terminal_inventory_reference"
	}
	var part closingPart
	flush := func() error {
		if len(plan.Parts) >= l.options.MaxParts {
			return ErrSpoolBudget
		}
		part.Schema = 1
		part.JournalIncarnation = id
		part.Index = len(plan.Parts)
		data, e := json.Marshal(part)
		if e != nil {
			return e
		}
		if e = l.persistBytes(fmt.Sprintf("%s.part-%d", id, part.Index), data); e != nil {
			return e
		}
		plan.Parts = append(plan.Parts, closingObjectClaim(fmt.Sprintf("manifest-%s-part-%d", id, part.Index), data))
		part = closingPart{}
		return nil
	}
	for _, inv := range inventory {
		part.Schema = 1
		part.JournalIncarnation = id
		part.Index = len(plan.Parts)
		part.Inventory = append(part.Inventory, inv)
		data, e := json.Marshal(part)
		if e != nil {
			return plan, e
		}
		if len(data) > l.options.PartBytes {
			part.Inventory = part.Inventory[:len(part.Inventory)-1]
			if len(part.Inventory) == 0 {
				return plan, ErrSpoolBudget
			}
			if e = flush(); e != nil {
				return plan, e
			}
			part.Inventory = []CustodyInventory{inv}
		}
	}
	if len(part.Inventory) > 0 || len(plan.Parts) == 0 {
		if e = flush(); e != nil {
			return plan, e
		}
	}
	if e = l.persist(id+".plan", plan); e != nil {
		return plan, e
	}
	return plan, nil
}

func (l *closingLedger) protectedObject(ctx context.Context, name string, claim storage.CustodyClaim, data []byte) (storage.CustodyReceipt, error) {
	var receipt storage.CustodyReceipt
	e := l.decode(name, &receipt)
	if e == nil {
		if !receipt.Matches(l.delivery.store.Destination(), claim) || receipt.VersionID == "" || receipt.VersionID == "null" {
			return receipt, errors.New("invalid manifest custody receipt")
		}
		verified, e := l.remote(func() (storage.CustodyReceipt, error) {
			return l.delivery.store.VerifyVersion(ctx, claim, receipt.VersionID)
		})
		if e != nil {
			return receipt, e
		}
		if verified != receipt {
			return receipt, errors.New("manifest protected version changed")
		}
		return receipt, nil
	}
	if !errors.Is(e, os.ErrNotExist) {
		return receipt, e
	}
	receipt, e = l.remote(func() (storage.CustodyReceipt, error) { return l.delivery.store.CreateExact(ctx, claim, data) })
	if e != nil {
		return receipt, e
	}
	if !receipt.Matches(l.delivery.store.Destination(), claim) || receipt.VersionID == "" || receipt.VersionID == "null" {
		return receipt, errors.New("invalid manifest custody result")
	}
	if e = l.persist(name, receipt); e != nil {
		return receipt, e
	}
	return receipt, nil
}
func (l *closingLedger) pass(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e := l.delivery.service.Err(); e != nil {
		return e
	}
	// Do not freeze a membership set while any sealed raw candidate remains.
	// Active collectors are not in Segments and cannot contribute to a sealed owner.
	raw, e := l.delivery.service.spool.Segments()
	if e != nil {
		return e
	}
	if len(raw) != 0 {
		return ErrClosingPending
	}
	if e = l.discoverUnowned(); e != nil {
		return e
	}
	names, e := l.names()
	if e != nil {
		return e
	}
	for _, name := range names {
		if !strings.HasSuffix(name, ".seal") {
			continue
		}
		id := strings.TrimSuffix(name, ".seal")
		var retained closingClaim
		if e = l.decode(id+".claim", &retained); e == nil {
			if e = l.retire(ctx, id, retained); e != nil {
				return e
			}
			return l.pending()
		} else if !errors.Is(e, os.ErrNotExist) {
			return e
		}
		plan, e := l.freeze(id)
		if e != nil {
			return e
		}
		manifest := closingManifest{Schema: 1, Plan: plan}
		for index, claim := range plan.Parts {
			if e := ctx.Err(); e != nil {
				return remoteDeliveryError{e}
			}
			data, e := l.read(fmt.Sprintf("%s.part-%d", id, index))
			if e != nil {
				return e
			}
			if closingObjectClaim(claim.Name, data) != claim {
				return errors.New("frozen manifest part changed")
			}
			receipt, e := l.protectedObject(ctx, fmt.Sprintf("%s.receipt-%d", id, index), claim, data)
			if e != nil {
				return e
			}
			manifest.Parts = append(manifest.Parts, receipt)
		}
		data, e := json.Marshal(manifest)
		if e != nil {
			return e
		}
		if e = l.persistBytes(id+".final", data); e != nil {
			return e
		}
		claim := closingObjectClaim("manifest-"+id+"-final", data)
		receipt, e := l.remote(func() (storage.CustodyReceipt, error) { return l.delivery.store.CreateExact(ctx, claim, data) })
		if e != nil {
			return e
		}
		if !receipt.Matches(l.delivery.store.Destination(), claim) || receipt.VersionID == "" || receipt.VersionID == "null" {
			return errors.New("invalid final manifest receipt")
		}
		verified, e := l.remote(func() (storage.CustodyReceipt, error) {
			return l.delivery.store.VerifyVersion(ctx, claim, receipt.VersionID)
		})
		if e != nil {
			return e
		}
		if receipt != verified {
			return errors.New("final manifest exact verification mismatch")
		}
		retained = closingClaim{Schema: 1, JournalIncarnation: id, Receipt: receipt}
		if e = l.persist(id+".claim", retained); e != nil {
			return e
		}
		if e = l.retire(ctx, id, retained); e != nil {
			return e
		}
		return l.pending() // one collector per bounded scheduler pass
	}
	return l.pending()
}
func (l *closingLedger) retire(ctx context.Context, id string, claim closingClaim) error {
	if claim.Schema != 1 || claim.JournalIncarnation != id || claim.Complete || claim.Receipt.Claim.Name != "manifest-"+id+"-final" || !claim.Receipt.Matches(l.delivery.store.Destination(), claim.Receipt.Claim) || claim.Receipt.VersionID == "" || claim.Receipt.VersionID == "null" {
		return errors.New("invalid retained terminal claim")
	}
	verified, e := l.remote(func() (storage.CustodyReceipt, error) {
		return l.delivery.store.VerifyVersion(ctx, claim.Receipt.Claim, claim.Receipt.VersionID)
	})
	if e != nil {
		return e
	}
	if verified != claim.Receipt {
		return errors.New("terminal claim version changed")
	}
	inventory, e := l.inventory(id)
	if e != nil {
		return e
	}
	if len(inventory) > 0 {
		var manifest closingManifest
		data, e := l.read(id + ".final")
		if e != nil {
			return e
		}
		if closingObjectClaim(claim.Receipt.Claim.Name, data) != claim.Receipt.Claim {
			return errors.New("terminal claim does not bind frozen manifest")
		}
		if e = json.Unmarshal(data, &manifest); e != nil {
			return e
		}
		frozen := map[string]CustodyInventory{}
		for i, partClaim := range manifest.Plan.Parts {
			data, e := l.read(fmt.Sprintf("%s.part-%d", id, i))
			if e != nil {
				return e
			}
			if closingObjectClaim(partClaim.Name, data) != partClaim {
				return errors.New("retirement part identity changed")
			}
			var part closingPart
			if e = json.Unmarshal(data, &part); e != nil {
				return e
			}
			for _, inv := range part.Inventory {
				frozen[inv.Receipt.Claim.Name] = inv
			}
		}
		for _, inv := range inventory {
			prior, ok := frozen[inv.Receipt.Claim.Name]
			if !ok || !equalClosingJSON(prior, inv) {
				return errors.New("inventory is not a member of retained manifest")
			}
		}
	}
	for _, inv := range inventory {
		name := inv.Receipt.Claim.Name + ".receipt"
		data, e := l.delivery.readControl(name)
		if e != nil {
			return e
		}
		if e = os.Remove(filepath.Join(l.delivery.dir, name)); e != nil {
			return e
		}
		if e = l.delivery.syncControlDir(); e != nil {
			return e
		}
		l.delivery.used -= int64(len(data))
		l.delivery.count--
	}
	names, e := l.names()
	if e != nil {
		return e
	}
	// Keep the seal until last so restart always resumes interrupted retirement.
	sort.SliceStable(names, func(i, j int) bool { return names[i] != id+".seal" && names[j] == id+".seal" })
	for _, name := range names {
		if !strings.HasPrefix(name, id+".") || name == id+".claim" {
			continue
		}
		data, e := l.read(name)
		if e != nil {
			return e
		}
		if e = os.Remove(filepath.Join(l.dir, name)); e != nil {
			return e
		}
		if e = l.syncDir(); e != nil {
			return e
		}
		l.used -= int64(len(data))
		l.entries--
	}
	return nil
}

func equalClosingJSON(a, b any) bool {
	x, e := json.Marshal(a)
	if e != nil {
		return false
	}
	y, e := json.Marshal(b)
	return e == nil && bytes.Equal(x, y)
}
func (l *closingLedger) checkMembership(id string, plan *closingPlan, inventory []CustodyInventory) error {
	if plan.Intent.Registration.Schema != 1 || plan.Intent.Registration.JournalIncarnation != id || plan.Intent.Complete || plan.Intent.Registration.Complete || plan.Seal.Complete {
		return errors.New("invalid close intent identity")
	}
	gaps := map[string]bool{}
	if plan.Seal.Outcome != "sealed" {
		gaps["seal_not_confirmed"] = true
	}
	var baseline, terminal *CorrelationReference
	var previous uint64
	for i, inv := range inventory {
		if !plan.Intent.Registration.Unidentified && inv.Workload != nil && *inv.Workload != plan.Intent.Registration.Workload {
			return errors.New("closing workload differs from inventory")
		}
		if inv.Incomplete || inv.Unidentified {
			gaps["incomplete_or_unidentified_segment"] = true
		}
		if (i == 0 && inv.FirstSequence != 0) || (i > 0 && (previous == ^uint64(0) || inv.FirstSequence != previous+1)) {
			gaps["journal_sequence_gap"] = true
		}
		previous = inv.LastSequence
		for _, ref := range []*CorrelationReference{inv.Baseline, inv.Terminal} {
			if ref == nil {
				continue
			}
			target := &baseline
			if ref.Scope == TerminalScope {
				target = &terminal
			}
			if *target != nil {
				return errors.New("conflicting repeated closing fence")
			}
			*target = ref
			if (!plan.Intent.Registration.Unidentified && ref.Header.Incarnation != plan.Intent.Registration.Workload.ProducerIncarnation) || ref.Header.Loss {
				return errors.New("closing reference producer identity or loss")
			}
			found := false
			for _, candidate := range inventory {
				if ref.FrameSequence >= candidate.FirstSequence && ref.FrameSequence <= candidate.LastSequence {
					found = true
					break
				}
			}
			if !found {
				gaps["referenced_frame_not_in_inventory"] = true
			}
		}
	}
	if len(inventory) == 0 {
		gaps["no_identified_inventory"] = true
	} else if plan.Seal.Outcome == "sealed" && previous != plan.Seal.LastSequence {
		gaps["sealed_sequence_boundary_missing"] = true
	}
	if baseline == nil || terminal == nil {
		gaps["baseline_or_terminal_reference_missing"] = true
	}
	if plan.Intent.Baseline != nil && baseline != nil && !equalClosingJSON(plan.Intent.Baseline, baseline) {
		return errors.New("baseline intent conflicts with inventory")
	}
	if plan.Intent.Terminal != nil && terminal != nil && !equalClosingJSON(plan.Intent.Terminal, terminal) {
		return errors.New("terminal intent conflicts with inventory")
	}
	for gap := range gaps {
		plan.Gaps = append(plan.Gaps, gap)
	}
	sort.Strings(plan.Gaps)
	return nil
}

func (l *closingLedger) pending() error {
	names, e := l.names()
	if e != nil {
		return e
	}
	for _, name := range names {
		if strings.HasSuffix(name, ".seal") {
			return ErrClosingPending
		}
	}
	entries, e := boundedClosingDirectory(l.delivery.dir, l.delivery.config.MaxInventoryEntries+3)
	if e != nil {
		return e
	}
	for _, entry := range entries {
		if !inventoryName(entry.Name()) {
			continue
		}
		data, e := l.delivery.readControl(entry.Name())
		if e != nil {
			return e
		}
		var inv CustodyInventory
		if e = json.Unmarshal(data, &inv); e != nil {
			return e
		}
		if e = l.delivery.validateInventory(entry.Name(), inv); e != nil {
			return e
		}
		if !tokenOK(inv.JournalIncarnation) {
			return ErrClosingPending
		}
	}
	return nil
}

// A crash between the first raw journal write and registration cannot be
// relabelled as an admitted collector. Preserve an explicitly unidentified owner
// only within this already-bound protected spool, without adopting old config.
func (l *closingLedger) discoverUnowned() error {
	entries, e := boundedClosingDirectory(l.delivery.dir, l.delivery.config.MaxInventoryEntries+3)
	if e != nil {
		return e
	}
	for _, entry := range entries {
		if !inventoryName(entry.Name()) {
			continue
		}
		data, e := l.delivery.readControl(entry.Name())
		if e != nil {
			return e
		}
		var inv CustodyInventory
		if e = json.Unmarshal(data, &inv); e != nil {
			return e
		}
		if e = l.delivery.validateInventory(entry.Name(), inv); e != nil {
			return e
		}
		id := inv.JournalIncarnation
		if !tokenOK(id) {
			continue
		} // unparseable bytes cannot invent a journal identity
		if _, e = l.read(id + ".open"); e == nil {
			continue
		} else if !errors.Is(e, os.ErrNotExist) {
			return e
		}
		if _, e = l.read(id + ".claim"); e == nil {
			return errors.New("new inventory appeared after terminal manifest claim")
		} else if !errors.Is(e, os.ErrNotExist) {
			return e
		}
		registration := collectorRegistration{Schema: 1, JournalIncarnation: id, Unidentified: true}
		if e = l.persist(id+".open", registration); e != nil {
			return e
		}
		if e = l.persist(id+".intent", collectorCloseIntent{Registration: registration, Uncertainty: "unregistered_owner_context_unknown"}); e != nil {
			return e
		}
		if e = l.persist(id+".seal", collectorSeal{Outcome: "owner_crashed_seal_unknown"}); e != nil {
			return e
		}
	}
	return nil
}

func boundedClosingDirectory(path string, limit int) ([]os.DirEntry, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	entries, e := f.ReadDir(limit + 1)
	if e != nil && e != io.EOF {
		return nil, e
	}
	if len(entries) > limit {
		return nil, ErrSpoolBudget
	}
	return entries, nil
}

// The scheduler owns progression, but remote latency must not hold the control
// mutex needed by another collector's admission or durable sealing.
func (l *closingLedger) remote(operation func() (storage.CustodyReceipt, error)) (storage.CustodyReceipt, error) {
	l.mu.Unlock()
	receipt, err := operation()
	l.mu.Lock()
	if failure := l.delivery.service.Err(); failure != nil {
		return receipt, failure
	}
	if err != nil {
		return receipt, remoteDeliveryError{err}
	}
	return receipt, nil
}
