package networkusage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"sync"
)

type RequestScope string

const (
	BaselineScope RequestScope = "preboot_baseline"
	SampleScope   RequestScope = "device_sample"
	TerminalScope RequestScope = "terminal_device_cutoff"
)

// ProducerEvidence preserves exact bytes as JSON base64, including the newline.
// Base64 expansion is charged by the existing spool's actual encoded-byte budget.
// Header sequence is independent of the containing Record.Sequence.
type ProducerEvidence struct {
	Incarnation   string           `json:"incarnation"`
	Request       *ProducerRequest `json:"request,omitempty"`
	Header        *ProducerHeader  `json:"header,omitempty"`
	Scope         RequestScope     `json:"scope,omitempty"`
	NetworkScope  string           `json:"networkScope,omitempty"`
	Cutoff        string           `json:"cutoff,omitempty"`
	Raw           []byte           `json:"raw,omitempty"`
	SHA256        string           `json:"sha256,omitempty"`
	Bytes         int              `json:"bytes,omitempty"`
	FrameSequence uint64           `json:"frameSequence,string,omitempty"`
	FrameSHA256   string           `json:"frameSha256,omitempty"`
	FrameBytes    int              `json:"frameBytes,omitempty"`
}

// DurableFence is a locally synced correlation reference, never remote custody
// or complete lifetime coverage. Fetch through Fence after both observations.
type DurableFence struct {
	Request                            ProducerRequest
	Header                             ProducerHeader
	Scope                              RequestScope
	Cutoff                             string
	FrameSequence, CorrelationSequence uint64
	FrameSHA256                        string
	FrameBytes                         int
}
type pendingRequest struct {
	request       ProducerRequest
	scope         RequestScope
	frame         *ProducerHeader
	cutoff        string
	frameSequence uint64
	frameSHA256   string
	frameBytes    int
	receipt       *ProducerReceipt
	rawReceipt    []byte
}

// Correlator has exactly one requested flush in flight. Its mutex serializes
// callbacks from the FIFO reader and API response path. Durable data stays in
// Journal/OpenWithSpool, not an unbounded in-memory frame or request map.
type Correlator struct {
	mu                       sync.Mutex
	journal                  *Journal
	incarnation              string
	attempt, requestSequence uint64
	baseline                 bool
	preboot, activity        bool
	pending                  *pendingRequest
	last                     *DurableFence
	failed                   error
	closed                   bool
}

func NewCorrelator(j *Journal, incarnation string) (*Correlator, error) {
	if j == nil || !tokenOK(incarnation) {
		return nil, errors.New("correlation requires enabled journal and valid incarnation")
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.correlated || j.closed || j.failed != nil || j.last.Sequence != 0 {
		return nil, errors.New("correlation requires fresh available journal")
	}
	j.correlated = true
	r, e := j.next("producer_binding")
	if e != nil {
		return nil, e
	}
	r.Producer = &ProducerEvidence{Incarnation: incarnation}
	if e = j.append(r); e != nil {
		return nil, e
	}
	if !j.last.Valid {
		return nil, errors.New("invalid journal binding")
	}
	return &Correlator{journal: j, incarnation: incarnation}, nil
}

// Begin persists request intent before returning a tuple safe to send. Repeating
// the latest identity/scope returns that exact tuple for response-loss retry.
func (c *Correlator) Begin(id string, scope RequestScope) (ProducerRequest, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.available(); e != nil {
		return ProducerRequest{}, e
	}
	if !tokenOK(id) || (scope != BaselineScope && scope != SampleScope && scope != TerminalScope) {
		return ProducerRequest{}, errors.New("invalid producer request")
	}
	if c.pending != nil {
		if c.pending.request.ID == id && c.pending.scope == scope {
			return c.pending.request, nil
		}
		return ProducerRequest{}, errors.New("producer request already pending")
	}
	if c.last != nil && c.last.Request.ID == id && c.last.Scope == scope {
		return c.last.Request, nil
	}
	if c.last != nil && c.last.Scope == TerminalScope {
		return ProducerRequest{}, errors.New("producer already terminal")
	}
	if (!c.baseline && scope != BaselineScope) || (c.baseline && scope == BaselineScope) {
		return ProducerRequest{}, errors.New("baseline must precede device requests")
	}
	if scope != BaselineScope && !c.activity {
		return ProducerRequest{}, errors.New("activity intent required before device requests")
	}
	if c.requestSequence == math.MaxUint64 {
		return ProducerRequest{}, c.fail("request_sequence_overflow", nil)
	}
	req := ProducerRequest{Incarnation: c.incarnation, Sequence: c.requestSequence + 1, ID: id}
	if _, e := c.persist("producer_request", &ProducerEvidence{Incarnation: c.incarnation, Request: &req, Scope: scope}, 0, 0, false); e != nil {
		return ProducerRequest{}, e
	}
	if scope == BaselineScope {
		c.preboot = true
	}
	c.requestSequence = req.Sequence
	c.pending = &pendingRequest{request: req, scope: scope}
	return req, nil
}

// BeginActivity durably records intent immediately before startVM/loadSnapshot.
// It requires a matched baseline; the caller still owns actually starting work.
// Preboot remains explicit while configuration is slow, including periodic frames.
func (c *Correlator) BeginActivity() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.available(); e != nil {
		return e
	}
	if c.last != nil && c.last.Scope == TerminalScope || c.pending != nil && c.pending.scope == TerminalScope {
		return errors.New("terminal request prohibits activity authorization")
	}
	if c.activity {
		return nil
	}
	if !c.baseline || c.pending != nil {
		return errors.New("matched baseline required before activity")
	}
	if _, e := c.persist("producer_activity_intent", &ProducerEvidence{Incarnation: c.incarnation, Scope: SampleScope}, 0, 0, false); e != nil {
		return e
	}
	c.activity = true
	return nil
}

