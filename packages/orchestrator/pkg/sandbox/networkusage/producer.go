package networkusage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

const ProducerSchema = "sig.fc-measurement.v1"
const TerminalSchema = "sig.fc-terminal-measurement.v1"
const TerminalCutoff = "last_completed_device_operation"
const MaxProducerFrameBytes = 1024*1024 + 1 // producer JSON limit plus mandatory newline
const MaxProducerReceiptBytes = 2048

type ProducerRequest struct {
	Incarnation string `json:"incarnation"`
	Sequence    uint64 `json:"request_sequence"`
	ID          string `json:"request_id"`
}
type ProducerHeader struct {
	Schema          string  `json:"schema"`
	Incarnation     string  `json:"incarnation"`
	Attempt         uint64  `json:"attempt_sequence"`
	RequestSequence *uint64 `json:"request_sequence"`
	RequestID       *string `json:"request_id"`
	Loss            bool    `json:"loss_detected"`
}
type ProducerFrame struct {
	Header                               ProducerHeader
	Cutoff                               string
	Tx, Rx                               uint64
	NetworkPresent, DeviceMetricsPresent bool
	// Metrics is retained for existing telemetry consumers; it is not a second observation.
	Metrics json.RawMessage
}
type ProducerReceipt struct {
	Header ProducerHeader
	Cutoff string
}

func tokenOK(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, c := range []byte(s) {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// checkJSON rejects ambiguous duplicate keys at every nesting level. Unknown
// metrics fields are preserved; protocol envelope/header fields are closed.
func checkJSON(raw []byte) error {
	if !utf8.Valid(raw) || !validUnicodeEscapes(raw) {
		return errors.New("invalid producer UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 64 {
			return errors.New("producer JSON nesting exceeds limit")
		}
		t, e := d.Token()
		if e != nil {
			return e
		}
		delim, ok := t.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return e
				}
				key, ok := k.(string)
				if !ok || seen[key] {
					return errors.New("duplicate or invalid JSON key")
				}
				seen[key] = true
				if e = value(depth + 1); e != nil {
					return e
				}
			}
		case '[':
			for d.More() {
				if e = value(depth + 1); e != nil {
					return e
				}
			}
		default:
			return errors.New("unexpected JSON delimiter")
		}
		_, e = d.Token()
		return e
	}
	if e := value(0); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return errors.New("trailing JSON data")
	}
	return nil
}

// encoding/json replaces lone surrogate escapes; the pinned serde producer does
// not emit them. Reject rather than silently changing identity or metrics keys.
func validUnicodeEscapes(raw []byte) bool {
	inside := false
	for i := 0; i < len(raw); i++ {
		if raw[i] == '"' {
			inside = !inside
			continue
		}
		if !inside || raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw) {
			return false
		}
		if raw[i] != 'u' {
			continue
		}
		if i+4 >= len(raw) {
			return false
		}
		v, e := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if e != nil {
			return false
		}
		i += 4
		if v >= 0xDC00 && v <= 0xDFFF {
			return false
		}
		if v >= 0xD800 && v <= 0xDBFF {
			if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return false
			}
			low, e := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
			if e != nil || low < 0xDC00 || low > 0xDFFF {
				return false
			}
			i += 6
		}
	}
	return true
}

func object(raw []byte, allowed ...string) (map[string]json.RawMessage, error) {
	var m map[string]json.RawMessage
	if e := json.Unmarshal(raw, &m); e != nil {
		return nil, e
	}
	if m == nil {
		return nil, errors.New("required JSON object")
	}
	if len(allowed) > 0 {
		for k := range m {
			found := false
			for _, a := range allowed {
				if k == a {
					found = true
				}
			}
			if !found {
				return nil, fmt.Errorf("unknown producer field %s", k)
			}
		}
	}
	return m, nil
}
func required(m map[string]json.RawMessage, k string, out any) error {
	raw, ok := m[k]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("missing producer field %s", k)
	}
	return json.Unmarshal(raw, out)
}
func decodeHeader(raw []byte) (ProducerHeader, error) {
	var h ProducerHeader
	m, e := object(raw, "schema", "incarnation", "attempt_sequence", "request_sequence", "request_id", "loss_detected")
	if e != nil {
		return h, e
	}
	for k, out := range map[string]any{"schema": &h.Schema, "incarnation": &h.Incarnation, "attempt_sequence": &h.Attempt, "loss_detected": &h.Loss} {
		if e = required(m, k, out); e != nil {
			return h, e
		}
	}
	for k, out := range map[string]any{"request_sequence": &h.RequestSequence, "request_id": &h.RequestID} {
		v, ok := m[k]
		if !ok {
			return h, fmt.Errorf("missing producer field %s", k)
		}
		if e = json.Unmarshal(v, out); e != nil {
			return h, e
		}
	}
	if h.Schema != ProducerSchema || !tokenOK(h.Incarnation) || h.Attempt == 0 {
		return h, errors.New("invalid producer header")
	}
	if (h.RequestID == nil) != (h.RequestSequence == nil) {
		return h, errors.New("partial request identity")
	}
	if h.RequestID != nil && (!tokenOK(*h.RequestID) || *h.RequestSequence == 0) {
		return h, errors.New("invalid producer request identity")
	}
	return h, nil
}

