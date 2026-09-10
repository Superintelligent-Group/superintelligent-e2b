package networkusage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"
)

func producerFixtures(t *testing.T) ([][]byte, [][]byte) {
	t.Helper()
	read := func(name string) [][]byte {
		data, e := os.ReadFile("testdata/" + name)
		if e != nil {
			t.Fatal(e)
		}
		lines := bytes.SplitAfter(data, []byte{'\n'})
		return lines[:len(lines)-1]
	}
	return read("sup909-producer.jsonl"), read("sup909-receipts.jsonl")
}
func newTestCorrelator(t *testing.T) (*Correlator, *faultFile) {
	t.Helper()
	j, f := memoryJournal()
	c, e := NewCorrelator(j, "sup909_local_boot")
	if e != nil {
		t.Fatal(e)
	}
	return c, f
}
func mustBegin(t *testing.T, c *Correlator, id string, scope RequestScope) ProducerRequest {
	t.Helper()
	r, e := c.Begin(id, scope)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func mustObserve(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func baseline(t *testing.T, c *Correlator, frames, receipts [][]byte) {
	t.Helper()
	mustBegin(t, c, "baseline", BaselineScope)
	mustObserve(t, c.ObserveFrame(frames[0]))
	mustObserve(t, c.ObserveReceipt(receipts[0]))
	f, e := c.Fence()
	if e != nil || f == nil {
		t.Fatal(f, e)
	}
}
func traffic(t *testing.T, c *Correlator, frames, receipts [][]byte) {
	t.Helper()
	baseline(t, c, frames, receipts)
	mustObserve(t, c.BeginActivity())
	mustObserve(t, c.ObserveFrame(frames[1]))
	mustBegin(t, c, "traffic", SampleScope)
	mustObserve(t, c.ObserveFrame(frames[2]))
	mustObserve(t, c.ObserveReceipt(receipts[1]))
}

func TestProducerPinnedFramesBothOrdersAndDurableRaw(t *testing.T) {
	frames, receipts := producerFixtures(t)
	for _, responseFirst := range []bool{false, true} {
		c, file := newTestCorrelator(t)
		mustBegin(t, c, "baseline", BaselineScope)
		if responseFirst {
			mustObserve(t, c.ObserveReceipt(receipts[0]))
		} else {
			mustObserve(t, c.ObserveFrame(frames[0]))
		}
		f, e := c.Fence()
		if e != nil || f != nil {
			t.Fatal("premature fence", f, e)
		}
		if responseFirst {
			mustObserve(t, c.ObserveFrame(frames[0]))
		} else {
			mustObserve(t, c.ObserveReceipt(receipts[0]))
		}
		mustObserve(t, c.BeginActivity())
		mustObserve(t, c.ObserveFrame(frames[1]))
		mustBegin(t, c, "traffic", SampleScope)
		mustObserve(t, c.ObserveFrame(frames[2]))
		mustObserve(t, c.ObserveReceipt(receipts[1]))
		req := mustBegin(t, c, "terminal", TerminalScope)
		if req.Sequence != 3 {
			t.Fatal(req)
		}
		mustObserve(t, c.ObserveReceipt(receipts[2]))
		mustObserve(t, c.ObserveFrame(frames[3]))
		f, e = c.Fence()
		if e != nil || f == nil || f.Cutoff != TerminalCutoff || f.Header.Attempt != 4 {
			t.Fatal(f, e)
		}
		if e := c.BeginActivity(); e == nil {
			t.Fatal("activity after terminal accepted")
		}
		before := file.writes
		retry := mustBegin(t, c, "terminal", TerminalScope)
		if retry != req {
			t.Fatal(retry, req)
		}
		mustObserve(t, c.ObserveReceipt(receipts[2]))
		if file.writes != before {
			t.Fatal("retry rewrote evidence")
		}
		rs := records(t, file.Bytes())
		rawIndex := 0
		for _, r := range rs {
			if r.Complete {
				t.Fatal("complete claim")
			}
			if r.Kind == "producer_frame" {
				if !bytes.Equal(r.Producer.Raw, frames[rawIndex]) {
					t.Fatal("raw changed")
				}
				h := sha256.Sum256(frames[rawIndex])
				if r.Producer.SHA256 != hex.EncodeToString(h[:]) || r.Producer.Bytes != len(frames[rawIndex]) {
					t.Fatal("raw hash/size")
				}
				if rawIndex == 0 && r.Producer.NetworkScope != "preboot_no_devices" {
					t.Fatal(r)
				}
				rawIndex++
			}
			if r.Kind == "producer_correlation" {
				h := sha256.Sum256(r.Producer.Raw)
				if r.Producer.SHA256 != hex.EncodeToString(h[:]) || r.Producer.FrameSequence == 0 || r.Producer.FrameSHA256 == "" || r.Producer.FrameBytes == 0 {
					t.Fatal("bad correlation references")
				}
			}
		}
		if rawIndex != 4 || rs[len(rs)-1].TxBytes != 5904 {
			t.Fatal(rawIndex, rs[len(rs)-1])
		}
	}
}

func TestProducerRequestOwnershipAndExplicitActivity(t *testing.T) {
	frames, receipts := producerFixtures(t)
	c, _ := newTestCorrelator(t)
	if _, e := NewCorrelator(nil, "epoch"); e == nil {
		t.Fatal("nil journal accepted")
	}
	if _, e := c.Begin("early", SampleScope); e == nil {
		t.Fatal("sample before baseline")
	}
	if e := c.BeginActivity(); e == nil {
		t.Fatal("activity before baseline")
	}
	req := mustBegin(t, c, "baseline", BaselineScope)
	if retry := mustBegin(t, c, "baseline", BaselineScope); retry != req {
		t.Fatal("retry identity")
	}
	if _, e := c.Begin("other", BaselineScope); e == nil {
		t.Fatal("multiple pending")
	}
	if e := c.journal.Observe(1, 1); e == nil {
		t.Fatal("legacy Observe bypass")
	}
	if e := c.journal.Gap("external"); e == nil {
		t.Fatal("legacy Gap bypass")
	}
	mustObserve(t, c.ObserveFrame(frames[0]))
	mustObserve(t, c.ObserveReceipt(receipts[0]))
	if _, e := c.Begin("traffic", SampleScope); e == nil {
		t.Fatal("sample before activity intent")
	}
	// Slow preboot configuration: unsolicited no-device frame stays explicit,
	// including after baseline acknowledgment and before BeginActivity.
	periodic := bytes.Replace(frames[0], []byte(`"attempt_sequence":1`), []byte(`"attempt_sequence":2`), 1)
	periodic = bytes.Replace(periodic, []byte(`"request_sequence":1`), []byte(`"request_sequence":null`), 1)
	periodic = bytes.Replace(periodic, []byte(`"request_id":"baseline"`), []byte(`"request_id":null`), 1)
	mustObserve(t, c.ObserveFrame(periodic))
	mustObserve(t, c.BeginActivity())
	periodic = bytes.Replace(periodic, []byte(`"attempt_sequence":2`), []byte(`"attempt_sequence":3`), 1)
	if e := c.ObserveFrame(periodic); e == nil {
		t.Fatal("no devices after activity accepted")
	}
	if f, e := c.Fence(); e == nil || f != nil {
		t.Fatal("fence after gap")
	}
}

func TestProducerTerminalFrameClosesStreamBeforeResponse(t *testing.T) {
	frames, receipts := producerFixtures(t)
	c, _ := newTestCorrelator(t)
	traffic(t, c, frames, receipts)
	mustBegin(t, c, "terminal", TerminalScope)
	mustObserve(t, c.ObserveFrame(frames[3]))
	periodic := bytes.Replace(frames[1], []byte(`"attempt_sequence":2`), []byte(`"attempt_sequence":5`), 1)
	if e := c.ObserveFrame(periodic); e == nil {
		t.Fatal("postterminal frame accepted")
	}
	if e := c.ObserveReceipt(receipts[2]); e == nil {
		t.Fatal("receipt erased gap")
	}
	if f, e := c.Fence(); e == nil || f != nil {
		t.Fatal("postterminal fence")
	}
}

func TestProducerDurabilityFailuresNeverAcknowledge(t *testing.T) {
	frames, receipts := producerFixtures(t)
	for _, stage := range []string{"intent", "frame", "correlation", "activity"} {
		for _, fault := range []string{"write", "short", "sync"} {
			t.Run(stage+fault, func(t *testing.T) {
				c, file := newTestCorrelator(t)
				if stage != "intent" {
					mustBegin(t, c, "baseline", BaselineScope)
				}
				if stage == "correlation" || stage == "activity" {
					mustObserve(t, c.ObserveFrame(frames[0]))
				}
				if stage == "activity" {
					mustObserve(t, c.ObserveReceipt(receipts[0]))
				}
				switch fault {
				case "write":
					file.writeErr = errors.New("disk write")
				case "short":
					file.short = true
				case "sync":
					file.syncErr = errors.New("disk sync")
				}
				var e error
				switch stage {
				case "intent":
					_, e = c.Begin("baseline", BaselineScope)
				case "frame":
					e = c.ObserveFrame(frames[0])
				case "correlation":
					e = c.ObserveReceipt(receipts[0])
				case "activity":
					e = c.BeginActivity()
				}
				if e == nil {
					t.Fatal("fault accepted")
				}
				if f, e := c.Fence(); f != nil || e == nil {
					t.Fatal("fence after durability failure")
				}
				file.short = false
				file.writeErr = nil
				file.syncErr = nil
				if e = c.ObserveReceipt(receipts[0]); e == nil {
					t.Fatal("repaired failed journal")
				}
			})
		}
	}
}

func TestProducerParsingRejectsAmbiguityAndRequiredFields(t *testing.T) {
	frames, receipts := producerFixtures(t)
	invalid := [][]byte{
		bytes.TrimSuffix(frames[0], []byte{'\n'}), append(bytes.Clone(frames[0]), frames[0]...),
		bytes.Replace(frames[0], []byte(`"loss_detected":false`), []byte(`"loss_detected":false,"loss_detected":true`), 1),
		bytes.Replace(frames[0], []byte(`"loss_detected":false`), []byte(`"unknown":false`), 1),
		bytes.Replace(frames[0], []byte(`"attempt_sequence":1`), []byte(`"attempt_sequence":18446744073709551616`), 1),
		bytes.Replace(frames[0], []byte(`"request_sequence":1`), []byte(`"request_sequence":null`), 1),
		bytes.Replace(frames[0], []byte(`"tx_bytes_count":0`), []byte(`"tx_bytes_count":-1`), 1),
		bytes.Replace(frames[0], []byte(`"tx_bytes_count":0`), []byte(`"tx_bytes_count":0.1`), 1),
		bytes.Replace(frames[0], []byte(`"schema"`), []byte{'"', 0xff, '"'}, 1),
	}
	for i, raw := range invalid {
		if _, e := DecodeProducerFrame(raw); e == nil {
			t.Fatalf("invalid frame %d accepted", i)
		}
	}
	for _, field := range []string{"schema", "incarnation", "attempt_sequence", "request_sequence", "request_id", "loss_detected"} {
		var obj map[string]json.RawMessage
		json.Unmarshal(receipts[0], &obj)
		delete(obj, field)
		raw, _ := json.Marshal(obj)
		if _, e := DecodeProducerReceipt(raw, false); e == nil {
			t.Fatal("missing", field)
		}
	}
	big := bytes.Replace(frames[2], []byte(`"tx_bytes_count":1086`), []byte(`"tx_bytes_count":9007199254740993`), -1)
	f, e := DecodeProducerFrame(big)
	if e != nil || f.Tx != 9007199254740993 {
		t.Fatal("u64 precision", f.Tx, e)
	}
}

func TestProducerGapsPreserveBoundedRawAndInvalidate(t *testing.T) {
	frames, receipts := producerFixtures(t)
	cases := [][]byte{
		bytes.TrimSuffix(frames[0], []byte{'\n'}),
		bytes.Replace(frames[0], []byte(`"loss_detected":false`), []byte(`"loss_detected":true`), 1),
		bytes.Replace(frames[0], []byte(`"attempt_sequence":1`), []byte(`"attempt_sequence":2`), 1),
		bytes.Replace(frames[0], []byte(`sup909_local_boot`), []byte(`foreign`), 1),
	}
	for _, raw := range cases {
		c, file := newTestCorrelator(t)
		mustBegin(t, c, "baseline", BaselineScope)
		if e := c.ObserveFrame(raw); e == nil {
			t.Fatal("invalid frame accepted")
		}
		rs := records(t, file.Bytes())
		last := rs[len(rs)-1]
		if last.Valid || last.Complete || !bytes.Equal(last.Producer.Raw, raw) {
			t.Fatal("gap evidence lost")
		}
		if e := c.ObserveReceipt(receipts[0]); e == nil {
			t.Fatal("receipt repaired gap")
		}
	}
	c, _ := newTestCorrelator(t)
	baseline(t, c, frames, receipts)
	if e := c.ObserveFrame(frames[0]); e == nil {
		t.Fatal("duplicate accepted")
	}
}

func TestProducerSpoolBudgetAndRawRoundTrip(t *testing.T) {
	frames, receipts := producerFixtures(t)
	for _, segmentBytes := range []int64{500, 50000} {
		spool, e := OpenSpool(t.TempDir(), SpoolOptions{MaxBytes: 200000, SegmentBytes: segmentBytes, MaxSegments: 20})
		if e != nil {
			t.Fatal(e)
		}
		j, e := OpenWithSpool(spool, "sandbox")
		if e != nil {
			t.Fatal(e)
		}
		c, e := NewCorrelator(j, "sup909_local_boot")
		if e != nil {
			t.Fatal(e)
		}
		mustBegin(t, c, "baseline", BaselineScope)
		e = c.ObserveFrame(frames[0])
		if segmentBytes == 500 {
			if e == nil {
				t.Fatal("base64 frame escaped spool budget")
			}
			if f, e := c.Fence(); e == nil || f != nil {
				t.Fatal("budget fence")
			}
		} else {
			mustObserve(t, e)
			mustObserve(t, c.ObserveReceipt(receipts[0]))
			if f, e := c.Fence(); e != nil || f == nil {
				t.Fatal(f, e)
			}
		}
		c.Close()
		segments, e := spool.Segments()
		if e != nil {
			t.Fatal(e)
		}
		if len(segments) == 0 {
			t.Fatal("missing durable spool segments")
		}
		spool.Close()
	}
}

func TestProducerConcurrentCallbacksAndFenceIsolation(t *testing.T) {
	frames, receipts := producerFixtures(t)
	for i := 0; i < 30; i++ {
		c, _ := newTestCorrelator(t)
		mustBegin(t, c, "baseline", BaselineScope)
		errs := make(chan error, 2)
		go func() { errs <- c.ObserveFrame(frames[0]) }()
		go func() { errs <- c.ObserveReceipt(receipts[0]) }()
		mustObserve(t, <-errs)
		mustObserve(t, <-errs)
		fence, e := c.Fence()
		if e != nil || fence == nil {
			t.Fatal(fence, e)
		}
		*fence.Header.RequestID = "caller_mutation"
		next, e := c.Fence()
		if e != nil || *next.Header.RequestID != "baseline" {
			t.Fatal("mutable fence alias")
		}
	}
}

func TestProducerReceiptConflictsAndCounterOverflow(t *testing.T) {
	frames, receipts := producerFixtures(t)
	for _, field := range []string{"attempt_sequence", "request_sequence", "request_id", "incarnation", "loss_detected"} {
		t.Run(field, func(t *testing.T) {
			c, _ := newTestCorrelator(t)
			mustBegin(t, c, "baseline", BaselineScope)
			mustObserve(t, c.ObserveFrame(frames[0]))
			var obj map[string]json.RawMessage
			json.Unmarshal(receipts[0], &obj)
			replacements := map[string]string{"attempt_sequence": "9", "request_sequence": "2", "request_id": `"other"`, "incarnation": `"foreign"`, "loss_detected": "true"}
			obj[field] = json.RawMessage(replacements[field])
			raw, _ := json.Marshal(obj)
			if e := c.ObserveReceipt(raw); e == nil {
				t.Fatal("conflicting header accepted")
			}
			if f, e := c.Fence(); e == nil || f != nil {
				t.Fatal("conflict fence")
			}
		})
	}
	c, _ := newTestCorrelator(t)
	traffic(t, c, frames, receipts)
	c.journal.last.TxBytes = ^uint64(0)
	mustBegin(t, c, "terminal", TerminalScope)
	if e := c.ObserveFrame(frames[3]); e == nil {
		t.Fatal("counter overflow accepted")
	}
	if f, e := c.Fence(); e == nil || f != nil {
		t.Fatal("overflow fence")
	}
	c, _ = newTestCorrelator(t)
	baseline(t, c, frames, receipts)
	mustObserve(t, c.BeginActivity())
	c.requestSequence = ^uint64(0)
	if _, e := c.Begin("overflow", SampleScope); e == nil {
		t.Fatal("request wrapped")
	}
	c, _ = newTestCorrelator(t)
	mustBegin(t, c, "baseline", BaselineScope)
	c.attempt = ^uint64(0)
	if e := c.ObserveFrame(frames[0]); e == nil {
		t.Fatal("attempt wrapped")
	}
}

func TestProducerBoundsUnknownFieldsAndAbsentPreboot(t *testing.T) {
	frames, _ := producerFixtures(t)
	var top map[string]json.RawMessage
	json.Unmarshal(frames[0], &top)
	top["metrics"] = json.RawMessage(`{"future_metric":{"safe":"ok"}}`)
	raw, _ := json.Marshal(top)
	raw = append(raw, '\n')
	c, file := newTestCorrelator(t)
	mustBegin(t, c, "baseline", BaselineScope)
	mustObserve(t, c.ObserveFrame(raw))
	rs := records(t, file.Bytes())
	if rs[len(rs)-1].Producer.NetworkScope != "preboot_no_devices" {
		t.Fatal("absence became observed zero")
	}
	// Unknown metrics are accepted, unknown protocol fields are rejected.
	top["unexpected"] = json.RawMessage(`1`)
	raw, _ = json.Marshal(top)
	raw = append(raw, '\n')
	if _, e := DecodeProducerFrame(raw); e == nil {
		t.Fatal("unknown envelope")
	}
	delete(top, "unexpected")
	top["metrics"] = json.RawMessage(`{"future":"\ud800"}`)
	raw, _ = json.Marshal(top)
	raw = append(raw, '\n')
	if _, e := DecodeProducerFrame(raw); e == nil {
		t.Fatal("surrogate repaired")
	}
	top["metrics"] = json.RawMessage(`{"padding":""}`)
	base, _ := json.Marshal(top)
	padding := MaxProducerFrameBytes - 1 - len(base)
	large := bytes.Replace(base, []byte(`"padding":""`), append(append([]byte(`"padding":"`), bytes.Repeat([]byte{'x'}, padding)...), '"'), 1)
	large = append(large, '\n')
	if len(large) != MaxProducerFrameBytes {
		t.Fatal(len(large))
	}
	if _, e := DecodeProducerFrame(large); e != nil {
		t.Fatal("producer limit rejected", e)
	}
	large = append(large[:len(large)-1], ' ', '\n')
	if _, e := DecodeProducerFrame(large); e == nil {
		t.Fatal("oversize accepted")
	}
}

func TestProducerCloseAndExternalJournalFailureInvalidateFence(t *testing.T) {
	frames, receipts := producerFixtures(t)
	c, _ := newTestCorrelator(t)
	mustBegin(t, c, "baseline", BaselineScope)
	if e := c.Close(); e == nil {
		t.Fatal("pending close succeeded")
	}
	if f, e := c.Fence(); e == nil || f != nil {
		t.Fatal("closed fence")
	}
	c, _ = newTestCorrelator(t)
	baseline(t, c, frames, receipts)
	mustObserve(t, c.journal.Close())
	if f, e := c.Fence(); e == nil || f != nil {
		t.Fatal("external close fence")
	}
	c, file := newTestCorrelator(t)
	mustBegin(t, c, "baseline", BaselineScope)
	c.journal.now = func() time.Time { return time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC) }
	if e := c.ObserveFrame(frames[0]); e == nil {
		t.Fatal("clock regression")
	}
	rs := records(t, file.Bytes())
	if rs[len(rs)-1].Valid {
		t.Fatal("clock invalidity erased")
	}
}

func TestProducerFixtureProvenance(t *testing.T) {
	raw, e := os.ReadFile("testdata/sup909-producer.jsonl")
	if e != nil {
		t.Fatal(e)
	}
	provenance, e := os.ReadFile("testdata/sup909-provenance.json")
	if e != nil {
		t.Fatal(e)
	}
	var p struct {
		RawSHA    string `json:"rawSourceSHA256"`
		BinarySHA string `json:"binary_sha256"`
	}
	if e = json.Unmarshal(provenance, &p); e != nil {
		t.Fatal(e)
	}
	digest := sha256.Sum256(raw)
	const recorded = "20e257cfb300eaee8e75fa07b2ebf05bfd6a66732fe291aece2705011fbd84c5"
	if p.RawSHA != recorded || hex.EncodeToString(digest[:]) != recorded {
		t.Fatal("captured producer bytes drifted")
	}
	if p.BinarySHA != "bfe0e5276dcdc8614316335887d7c8555eb6544d707b359070e420f67f33f9f7" {
		t.Fatal("wrong producer binary")
	}
	if bytes.Contains(raw, []byte("\r\n")) {
		t.Fatal("raw producer capture normalized")
	}
}
