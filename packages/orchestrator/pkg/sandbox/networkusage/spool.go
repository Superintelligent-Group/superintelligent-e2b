package networkusage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var ErrSpoolBudget = errors.New("network spool budget exhausted; coverage incomplete")

// SpoolOptions bounds logical file bytes (including the one-byte sticky marker)
// and segment count. Filesystem block/metadata overhead is separately bounded
// by MaxSegments, not measured by MaxBytes. Directory must be host-owned.
type SpoolOptions struct {
	MaxBytes, SegmentBytes int64
	MaxSegments            int
}

// spoolAllows is the arithmetic boundary replayed against the Dafny oracle.
func spoolAllows(used int64, count int, bytes int64, newSegment bool, options SpoolOptions) bool {
	return bytes <= options.SegmentBytes && bytes <= options.MaxBytes-used && (!newSegment || count < options.MaxSegments)
}

type Segment struct {
	Name, SHA256 string
	Bytes        int64
	Incomplete   bool
}

// Spool has one OS-locked owner. Acknowledgment is a privileged local API:
// its caller must first obtain durable remote custody of these exact bytes.
// A matching hash proves content identity, not remote durability or billing.
type Spool struct {
	mu             sync.Mutex
	dir            string
	options        SpoolOptions
	lock           *os.File
	used           int64
	count, writers int
	closed         bool
	failed         error
	syncDirectory  func() error // injectable directory durability boundary
}

