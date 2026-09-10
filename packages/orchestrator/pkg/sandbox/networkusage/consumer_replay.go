package networkusage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

type DeviceByteTotal struct{ Device, TXBytes, RXBytes string }
type VerifiedPrefix struct {
	FirstSequence, LastSequence  *uint64
	LowerBaseline, UpperTerminal *CorrelationReference
	Scope                        string
	Devices                      []DeviceByteTotal
}
type consumerReplay struct {
	id                           string
	workload                     WorkloadBinding
	journal                      *Journal
	correlator                   *Correlator
	sink                         consumerCompareSink
	now                          time.Time
	previous, attempt            uint64
	closedPreSequence            uint64
	closed, closedPreKnown       bool
	seen, stopped, terminalFrame bool
	maxDevices                   int
	totals                       map[string][2]uint64
	prefix                       VerifiedPrefix
	baseline, terminal           *CorrelationReference
	requestedFrame               *CorrelationReference
	gaps                         map[string]bool
}

// The sink retains only the single source record being replayed. Existing
// Correlator/Journal transitions regenerate counters and correlation identity;
// no accumulated JSON history or writable source spool is constructed.
type consumerCompareSink struct {
	expected Record
	writes   int
}

func (s *consumerCompareSink) Write(raw []byte) (int, error) {
	var got Record
	if err := decodeConsumerJSON(raw, &got); err != nil {
		return 0, err
	}
	if s.writes != 0 || !reflect.DeepEqual(got, s.expected) {
		return 0, errors.New("retained record differs from producer replay")
	}
	s.writes++
	return len(raw), nil
}
func (*consumerCompareSink) Sync() error  { return nil }
func (*consumerCompareSink) Close() error { return nil }

