package networkusage

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/e2b-dev/infra/packages/shared/pkg/storage"
)

func (r *CorrelationReference) validate() error {
	if r == nil {
		return nil
	}
	header, e := json.Marshal(r.Header)
	if e != nil {
		return e
	}
	if _, e = decodeHeader(header); e != nil {
		return e
	}
	if r.Header.Loss || r.Header.RequestID == nil || r.Header.RequestSequence == nil || r.FrameSequence >= r.CorrelationSequence || r.FrameBytes < 1 || r.FrameBytes > MaxProducerFrameBytes || r.ReceiptBytes < 1 || r.ReceiptBytes > 2048 {
		return errors.New("invalid retained fence reference")
	}
	if (r.Scope == TerminalScope && r.Cutoff != TerminalCutoff) || (r.Scope == BaselineScope && r.Cutoff != "") || (r.Scope != BaselineScope && r.Scope != TerminalScope) {
		return errors.New("invalid retained fence scope")
	}
	for _, hash := range []string{r.FrameSHA256, r.ReceiptSHA256} {
		if len(hash) != 64 {
			return errors.New("invalid retained fence hash")
		}
		if _, e = hex.DecodeString(hash); e != nil {
			return e
		}
	}
	return nil
}
func (l *closingLedger) validateControl(name string) error {
	id, suffix, _ := strings.Cut(name, ".")
	var value any
	switch suffix {
	case "open":
		value = &collectorRegistration{}
	case "intent":
		value = &collectorCloseIntent{}
	case "seal":
		value = &collectorSeal{}
	case "plan":
		value = &closingPlan{}
	case "final":
		value = &closingManifest{}
	case "claim":
		value = &closingClaim{}
	default:
		if strings.HasPrefix(suffix, "part-") {
			value = &closingPart{}
		} else if strings.HasPrefix(suffix, "receipt-") {
			value = &storage.CustodyReceipt{}
		} else {
			return errors.New("invalid control suffix")
		}
	}
	if e := l.decode(name, value); e != nil {
		return e
	}
	switch v := value.(type) {
	case *collectorRegistration:
		return validateRegistration(id, *v)
	case *collectorCloseIntent:
		return validateIntent(id, *v)
	case *collectorSeal:
		return validateSeal(*v)
	case *closingPlan:
		return l.validatePlan(id, *v)
	case *closingManifest:
		if v.Schema != 1 || v.Complete || len(v.Parts) != len(v.Plan.Parts) {
			return errors.New("invalid final manifest shape")
		}
		if e := l.validatePlan(id, v.Plan); e != nil {
			return e
		}
		for i, r := range v.Parts {
			if !validClosingReceipt(r, l.delivery.store.Destination(), v.Plan.Parts[i].Name) || r.Claim != v.Plan.Parts[i] {
				return errors.New("invalid final manifest part receipt")
			}
		}
	case *closingClaim:
		if v.Schema != 1 || v.JournalIncarnation != id || v.Complete || !validClosingReceipt(v.Receipt, l.delivery.store.Destination(), "manifest-"+id+"-final") {
			return errors.New("invalid retained terminal claim")
		}
	case *closingPart:
		if v.Schema != 1 || v.JournalIncarnation != id || v.Complete || v.Index < 0 || v.Index >= l.options.MaxParts || suffix != fmt.Sprintf("part-%d", v.Index) || len(v.Inventory) > l.delivery.config.MaxInventoryEntries {
			return errors.New("invalid closing part")
		}
		for _, inv := range v.Inventory {
			if inv.JournalIncarnation != id {
				return errors.New("mixed closing part owner")
			}
			if e := l.delivery.validateInventory(inv.Receipt.Claim.Name+".receipt", inv); e != nil {
				return e
			}
		}
	case *storage.CustodyReceipt:
		if !validClosingReceipt(*v, l.delivery.store.Destination(), "manifest-"+id+"-part-"+strings.TrimPrefix(suffix, "receipt-")) {
			return errors.New("invalid manifest part receipt")
		}
	}
	return nil
}
func validClosingReceipt(r storage.CustodyReceipt, d storage.CustodyDestination, name string) bool {
	return r.Claim.Name == name && r.VersionID != "" && r.VersionID != "null" && r.Matches(d, r.Claim)
}
func validateRegistration(id string, r collectorRegistration) error {
	if r.Unidentified {
		if r.Schema == 1 && r.JournalIncarnation == id && !r.Complete && r.Workload == (WorkloadBinding{}) {
			return nil
		}
		return errors.New("invalid unidentified registration")
	}
	if r.Schema != 1 || r.JournalIncarnation != id || r.Complete || !tokenOK(r.Workload.ProducerIncarnation) {
		return errors.New("invalid closing registration")
	}
	return r.Workload.validate(r.Workload.SandboxID)
}
func validateIntent(id string, intent collectorCloseIntent) error {
	if intent.Complete || len(intent.Uncertainty) > 80 {
		return errors.New("invalid close intent")
	}
	return errors.Join(validateRegistration(id, intent.Registration), validateFencePair(intent.Baseline, intent.Terminal))
}

func validateFencePair(baseline, terminal *CorrelationReference) error {
	if (baseline != nil && baseline.Scope != BaselineScope) || (terminal != nil && terminal.Scope != TerminalScope) {
		return errors.New("retained fence stored under wrong scope")
	}
	return errors.Join(baseline.validate(), terminal.validate())
}
func validateSeal(seal collectorSeal) error {
	if seal.Complete || (seal.Outcome != "sealed" && seal.Outcome != "seal_failed" && seal.Outcome != "owner_crashed_seal_unknown") {
		return errors.New("invalid sealing outcome")
	}
	return nil
}
func (l *closingLedger) validatePlan(id string, plan closingPlan) error {
	if plan.Complete || len(plan.Parts) < 1 || len(plan.Parts) > l.options.MaxParts || len(plan.Gaps) > 8 {
		return errors.New("invalid frozen manifest plan")
	}
	if e := errors.Join(validateIntent(id, plan.Intent), validateSeal(plan.Seal)); e != nil {
		return e
	}
	for i, claim := range plan.Parts {
		if claim.Name != fmt.Sprintf("manifest-%s-part-%d", id, i) || claim.Bytes > int64(l.options.PartBytes) {
			return errors.New("invalid frozen part claim")
		}
		if _, e := l.delivery.store.Destination().ObjectKey(claim); e != nil {
			return e
		}
	}
	for _, gap := range plan.Gaps {
		if len(gap) > 80 {
			return errors.New("invalid manifest gap")
		}
	}
	return nil
}