// ObserveFrame returns only after exact frame bytes and counters have synced.
// A requested frame alone is not a fence: its matching API receipt must sync too.
func (c *Correlator) ObserveFrame(raw []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.available(); e != nil {
		return e
	}
	if !c.preboot {
		return c.fail("frame_before_baseline_intent", raw)
	}
	f, e := DecodeProducerFrame(raw)
	if e != nil {
		return c.fail("invalid_producer_frame", raw)
	}
	if f.Header.Incarnation != c.incarnation || c.attempt == math.MaxUint64 || f.Header.Attempt != c.attempt+1 {
		return c.fail("producer_sequence_or_incarnation", raw)
	}
	if f.Header.Loss {
		return c.fail("producer_reported_loss", raw)
	}
	if c.last != nil && c.last.Scope == TerminalScope || c.pending != nil && c.pending.scope == TerminalScope && c.pending.frame != nil {
		return c.fail("frame_after_terminal", raw)
	}
	networkScope := "device_aggregate"
	if !f.DeviceMetricsPresent {
		if !c.preboot || c.activity || f.Tx != 0 || f.Rx != 0 {
			return c.fail("missing_device_metrics", raw)
		}
		networkScope = "preboot_no_devices" // aggregate zero/absence is not observed network traffic
	} else if !f.NetworkPresent {
		return c.fail("missing_network_counters", raw)
	}
	var scope RequestScope
	if f.Header.RequestID != nil {
		p := c.pending
		if p == nil || p.frame != nil || *f.Header.RequestID != p.request.ID || *f.Header.RequestSequence != p.request.Sequence {
			return c.fail("unexpected_requested_frame", raw)
		}
		if (p.scope == TerminalScope) != (f.Cutoff == TerminalCutoff) {
			return c.fail("terminal_frame_scope_mismatch", raw)
		}
		scope = p.scope
	}
	if f.Cutoff != "" && scope != TerminalScope {
		return c.fail("unexpected_terminal_frame", raw)
	}
	hash := sha256.Sum256(raw)
	hashText := hex.EncodeToString(hash[:])
	evidence := &ProducerEvidence{Incarnation: c.incarnation, Header: &f.Header, Scope: scope, NetworkScope: networkScope, Cutoff: f.Cutoff, Raw: bytes.Clone(raw), SHA256: hashText, Bytes: len(raw)}
	sequence, e := c.persist("producer_frame", evidence, f.Tx, f.Rx, networkScope == "device_aggregate")
	if e != nil {
		return e
	}
	c.attempt = f.Header.Attempt
	if f.Header.RequestID != nil {
		p := c.pending
		p.frame = &f.Header
		p.cutoff = f.Cutoff
		p.frameSequence = sequence
		p.frameSHA256 = hashText
		p.frameBytes = len(raw)
		return c.match()
	}
	return nil
}

