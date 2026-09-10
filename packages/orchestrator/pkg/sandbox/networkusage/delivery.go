package networkusage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/e2b-dev/infra/packages/shared/pkg/storage"
)

// cooperativeCancellation marks a checked pass cancellation at a point where
// no new mutation is performed. An OS read/fsync timeout has no such marker.
type cooperativeCancellation struct{ cause error }

func (e cooperativeCancellation) Error() string { return e.cause.Error() }
func (e cooperativeCancellation) Unwrap() error { return e.cause }

func onlyCooperativeCancellation(err error) bool {
	if _, ok := err.(cooperativeCancellation); ok {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !onlyCooperativeCancellation(child) {
				return false
			}
		}
		return true
	}
	return false
}

// DeliverOnce is an explicit bounded pass, not a background process or an
// authorization gate. The operator must authorize the store and protect its
// prefix before using remote custody to reclaim local evidence.
func (s *Spool) DeliverOnce(ctx context.Context, store storage.ImmutableCustodyStore, destination storage.CustodyDestination, limit int) (int, error) {
	return s.deliverOnce(ctx, store, destination, limit, s.readSegment)
}

func (s *Spool) deliverOnce(ctx context.Context, store storage.ImmutableCustodyStore, destination storage.CustodyDestination, limit int, read func(context.Context, Segment, int64) ([]byte, error)) (int, error) {
	if store == nil || limit < 1 || limit > 100 {
		return 0, errors.New("invalid delivery configuration")
	}
	if err := destination.Validate(); err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	segments, err := s.deliveryCandidates(ctx, limit, destination.MaxObjectBytes)
	if err != nil {
		return 0, err
	}
	delivered := 0
	for _, segment := range segments {
		if delivered == limit {
			break
		}
		if err = ctx.Err(); err != nil {
			return delivered, cooperativeCancellation{err}
		}
		data, err := read(ctx, segment, destination.MaxObjectBytes)
		if err != nil {
			return delivered, err
		}
		hash := sha256.Sum256(data)
		claim := storage.CustodyClaim{Name: segment.Name, SHA256: hex.EncodeToString(hash[:]), Bytes: segment.Bytes}
		if _, err = destination.ObjectKey(claim); err != nil {
			return delivered, err
		}
		receipt, err := store.CreateExact(ctx, claim, data)
		if err != nil {
			return delivered, err
		}
		if !receipt.Matches(destination, claim) {
			return delivered, errors.New("custody receipt identity mismatch")
		}
		if err = s.acknowledge(ctx, segment.Name, claim.SHA256); err != nil {
			return delivered, err
		}
		delivered++
	}
	return delivered, nil
}

// Enumerate metadata in bounded chunks; do not hash backlog files or hold the
// writer mutex while reading immutable evidence. A concurrent retention pass
// may remove a candidate; that returns an error rather than fabricating custody.
func (s *Spool) deliveryCandidates(ctx context.Context, limit int, maxBytes int64) ([]Segment, error) {
	s.mu.Lock()
	unavailable := s.closed || s.failed != nil
	s.mu.Unlock()
	if unavailable {
		return nil, errors.New("spool unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, cooperativeCancellation{err}
	}
	dir, err := os.Open(s.dir)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	out := []Segment{}
	for len(out) < limit {
		if err = ctx.Err(); err != nil {
			return nil, cooperativeCancellation{err}
		}
		entries, readErr := dir.ReadDir(16)
		for _, entry := range entries {
			if err = ctx.Err(); err != nil {
				return nil, cooperativeCancellation{err}
			}
			name := entry.Name()
			if !segmentName(name) || strings.HasSuffix(name, ".active") {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				return nil, err
			}
			if !info.Mode().IsRegular() || info.Size() > maxBytes {
				return nil, errors.New("invalid delivery candidate")
			}
			out = append(out, Segment{Name: name, Bytes: info.Size(), Incomplete: strings.HasSuffix(name, ".incomplete")})
			if len(out) == limit {
				break
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	return out, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, cooperativeCancellation{err}
	}
	if len(p) > 32*1024 {
		p = p[:32*1024]
	}
	return r.reader.Read(p)
}

func (s *Spool) readSegment(ctx context.Context, segment Segment, maxBytes int64) ([]byte, error) {
	s.mu.Lock()
	unavailable := s.closed || s.failed != nil
	s.mu.Unlock()
	if unavailable || !segmentName(segment.Name) || segment.Bytes < 0 || segment.Bytes > maxBytes {
		return nil, errors.New("invalid delivery segment")
	}
	f, err := os.Open(filepath.Join(s.dir, segment.Name))
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(contextReader{ctx: ctx, reader: io.LimitReader(f, maxBytes+1)})
	err = errors.Join(err, f.Close())
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != segment.Bytes {
		return nil, errors.New("local segment changed before delivery")
	}
	return data, nil
}
