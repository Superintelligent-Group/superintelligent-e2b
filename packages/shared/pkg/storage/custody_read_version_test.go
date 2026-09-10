package storage

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

type readVersionBody struct {
	io.Reader
	closed   atomic.Int32
	closeErr error
}

func (b *readVersionBody) Close() error { b.closed.Add(1); return b.closeErr }

type readVersionErrorReader struct{}

func (readVersionErrorReader) Read(p []byte) (int, error) {
	return copy(p, "raw"), errors.New("transport body failure")
}

func readVersionFixture(t *testing.T, fault string, body io.ReadCloser) (*ProtectedCustodyReader, CustodyClaim, *atomic.Int32) {
	t.Helper()
	c := readerTestConfig()
	claim := custodyTestClaim([]byte("raw"))
	destination := c.Policy.Destination
	destination.ProducerUserID = c.ExpectedProducerUserID
	reads := &atomic.Int32{}
	now := time.Now().UTC().Truncate(time.Second)
	cfg := aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("fake", "fake", "")}
	cfg.HTTPClient = custodyHTTPDo(func(r *http.Request) (*http.Response, error) {
		text := ""
		if r.URL.Host == "sts.us-east-1.amazonaws.com" {
			i := readerTestIdentity()
			text = fmt.Sprintf("<GetCallerIdentityResponse><GetCallerIdentityResult><Account>%s</Account><Arn>%s</Arn><UserId>%s</UserId></GetCallerIdentityResult></GetCallerIdentityResponse>", *i.Account, *i.Arn, *i.UserId)
		} else {
			if r.URL.Host != c.Policy.Destination.Bucket+".s3.us-east-1.amazonaws.com" || r.Header.Get("X-Amz-Expected-Bucket-Owner") != destination.AccountID {
				t.Error("endpoint or account owner not pinned")
			}
			switch {
			case r.URL.Query().Has("policy"):
				text = `{"Version":"2012-10-17","Statement":[]}`
			case r.URL.Query().Has("versioning"):
				text = "<VersioningConfiguration><Status>Enabled</Status></VersioningConfiguration>"
			case r.URL.Query().Has("object-lock"):
				text = "<ObjectLockConfiguration><ObjectLockEnabled>Enabled</ObjectLockEnabled><Rule><DefaultRetention><Mode>GOVERNANCE</Mode><Days>30</Days></DefaultRetention></Rule></ObjectLockConfiguration>"
			default:
				reads.Add(1)
				key, _ := destination.ObjectKey(claim)
				if r.URL.Query().Get("versionId") != "exact-version" || r.URL.Path != "/"+key {
					t.Error("key or version not pinned")
				}
				if deadline, ok := r.Context().Deadline(); !ok || time.Until(deadline) > 30*time.Second {
					t.Error("request deadline not bounded")
				}
				h := make(http.Header)
				h.Set("Content-Length", "3")
				h.Set("ETag", `"identity"`)
				h.Set("X-Amz-Version-Id", "exact-version")
				h.Set("Last-Modified", now.Format(http.TimeFormat))
				h.Set("X-Amz-Object-Lock-Mode", "GOVERNANCE")
				h.Set("X-Amz-Object-Lock-Retain-Until-Date", now.Add(30*24*time.Hour).Format(time.RFC3339))
				digest, _ := hex.DecodeString(claim.SHA256)
				h.Set("X-Amz-Checksum-Sha256", base64.StdEncoding.EncodeToString(digest))
				for k, v := range custodyMetadata(destination, claim) {
					h.Set("X-Amz-Meta-"+k, v)
				}
				phase := "head-"
				if r.Method == http.MethodGet {
					phase = "get-"
					if r.Header.Get("If-Match") != `"identity"` {
						t.Error("GET ETag not pinned")
					}
				}
				if strings.HasPrefix(fault, phase) {
					switch strings.TrimPrefix(fault, phase) {
					case "version":
						h.Set("X-Amz-Version-Id", "substitution")
					case "missing-version":
						h.Del("X-Amz-Version-Id")
					case "length":
						h.Set("Content-Length", "4")
					case "metadata":
						h.Set("X-Amz-Meta-Producer-User-Id", "other")
					case "checksum":
						h.Set("X-Amz-Checksum-Sha256", base64.StdEncoding.EncodeToString(make([]byte, 32)))
					case "composite":
						h.Set("X-Amz-Checksum-Type", "COMPOSITE")
					case "unknown-checksum-type":
						h.Set("X-Amz-Checksum-Type", "UNKNOWN")
					case "modified":
						h.Set("Last-Modified", now.Add(-time.Hour).Format(http.TimeFormat))
					case "retained-changed":
						h.Set("X-Amz-Object-Lock-Retain-Until-Date", now.Add(31*24*time.Hour).Format(time.RFC3339))
					case "missing-etag":
						h.Del("ETag")
					case "retention":
						h.Set("X-Amz-Object-Lock-Retain-Until-Date", now.Add(-time.Hour).Format(time.RFC3339))
					case "missing-retention":
						h.Del("X-Amz-Object-Lock-Retain-Until-Date")
					case "etag":
						h.Set("ETag", `"other"`)
					}
				}
				if fault == "no-checksum" {
					h.Del("X-Amz-Checksum-Sha256")
				}
				responseBody := io.ReadCloser(io.NopCloser(strings.NewReader("")))
				if r.Method == http.MethodGet {
					responseBody = body
				}
				return &http.Response{StatusCode: 200, Header: h, Body: responseBody}, nil
			}
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(text))}, nil
	})
	r, err := newProtectedReaderConfigured(t.Context(), c, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return r, claim, reads
}