// ObserveReceipt accepts response-before-frame, frame-before-response and exact
// response retries. Successful return can still mean pending; consult Fence.
func (c *Correlator) ObserveReceipt(raw []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.available(); e != nil {
		return e
	}
	if c.pending == nil {
		if c.last != nil {
			r, e := DecodeProducerReceipt(raw, c.last.Scope == TerminalScope)
			if e == nil && headersEqual(r.Header, c.last.Header) && r.Cutoff == c.last.Cutoff {
				return nil
			}
		}
		return c.fail("unexpected_producer_receipt", raw)
	}
	p := c.pending
	r, e := DecodeProducerReceipt(raw, p.scope == TerminalScope)
	if e != nil {
		return c.fail("invalid_producer_receipt", raw)
	}
	if r.Header.Incarnation != c.incarnation || r.Header.Loss || *r.Header.RequestID != p.request.ID || *r.Header.RequestSequence != p.request.Sequence {
		return c.fail("producer_receipt_identity_or_loss", raw)
	}
	if p.receipt != nil {
		if !headersEqual(p.receipt.Header, r.Header) || p.receipt.Cutoff != r.Cutoff {
			return c.fail("conflicting_producer_receipt", raw)
		}
	}
	p.receipt = &r
	p.rawReceipt = bytes.Clone(raw)
	return c.match()
}
func (c *Correlator) match() error {
	p := c.pending
	if p == nil || p.frame == nil || p.receipt == nil {
		return nil
	}
	if !headersEqual(*p.frame, p.receipt.Header) || p.cutoff != p.receipt.Cutoff {
		return c.fail("producer_frame_receipt_mismatch", nil)
	}
	receiptHash := sha256.Sum256(p.rawReceipt)
	evidence := &ProducerEvidence{Incarnation: c.incarnation, Request: &p.request, Header: p.frame, Scope: p.scope, Cutoff: p.cutoff, Raw: p.rawReceipt, Bytes: len(p.rawReceipt), SHA256: hex.EncodeToString(receiptHash[:]), FrameSequence: p.frameSequence, FrameSHA256: p.frameSHA256, FrameBytes: p.frameBytes}
	seq, e := c.persist("producer_correlation", evidence, 0, 0, false)
	if e != nil {
		return e
	}
	c.last = &DurableFence{Request: p.request, Header: *p.frame, Scope: p.scope, Cutoff: p.cutoff, FrameSequence: p.frameSequence, CorrelationSequence: seq, FrameSHA256: p.frameSHA256, FrameBytes: p.frameBytes}
	if p.scope == BaselineScope {
		c.baseline = true
	}
	c.pending = nil
	return nil
}

func (c *Correlator) Fence() (*DurableFence, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.available(); e != nil {
		return nil, e
	}
	if c.pending != nil || c.last == nil {
		return nil, nil
	}
	f := *c.last
	f.Header = cloneHeader(f.Header)
	return &f, nil
}
func cloneHeader(h ProducerHeader) ProducerHeader {
	if h.RequestID != nil {
		id := *h.RequestID
		seq := *h.RequestSequence
		h.RequestID = &id
		h.RequestSequence = &seq
	}
	return h
}
func (c *Correlator) available() error {
	if c.failed != nil {
		return c.failed
	}
	if c.closed {
		return errors.New("producer correlator closed")
	}
	c.journal.mu.Lock()
	defer c.journal.mu.Unlock()
	if c.journal.failed != nil || c.journal.closed || !c.journal.last.Valid {
		c.failed = errors.Join(errors.New("producer journal unavailable or invalid"), c.journal.failed)
	}
	return c.failed
}
func (c *Correlator) persist(kind string, evidence *ProducerEvidence, tx, rx uint64, count bool) (uint64, error) {
	j := c.journal
	j.mu.Lock()
	defer j.mu.Unlock()
	r, e := j.next(kind)
	if e != nil {
		c.failed = e
		return 0, e
	}
	r.Producer = evidence
	if count {
		if tx > math.MaxUint64-r.TxBytes || rx > math.MaxUint64-r.RxBytes {
			r.Kind = "gap"
			r.Valid = false
			r.Reason = "counter_overflow"
		} else {
			r.DeltaTxBytes = tx
			r.DeltaRxBytes = rx
			r.TxBytes += tx
			r.RxBytes += rx
		}
	}
	if e = j.append(r); e != nil {
		c.failed = e
		return 0, e
	}
	if !j.last.Valid {
		c.failed = errors.New("producer journal invalid")
		return 0, c.failed
	}
	return r.Sequence, nil
}
func (c *Correlator) fail(reason string, raw []byte) error {
	j := c.journal
	j.mu.Lock()
	defer j.mu.Unlock()
	r, e := j.next("gap")
	if e == nil {
		r.Valid = false
		r.Reason = reason
		r.Producer = &ProducerEvidence{Incarnation: c.incarnation, Bytes: len(raw)}
		if len(raw) <= MaxProducerFrameBytes {
			r.Producer.Raw = bytes.Clone(raw)
			hash := sha256.Sum256(raw)
			r.Producer.SHA256 = hex.EncodeToString(hash[:])
		}
		e = j.append(r)
	}
	c.failed = errors.Join(errors.New(reason), e)
	return c.failed
}

// Gap is the correlated owner's explicit transport/partial-tail failure path.
func (c *Correlator) Gap(reason string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(reason) == 0 || len(reason) > 80 {
		return errors.New("invalid gap reason")
	}
	if e := c.available(); e != nil {
		return e
	}
	return c.fail(reason, nil)
}

// Close remains incomplete. Even a terminal fence is not protected custody.
func (c *Correlator) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return c.failed
	}
	if c.pending != nil && c.failed == nil {
		c.fail("unresolved_producer_request", nil)
	}
	c.closed = true
	c.failed = errors.Join(c.failed, c.journal.Close())
	return c.failed
}