func newConsumerReplay(id string, workload WorkloadBinding, maxDevices int) *consumerReplay {
	return &consumerReplay{id: id, workload: workload, maxDevices: maxDevices, totals: map[string][2]uint64{}, gaps: map[string]bool{}, prefix: VerifiedPrefix{Scope: "verified_contiguous_activity_prefix"}}
}
func (v *consumerReplay) gap(reason string) { v.stopped = true; v.gaps[reason] = true }
func (v *consumerReplay) record(r Record) error {
	switch r.Kind {
	case "start", "producer_binding", "producer_request", "producer_activity_intent", "producer_frame", "producer_correlation", "gap", "closed":
	default:
		return errors.New("unsupported retained journal kind")
	}
	if r.SchemaVersion != 1 || r.Complete || r.Incarnation != v.id || r.SandboxID != v.workload.SandboxID {
		return errors.New("unsupported or conflicting retained journal identity")
	}
	if r.Workload != nil && *r.Workload != v.workload {
		return errors.New("retained workload substitution")
	}
	if r.Kind != "start" && r.Workload == nil {
		return errors.New("missing retained workload binding")
	}
	if strings.HasPrefix(r.Kind, "producer_") && r.Producer == nil {
		return errors.New("missing retained producer structure")
	}
	if v.closed {
		return errors.New("journal record after close")
	}
	if r.Kind == "closed" {
		v.closed = true
		v.closedPreSequence = v.previous
		v.closedPreKnown = v.seen && v.previous != math.MaxUint64 && r.Sequence == v.previous+1
	}
	if v.seen {
		if r.Sequence <= v.previous {
			return errors.New("duplicate or reordered journal record")
		}
		if v.previous == math.MaxUint64 || r.Sequence != v.previous+1 {
			v.gap("journal_sequence_gap")
		}
	} else if r.Sequence != 0 || r.Kind != "start" {
		v.gap("journal_start_missing")
	}
	v.seen = true
	v.previous = r.Sequence
	var frame *ProducerFrame
	if r.Producer != nil {
		p := r.Producer
		if p.Incarnation != v.workload.ProducerIncarnation {
			return errors.New("producer incarnation substitution")
		}
		if r.Kind == "producer_request" || r.Kind == "producer_correlation" {
			if p.Request == nil || !tokenOK(p.Request.ID) || p.Request.Sequence == 0 || p.Request.Incarnation != v.workload.ProducerIncarnation || (p.Scope != BaselineScope && p.Scope != SampleScope && p.Scope != TerminalScope) {
				return errors.New("invalid retained request structure")
			}
		}
		if p.Raw != nil || p.SHA256 != "" {
			h := sha256.Sum256(p.Raw)
			if p.Bytes != len(p.Raw) || p.SHA256 != hex.EncodeToString(h[:]) {
				return errors.New("producer source bytes substituted")
			}
		}
		switch r.Kind {
		case "producer_binding":
			if !equalClosingJSON(p, &ProducerEvidence{Incarnation: v.workload.ProducerIncarnation}) {
				return errors.New("invalid binding evidence shape")
			}
		case "producer_activity_intent":
			if !equalClosingJSON(p, &ProducerEvidence{Incarnation: v.workload.ProducerIncarnation, Scope: SampleScope}) {
				return errors.New("invalid activity evidence shape")
			}
		case "producer_request":
			if !equalClosingJSON(p, &ProducerEvidence{Incarnation: v.workload.ProducerIncarnation, Request: p.Request, Scope: p.Scope}) {
				return errors.New("invalid request evidence shape")
			}
		case "producer_frame":
			if p.Request != nil || p.FrameSequence != 0 || p.FrameSHA256 != "" || p.FrameBytes != 0 {
				return errors.New("invalid frame evidence shape")
			}
			f, err := DecodeProducerFrame(p.Raw)
			if err != nil {
				return err
			}
			if p.Header == nil || !headersEqual(f.Header, *p.Header) || f.Header.Incarnation != v.workload.ProducerIncarnation || f.Cutoff != p.Cutoff {
				return errors.New("frame header substitution")
			}
			if f.DeviceMetricsPresent {
				if !f.NetworkPresent || p.NetworkScope != "device_aggregate" {
					return errors.New("invalid retained device scope")
				}
			} else if p.NetworkScope != "preboot_no_devices" || f.Tx != 0 || f.Rx != 0 {
				return errors.New("invalid retained preboot scope")
			}
			if (f.Header.RequestID == nil && p.Scope != "") || (f.Header.RequestID != nil && p.Scope != BaselineScope && p.Scope != SampleScope && p.Scope != TerminalScope) || (f.Cutoff == TerminalCutoff) != (p.Scope == TerminalScope) {
				return errors.New("invalid retained requested-frame scope")
			}
			if f.Header.Loss || f.Header.Attempt <= v.attempt || v.terminalFrame {
				return errors.New("duplicate, loss, or post-terminal frame")
			}
			if v.attempt == math.MaxUint64 || f.Header.Attempt != v.attempt+1 {
				v.gap("producer_attempt_gap")
			}
			v.attempt = f.Header.Attempt
			v.terminalFrame = f.Cutoff == TerminalCutoff
			if f.Header.RequestID != nil {
				v.requestedFrame = &CorrelationReference{Header: f.Header, FrameSequence: r.Sequence, FrameSHA256: p.SHA256, FrameBytes: p.Bytes}
			}
			frame = &f
		case "producer_correlation":
			if p.NetworkScope != "" || p.FrameSequence >= r.Sequence || p.FrameBytes < 1 || p.FrameBytes > MaxProducerFrameBytes || len(p.FrameSHA256) != 64 {
				return errors.New("invalid correlation frame shape")
			}
			if _, err := hex.DecodeString(p.FrameSHA256); err != nil {
				return err
			}
			receipt, err := DecodeProducerReceipt(p.Raw, p.Scope == TerminalScope)
			if err != nil {
				return err
			}
			if p.Header == nil || !headersEqual(receipt.Header, *p.Header) || receipt.Cutoff != p.Cutoff || p.Request == nil || p.Request.Incarnation != v.workload.ProducerIncarnation || *receipt.Header.RequestID != p.Request.ID || *receipt.Header.RequestSequence != p.Request.Sequence {
				return errors.New("receipt header substitution")
			}
			if v.requestedFrame != nil {
				f := v.requestedFrame
				if !headersEqual(f.Header, *p.Header) || f.FrameSequence != p.FrameSequence || f.FrameSHA256 != p.FrameSHA256 || f.FrameBytes != p.FrameBytes {
					return errors.New("correlation substitutes observed frame")
				}
				v.requestedFrame = nil
			} else {
				v.gap("correlation_source_frame_missing")
			}
		}
	}
	if r.Kind == "producer_frame" && frame == nil {
		return errors.New("missing producer frame")
	}
	if r.Kind == "producer_correlation" {
		ref := correlationReference(r)
		if ref != nil {
			if err := ref.validate(); err != nil {
				return err
			}
			target := &v.baseline
			if ref.Scope == TerminalScope {
				target = &v.terminal
			}
			if *target != nil {
				return errors.New("duplicate retained fence")
			}
			*target = ref
		}
	}
	if r.Kind == "gap" || !r.Valid {
		v.gap("invalid_journal_record")
	}
	parsed, err := time.Parse(time.RFC3339Nano, r.ObservedAt)
	if err != nil {
		return errors.New("invalid journal timestamp")
	}
	var devices map[string][2]uint64
	if frame != nil {
		devices, err = consumerDeviceDeltas(*frame, v.maxDevices)
		if err != nil {
			return err
		}
	}
	if v.stopped {
		return nil
	}
	v.sink = consumerCompareSink{expected: r}
	v.now = parsed
	switch r.Kind {
	case "start":
		if v.journal != nil {
			return errors.New("duplicate journal start")
		}
		v.journal = &Journal{file: &v.sink, now: func() time.Time { return v.now }, last: Record{SchemaVersion: 1, SandboxID: r.SandboxID, Incarnation: v.id, Valid: true}}
		initial := v.journal.last
		initial.Kind = "start"
		err = v.journal.append(initial)
	case "producer_binding":
		if v.journal == nil || r.Producer == nil {
			return errors.New("binding before journal")
		}
		v.journal.workload = &v.workload
		v.correlator, err = NewCorrelator(v.journal, v.workload.ProducerIncarnation)
	default:
		if v.correlator == nil {
			return errors.New("producer record before binding")
		}
		switch r.Kind {
		case "producer_request":
			if r.Producer == nil || r.Producer.Request == nil {
				return errors.New("missing producer request")
			}
			_, err = v.correlator.Begin(r.Producer.Request.ID, r.Producer.Scope)
		case "producer_activity_intent":
			err = v.correlator.BeginActivity()
		case "producer_frame":
			err = v.correlator.ObserveFrame(r.Producer.Raw)
		case "producer_correlation":
			if r.Producer == nil {
				return errors.New("missing producer correlation")
			}
			// API receipt arrival before its frame has no independent journal
			// record. Its exact retained bytes are replayed at the correlation.
			err = v.correlator.ObserveReceipt(r.Producer.Raw)
		case "closed":
			err = v.correlator.Close()
		default:
			return fmt.Errorf("unsupported retained record kind %s", r.Kind)
		}
	}
	if err != nil {
		return err
	}
	if v.sink.writes != 1 {
		return errors.New("retained transition did not produce exactly one record")
	}
	if frame != nil {
		if v.correlator.activity {
			if v.prefix.FirstSequence == nil {
				sequence := r.Sequence
				v.prefix.FirstSequence = &sequence
				v.prefix.LowerBaseline = v.baseline
			}
			for device, delta := range devices {
				prior, exists := v.totals[device]
				if !exists && len(v.totals) >= v.maxDevices {
					return errors.New("device identity budget")
				}
				if delta[0] > math.MaxUint64-prior[0] || delta[1] > math.MaxUint64-prior[1] {
					return errors.New("device total overflow")
				}
				v.totals[device] = [2]uint64{prior[0] + delta[0], prior[1] + delta[1]}
			}
			sequence := r.Sequence
			v.prefix.LastSequence = &sequence
		}
	}
	if r.Kind == "producer_correlation" && r.Producer.Scope == TerminalScope {
		v.prefix.UpperTerminal = v.terminal
	}
	return nil
}