func TestProtectedReadVersionSDKResponsesAndBody(t *testing.T) {
	faults := []string{"", "no-checksum", "bad-bytes", "truncated", "oversized", "close-error", "read-error"}
	for _, phase := range []string{"head-", "get-"} {
		for _, fault := range []string{"version", "missing-version", "length", "metadata", "checksum", "composite", "unknown-checksum-type", "retention", "missing-retention", "missing-etag"} {
			faults = append(faults, phase+fault)
		}
	}
	faults = append(faults, "get-etag", "get-modified", "get-retained-changed")
	for _, fault := range faults {
		t.Run(fault, func(t *testing.T) {
			data := "raw"
			switch fault {
			case "bad-bytes":
				data = "bad"
			case "truncated":
				data = "ra"
			case "oversized":
				data = "raw-extra"
			}
			body := &readVersionBody{Reader: strings.NewReader(data)}
			if fault == "read-error" {
				body.Reader = readVersionErrorReader{}
			}
			if fault == "close-error" {
				body.closeErr = errors.New("close failed")
			}
			r, claim, reads := readVersionFixture(t, fault, body)
			bytes, receipt, err := r.ReadVersion(t.Context(), claim, "exact-version")
			good := fault == "" || fault == "no-checksum"
			if (err == nil) != good {
				t.Fatalf("unexpected result: %v", err)
			}
			if good && (string(bytes) != "raw" || receipt.VersionID != "exact-version" || !receipt.Matches(r.store.destination, claim)) {
				t.Fatal("wrong returned bytes/receipt")
			}
			if !good && (bytes != nil || receipt != (CustodyReceipt{})) {
				t.Fatal("failure returned evidence")
			}
			if !strings.HasPrefix(fault, "head-") && (reads.Load() != 2 || body.closed.Load() != 1) {
				t.Fatalf("GET skipped or body leaked: reads=%d closes=%d", reads.Load(), body.closed.Load())
			}
		})
	}
}

func TestProtectedReadVersionRejectsBeforeRequests(t *testing.T) {
	r, claim, reads := readVersionFixture(t, "", io.NopCloser(strings.NewReader("raw")))
	destination := r.Destination()
	destination.ProducerUserID = "modified-copy"
	if r.Destination().ProducerUserID != readerTestConfig().ExpectedProducerUserID {
		t.Fatal("destination not immutable producer identity")
	}
	for _, version := range []string{"", "null"} {
		if _, _, err := r.ReadVersion(t.Context(), claim, version); err == nil {
			t.Fatal("missing version accepted")
		}
	}
	for _, size := range []int64{-1, r.store.destination.MaxObjectBytes + 1, MaxCustodyObjectBytes + 1} {
		bad := claim
		bad.Bytes = size
		if _, _, err := r.ReadVersion(t.Context(), bad, "exact-version"); err == nil {
			t.Fatal("invalid cap accepted")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := r.ReadVersion(ctx, claim, "exact-version"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if reads.Load() != 0 {
		t.Fatal("invalid input reached object API")
	}
}

type blockedReadVersionBody struct {
	started, closed chan struct{}
	closes          atomic.Int32
}

func (b *blockedReadVersionBody) Read([]byte) (int, error) {
	close(b.started)
	<-b.closed
	return 0, io.ErrClosedPipe
}
func (b *blockedReadVersionBody) Close() error {
	if b.closes.Add(1) == 1 {
		close(b.closed)
	}
	return nil
}
func TestProtectedReadVersionCancellationClosesBlockedBody(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(fmt.Sprint(deadline), func(t *testing.T) {
			body := &blockedReadVersionBody{started: make(chan struct{}), closed: make(chan struct{})}
			r, claim, _ := readVersionFixture(t, "no-checksum", body)
			ctx, cancel := context.WithCancel(t.Context())
			if deadline {
				cancel()
				ctx, cancel = context.WithTimeout(t.Context(), 100*time.Millisecond)
			}
			defer cancel()
			result := make(chan error, 1)
			go func() {
				bytes, receipt, err := r.ReadVersion(ctx, claim, "exact-version")
				if bytes != nil || receipt != (CustodyReceipt{}) {
					result <- errors.New("canceled read returned evidence")
					return
				}
				result <- err
			}()
			select {
			case <-body.started:
			case <-time.After(time.Second):
				t.Fatal("read did not start")
			}
			if !deadline {
				cancel()
			}
			select {
			case err := <-result:
				expected := context.Canceled
				if deadline {
					expected = context.DeadlineExceeded
				}
				if !errors.Is(err, expected) {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("canceled body not joined")
			}
			if body.closes.Load() != 1 {
				t.Fatal("body not closed exactly once")
			}
		})
	}
}
