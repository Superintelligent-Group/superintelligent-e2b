//go:build linux

package networkusage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/e2b-dev/infra/packages/shared/pkg/storage"
)

type custodyFake struct {
	destination                storage.CustodyDestination
	objects                    map[string][]byte
	loseResponse, wrongReceipt bool
	creates, verifies          int
	afterCommit                func()
}

func (f *custodyFake) CreateExact(ctx context.Context, c storage.CustodyClaim, data []byte) (storage.CustodyReceipt, error) {
	f.creates++
	if err := ctx.Err(); err != nil {
		return storage.CustodyReceipt{}, err
	}
	key, e := f.destination.ObjectKey(c)
	if e != nil {
		return storage.CustodyReceipt{}, e
	}
	if old, ok := f.objects[key]; ok && !bytes.Equal(old, data) {
		return storage.CustodyReceipt{}, errors.New("conflicting raw object")
	}
	f.objects[key] = append([]byte{}, data...)
	if f.afterCommit != nil {
		f.afterCommit()
	}
	if f.loseResponse {
		f.loseResponse = false
		return storage.CustodyReceipt{}, errors.New("response lost after commit")
	}
	return f.VerifyExact(ctx, c)
}

type cancelAfterRead struct {
	cancel context.CancelFunc
	reads  int
}

func (r *cancelAfterRead) Read(p []byte) (int, error) {
	r.reads++
	for i := range p {
		p[i] = 'x'
	}
	r.cancel()
	return len(p), nil
}

func TestDeliveryBoundedReadsAndDeadline(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 50; i++ {
		if e := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%032x.jsonl", i)), []byte("raw"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	s, e := OpenSpool(dir, SpoolOptions{MaxBytes: 10000, SegmentBytes: 1000, MaxSegments: 100})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	d := deliveryDestination()
	store := &custodyFake{destination: d, objects: map[string][]byte{}}
	reads, readBytes := 0, 0
	read := func(ctx context.Context, segment Segment, max int64) ([]byte, error) {
		reads++
		data, e := s.readSegment(ctx, segment, max)
		readBytes += len(data)
		return data, e
	}
	if n, e := s.deliverOnce(context.Background(), store, d, 1, read); e != nil || n != 1 || reads != 1 || readBytes != 3 {
		t.Fatalf("unbounded delivery: %d %d %d %v", n, reads, readBytes, e)
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if n, e := s.deliverOnce(ctx, store, d, 1, read); n != 0 || !errors.Is(e, context.DeadlineExceeded) || reads != 1 || store.creates != 1 {
		t.Fatal("expired pass performed I/O")
	}
	segments, e := s.Segments()
	if e != nil || len(segments) != 49 {
		t.Fatal("expired pass deleted evidence")
	}
	ctx, cancel = context.WithCancel(context.Background())
	source := &cancelAfterRead{cancel: cancel}
	n, e := io.Copy(io.Discard, contextReader{ctx: ctx, reader: source})
	if !errors.Is(e, context.Canceled) || n > 32*1024 || source.reads != 1 {
		t.Fatalf("chunk cancellation failed: %d %v", n, e)
	}
}

func TestDeliveryCancellationAfterCustodyRetainsLocal(t *testing.T) {
	s := testSpool(t, SpoolOptions{MaxBytes: 10000, SegmentBytes: 1000, MaxSegments: 10})
	j, e := OpenWithSpool(s, "test")
	if e != nil {
		t.Fatal(e)
	}
	if e = j.Close(); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := deliveryDestination()
	store := &custodyFake{destination: d, objects: map[string][]byte{}, afterCommit: cancel}
	if n, e := s.DeliverOnce(ctx, store, d, 1); n != 0 || !errors.Is(e, context.Canceled) {
		t.Fatal("canceled receipt deleted local evidence")
	}
	segments, e := s.Segments()
	if e != nil || len(segments) != 1 {
		t.Fatal("evidence removed after cancellation")
	}
}
func (f *custodyFake) VerifyExact(_ context.Context, c storage.CustodyClaim) (storage.CustodyReceipt, error) {
	f.verifies++
	key, e := f.destination.ObjectKey(c)
	r := storage.CustodyReceipt{Destination: f.destination, Claim: c, Key: key}
	if f.wrongReceipt {
		r.Destination.ClaimedHostID = "other"
	}
	return r, e
}
func deliveryDestination() storage.CustodyDestination {
	return storage.CustodyDestination{AccountID: "123456789012", Region: "us-east-1", Bucket: "evidence-bucket", Prefix: "network-usage/v1/test", ClaimedHostID: "claimed-host", MaxObjectBytes: 10000}
}

func TestDeliveryLostResponseRestartRawTail(t *testing.T) {
	dir := t.TempDir()
	name := "0123456789abcdef0123456789abcdef.active"
	raw := []byte("{\"sample\":1}\n{partial")
	if e := os.WriteFile(filepath.Join(dir, name), raw, 0600); e != nil {
		t.Fatal(e)
	}
	options := SpoolOptions{MaxBytes: 10000, SegmentBytes: 1000, MaxSegments: 10}
	s, e := OpenSpool(dir, options)
	if e != nil {
		t.Fatal(e)
	}
	d := deliveryDestination()
	store := &custodyFake{destination: d, objects: map[string][]byte{}, loseResponse: true}
	if n, e := s.DeliverOnce(context.Background(), store, d, 1); n != 0 || e == nil {
		t.Fatal("lost response acknowledged")
	}
	segments, e := s.Segments()
	if e != nil || len(segments) != 1 {
		t.Fatal("local evidence lost")
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = OpenSpool(dir, options)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if n, e := s.DeliverOnce(context.Background(), store, d, 1); e != nil || n != 1 {
		t.Fatalf("replay failed: %d %v", n, e)
	}
	if len(store.objects) != 1 || store.creates != 2 || store.verifies != 1 {
		t.Fatal("duplicate remote evidence")
	}
	for _, data := range store.objects {
		if !bytes.Equal(raw, data) {
			t.Fatal("raw tail changed")
		}
	}
	segments, e = s.Segments()
	if e != nil || len(segments) != 0 {
		t.Fatal("retention not reclaimed")
	}
}

func TestDeliveryRejectsWrongReceiptAndCancellation(t *testing.T) {
	s := testSpool(t, SpoolOptions{MaxBytes: 10000, SegmentBytes: 1000, MaxSegments: 10})
	j, e := OpenWithSpool(s, "test")
	if e != nil {
		t.Fatal(e)
	}
	if e = j.Close(); e != nil {
		t.Fatal(e)
	}
	d := deliveryDestination()
	store := &custodyFake{destination: d, objects: map[string][]byte{}, wrongReceipt: true}
	if _, e = s.DeliverOnce(context.Background(), store, d, 1); e == nil {
		t.Fatal("wrong receipt accepted")
	}
	segments, e := s.Segments()
	if e != nil || len(segments) != 1 {
		t.Fatal("evidence deleted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := store.creates
	if _, e = s.DeliverOnce(ctx, store, d, 1); !errors.Is(e, context.Canceled) || store.creates != before {
		t.Fatal("cancellation ignored")
	}
}