func consumerDeviceDeltas(frame ProducerFrame, limit int) (map[string][2]uint64, error) {
	metrics, err := object(frame.Metrics)
	if err != nil {
		return nil, err
	}
	result := map[string][2]uint64{}
	var tx, rx uint64
	for key, raw := range metrics {
		if !strings.HasPrefix(key, "net_") {
			continue
		}
		if len(result) >= limit || len(key) > 256 {
			return nil, errors.New("per-frame device budget")
		}
		device, err := object(raw)
		if err != nil {
			return nil, err
		}
		var x, y uint64
		if err := required(device, "tx_bytes_count", &x); err != nil {
			return nil, err
		}
		if err := required(device, "rx_bytes_count", &y); err != nil {
			return nil, err
		}
		if x > math.MaxUint64-tx || y > math.MaxUint64-rx {
			return nil, errors.New("per-flush device overflow")
		}
		tx += x
		rx += y
		result[key] = [2]uint64{x, y}
	}
	if frame.DeviceMetricsPresent && (tx != frame.Tx || rx != frame.Rx) {
		return nil, errors.New("device aggregate differs from per-device deltas")
	}
	return result, nil
}
func (v *consumerReplay) finish() VerifiedPrefix {
	for device, total := range v.totals {
		v.prefix.Devices = append(v.prefix.Devices, DeviceByteTotal{Device: device, TXBytes: strconv.FormatUint(total[0], 10), RXBytes: strconv.FormatUint(total[1], 10)})
	}
	sort.Slice(v.prefix.Devices, func(i, j int) bool { return v.prefix.Devices[i].Device < v.prefix.Devices[j].Device })
	return v.prefix
}

func consumerRecordLines(raw []byte, maxRecord int, consume func(Record) error) (partial bool, err error) {
	for len(raw) > 0 {
		index := bytes.IndexByte(raw, '\n')
		if index < 0 {
			if len(raw) > maxRecord {
				return false, errors.New("partial journal record budget")
			}
			return true, nil
		}
		if index > maxRecord {
			return false, errors.New("raw journal record budget")
		}
		var record Record
		if err := decodeConsumerJSON(raw[:index], &record); err != nil {
			return false, err
		}
		if err := consume(record); err != nil {
			return false, err
		}
		raw = raw[index+1:]
	}
	return false, nil
}

func preflightConsumerRecords(ctx context.Context, raw []byte, maxRecord int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for len(raw) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		length := bytes.IndexByte(raw, '\n')
		if length < 0 {
			if len(raw) > maxRecord {
				return errors.New("partial journal record budget")
			}
			return nil
		}
		if length > maxRecord {
			return errors.New("raw journal record budget before decode")
		}
		raw = raw[length+1:]
	}
	return ctx.Err()
}