// DecodeProducerFrame requires one complete newline-terminated producer frame.
// It never decodes u64 counters through floating point values.
func DecodeProducerFrame(raw []byte) (ProducerFrame, error) {
	var f ProducerFrame
	if len(raw) == 0 || len(raw) > MaxProducerFrameBytes || raw[len(raw)-1] != '\n' {
		return f, errors.New("producer frame size or missing newline")
	}
	if bytes.Contains(raw[:len(raw)-1], []byte{'\n'}) {
		return f, errors.New("multiple or noncanonical producer lines")
	}
	if e := checkJSON(raw); e != nil {
		return f, e
	}
	m, e := object(raw, "measurement", "metrics", "terminal_cutoff")
	if e != nil {
		return f, e
	}
	f.Header, e = decodeHeader(m["measurement"])
	if e != nil {
		return f, e
	}
	metrics, e := object(m["metrics"])
	if e != nil {
		return f, e
	}
	f.Metrics = bytes.Clone(m["metrics"])
	if _, ok := m["terminal_cutoff"]; ok {
		if e = required(m, "terminal_cutoff", &f.Cutoff); e != nil || f.Cutoff != TerminalCutoff {
			return f, errors.New("invalid terminal cutoff")
		}
		if f.Header.RequestID == nil {
			return f, errors.New("terminal frame without request")
		}
	}
	for k := range metrics {
		if strings.HasPrefix(k, "net_") {
			device, e := object(metrics[k])
			if e != nil {
				return f, e
			}
			var tx, rx uint64
			if e = required(device, "tx_bytes_count", &tx); e != nil {
				return f, e
			}
			if e = required(device, "rx_bytes_count", &rx); e != nil {
				return f, e
			}
			f.DeviceMetricsPresent = true
		}
	}
	if net, ok := metrics["net"]; ok {
		n, e := object(net)
		if e != nil {
			return f, e
		}
		if e = required(n, "tx_bytes_count", &f.Tx); e != nil {
			return f, e
		}
		if e = required(n, "rx_bytes_count", &f.Rx); e != nil {
			return f, e
		}
		f.NetworkPresent = true
	}
	return f, nil
}

func DecodeProducerReceipt(raw []byte, terminal bool) (ProducerReceipt, error) {
	var r ProducerReceipt
	if len(raw) == 0 || len(raw) > MaxProducerReceiptBytes {
		return r, errors.New("producer receipt size")
	}
	if e := checkJSON(raw); e != nil {
		return r, e
	}
	if terminal {
		m, e := object(raw, "schema", "cutoff", "measurement")
		if e != nil {
			return r, e
		}
		var schema string
		if e = required(m, "schema", &schema); e != nil || schema != TerminalSchema {
			return r, errors.New("invalid terminal receipt schema")
		}
		if e = required(m, "cutoff", &r.Cutoff); e != nil || r.Cutoff != TerminalCutoff {
			return r, errors.New("invalid terminal receipt cutoff")
		}
		var e2 error
		r.Header, e2 = decodeHeader(m["measurement"])
		if e2 != nil {
			return r, e2
		}
	} else {
		var e error
		r.Header, e = decodeHeader(raw)
		if e != nil {
			return r, e
		}
	}
	if r.Header.RequestID == nil {
		return r, errors.New("receipt without request identity")
	}
	return r, nil
}
func headersEqual(a, b ProducerHeader) bool {
	if a.Schema != b.Schema || a.Incarnation != b.Incarnation || a.Attempt != b.Attempt || a.Loss != b.Loss {
		return false
	}
	if a.RequestID == nil || b.RequestID == nil {
		return a.RequestID == nil && b.RequestID == nil && a.RequestSequence == nil && b.RequestSequence == nil
	}
	return b.RequestSequence != nil && a.RequestSequence != nil && *a.RequestID == *b.RequestID && *a.RequestSequence == *b.RequestSequence
}