func OpenSpool(directory string, options SpoolOptions) (*Spool, error) {
	if !filepath.IsAbs(directory) || options.MaxBytes < 2 || options.SegmentBytes < 1 || options.SegmentBytes >= options.MaxBytes || options.MaxSegments < 1 {
		return nil, errors.New("invalid network spool options")
	}
	lock, err := lockSpool(filepath.Join(directory, ".lock"))
	if err != nil {
		return nil, err
	}
	s := &Spool{dir: directory, options: options, lock: lock, used: 1}
	fail := func(err error) (*Spool, error) { unlockSpool(lock); return nil, err }
	marker, err := openSpoolControl(filepath.Join(directory, ".incomplete"), os.O_RDWR|os.O_CREATE|os.O_EXCL)
	created := err == nil
	if errors.Is(err, os.ErrExist) {
		marker, err = openSpoolControl(filepath.Join(directory, ".incomplete"), os.O_RDWR)
	}
	if err != nil {
		return fail(err)
	}
	info, err := marker.Stat()
	if err == nil && ((!created && info.Size() != 1) || info.Size() > 1) {
		err = errors.New("invalid network spool incompleteness marker")
	}
	if err == nil && created {
		_, err = marker.Write([]byte{0})
	}
	if err == nil {
		var value [1]byte
		_, err = marker.ReadAt(value[:], 0)
		if err == nil && value[0] != 0 && value[0] != 1 {
			err = errors.New("invalid network spool incompleteness marker value")
		}
	}
	if err == nil {
		err = marker.Sync()
	}
	err = errors.Join(err, marker.Close())
	if err != nil {
		return fail(err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fail(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == ".lock" || name == ".incomplete" {
			continue
		}
		if !segmentName(name) || !entry.Type().IsRegular() {
			return fail(errors.New("unexpected file in network spool"))
		}
		info, err := entry.Info()
		if err != nil {
			return fail(err)
		}
		if info.Size() > options.MaxBytes-s.used {
			return fail(ErrSpoolBudget)
		}
		s.used += info.Size()
		s.count++
		if strings.HasSuffix(name, ".active") {
			// An abandoned writer has no complete terminal protocol. Preserve all bytes,
			// including a possible partial JSON tail, under an immutable evidence name.
			if err = os.Rename(filepath.Join(directory, name), filepath.Join(directory, strings.TrimSuffix(name, ".active")+".incomplete")); err != nil {
				return fail(err)
			}
			if err = s.markIncomplete(); err != nil {
				return fail(err)
			}
		}
	}
	if s.count > options.MaxSegments {
		return fail(ErrSpoolBudget)
	}
	if err = s.syncDir(); err != nil {
		return fail(err)
	}
	return s, nil
}

func segmentName(name string) bool {
	base, ext, _ := strings.Cut(name, ".")
	if len(base) != 32 || (ext != "active" && ext != "jsonl" && ext != "incomplete") {
		return false
	}
	_, err := hex.DecodeString(base)
	return err == nil
}
func (s *Spool) syncDir() error {
	if s.syncDirectory != nil {
		s.failed = errors.Join(s.failed, s.syncDirectory())
		return s.failed
	}
	f, e := os.Open(s.dir)
	if e != nil {
		s.failed = e
		return e
	}
	s.failed = errors.Join(s.failed, f.Sync(), f.Close())
	return s.failed
}
func (s *Spool) markIncomplete() error {
	f, e := openSpoolControl(filepath.Join(s.dir, ".incomplete"), os.O_WRONLY)
	if e != nil {
		return e
	}
	n, e := f.WriteAt([]byte{1}, 0)
	if e == nil && n != 1 {
		e = io.ErrShortWrite
	}
	if e == nil {
		e = f.Sync()
	}
	return errors.Join(e, f.Close())
}

// OpenWithSpool uses the existing Journal transition/persistence implementation.
// Nil keeps the feature disabled. A failed append latches that journal.
func OpenWithSpool(s *Spool, sandboxID string) (*Journal, error) {
	if s == nil {
		return nil, nil
	}
	if sandboxID == "" || len(sandboxID) > 256 || !utf8.ValidString(sandboxID) {
		return nil, errors.New("invalid sandbox identity")
	}
	s.mu.Lock()
	if s.closed || s.failed != nil {
		s.mu.Unlock()
		return nil, errors.Join(errors.New("spool unavailable"), s.failed)
	}
	s.writers++
	s.mu.Unlock()
	writer := &spoolWriter{spool: s}
	j := &Journal{file: writer, now: time.Now, last: Record{SchemaVersion: 1, SandboxID: sandboxID, Incarnation: rand.Text(), Valid: true}}
	first := j.last
	first.Kind = "start"
	if err := j.append(first); err != nil {
		writer.Close()
		return nil, err
	}
	return j, nil
}

func (s *Spool) Segments() ([]Segment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("spool closed")
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	out := []Segment{}
	for _, entry := range entries {
		name := entry.Name()
		if !segmentName(name) || strings.HasSuffix(name, ".active") {
			continue
		}
		f, e := os.Open(filepath.Join(s.dir, name))
		if e != nil {
			return nil, e
		}
		h := sha256.New()
		n, e := io.Copy(h, f)
		e = errors.Join(e, f.Close())
		if e != nil {
			return nil, e
		}
		out = append(out, Segment{Name: name, SHA256: hex.EncodeToString(h.Sum(nil)), Bytes: n, Incomplete: strings.HasSuffix(name, ".incomplete")})
	}
	return out, nil
}

// Acknowledge removes immutable evidence only after exact content matching.
// Incomplete segments require custody of the raw bytes, not merely parsed rows.
func (s *Spool) Acknowledge(name, digest string) error {
	return s.acknowledge(context.Background(), name, digest)
}

func (s *Spool) acknowledge(ctx context.Context, name, digest string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed || s.failed != nil || !segmentName(name) || strings.HasSuffix(name, ".active") {
		return errors.New("invalid acknowledgment")
	}
	f, err := os.Open(filepath.Join(s.dir, name))
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(h, contextReader{ctx: ctx, reader: f})
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != digest {
		return errors.New("segment acknowledgment content mismatch")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = os.Remove(filepath.Join(s.dir, name)); err != nil {
		return err
	}
	s.used -= n
	s.count--
	return s.syncDir()
}
func (s *Spool) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	if s.writers != 0 {
		return errors.New("network spool still has writers")
	}
	s.closed = true
	return unlockSpool(s.lock)
}

type spoolWriter struct {
	spool  *Spool
	file   *os.File
	name   string
	size   int64
	failed error
	closed bool
}

func (w *spoolWriter) seal() error {
	if w.file == nil {
		return nil
	}
	err := w.file.Sync()
	err = errors.Join(err, w.file.Close())
	w.file = nil
	suffix := ".jsonl"
	if err != nil || w.failed != nil {
		suffix = ".incomplete"
	}
	err = errors.Join(err, os.Rename(filepath.Join(w.spool.dir, w.name), filepath.Join(w.spool.dir, strings.TrimSuffix(w.name, ".active")+suffix)))
	return errors.Join(err, w.spool.syncDir())
}
func (w *spoolWriter) Write(p []byte) (int, error) {
	s := w.spool
	s.mu.Lock()
	defer s.mu.Unlock()
	if w.closed {
		return 0, os.ErrClosed
	}
	if s.failed != nil {
		w.failed = s.failed
		return 0, w.failed
	}
	if w.failed != nil {
		return 0, w.failed
	}
	newSegment := w.file == nil || int64(len(p)) > s.options.SegmentBytes-w.size
	if !spoolAllows(s.used, s.count, int64(len(p)), newSegment, s.options) {
		w.failed = errors.Join(ErrSpoolBudget, s.markIncomplete())
		return 0, w.failed
	}
	if w.file != nil && int64(len(p)) > s.options.SegmentBytes-w.size {
		if e := w.seal(); e != nil {
			w.failed = e
			return 0, e
		}
	}
	if w.file == nil {
		id := make([]byte, 16)
		if _, e := rand.Read(id); e != nil {
			return 0, e
		}
		w.name = fmt.Sprintf("%x.active", id)
		f, e := os.OpenFile(filepath.Join(s.dir, w.name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			w.failed = e
			return 0, e
		}
		w.file = f
		w.size = 0
		s.count++
		if e = s.syncDir(); e != nil {
			w.failed = e
			return 0, e
		}
	}
	n, e := w.file.Write(p)
	w.size += int64(n)
	s.used += int64(n)
	if e == nil && n != len(p) {
		e = io.ErrShortWrite
	}
	if e != nil {
		w.failed = e
	}
	return n, e
}
func (w *spoolWriter) Sync() error {
	s := w.spool
	s.mu.Lock()
	defer s.mu.Unlock()
	if w.failed != nil {
		return w.failed
	}
	if w.file == nil {
		return os.ErrClosed
	}
	w.failed = w.file.Sync()
	return w.failed
}
func (w *spoolWriter) Close() error {
	s := w.spool
	s.mu.Lock()
	defer s.mu.Unlock()
	if w.closed {
		return w.failed
	}
	w.closed = true
	s.writers--
	return errors.Join(w.failed, w.seal())
}
