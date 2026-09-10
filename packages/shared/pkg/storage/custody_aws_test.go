package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

type custodyHTTPDo func(*http.Request) (*http.Response, error)

func (f custodyHTTPDo) Do(r *http.Request) (*http.Response, error) { return f(r) }

func TestAWSCustodyConstructorPinsEndpointsAndAccount(t *testing.T) {
	t.Setenv("AWS_ENDPOINT_URL", "https://untrusted.invalid")
	t.Setenv("AWS_ENDPOINT_URL_S3", "https://untrusted-s3.invalid")
	t.Setenv("AWS_ENDPOINT_URL_STS", "https://untrusted-sts.invalid")
	d := custodyTestDestination()
	for _, account := range []string{d.AccountID, "999999999999"} {
		cfg, e := config.LoadDefaultConfig(t.Context(), config.WithRegion(d.Region), config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("fake", "fake", "")))
		if e != nil {
			t.Fatal(e)
		}
		calls := 0
		cfg.HTTPClient = custodyHTTPDo(func(r *http.Request) (*http.Response, error) {
			calls++
			if calls > 1 {
				if r.URL.Host != "test-bucket.s3.us-east-1.amazonaws.com" {
					t.Fatalf("storage endpoint not pinned: %s", r.URL.Host)
				}
				return &http.Response{StatusCode: 403, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
			}
			if r.URL.Host != "sts.us-east-1.amazonaws.com" {
				t.Fatalf("identity endpoint not pinned: %s", r.URL.Host)
			}
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("<GetCallerIdentityResponse><GetCallerIdentityResult><Account>" + account + "</Account><Arn>arn:aws:sts::" + account + ":assumed-role/client/claimed</Arn><UserId>fake</UserId></GetCallerIdentityResult></GetCallerIdentityResponse>"))}, nil
		})
		store, e := newAWSCustodyConfigured(t.Context(), d, cfg)
		if account != d.AccountID {
			if e == nil {
				t.Fatal("accepted other caller account")
			}
			continue
		}
		if e != nil || calls != 1 {
			t.Fatal(e)
		}
		options := store.(*awsCustody).client.Options()
		if options.BaseEndpoint != nil || options.Region != d.Region || options.UsePathStyle {
			t.Fatal("storage endpoint options not pinned")
		}
		if _, e = store.VerifyExact(t.Context(), custodyTestClaim([]byte("raw"))); e == nil || calls != 2 {
			t.Fatal("expected pinned denied storage request")
		}
	}
}

func custodyTestDestination() CustodyDestination {
	return CustodyDestination{AccountID: "123456789012", Region: "us-east-1", Bucket: "test-bucket", Prefix: "network-usage/v1/test", ClaimedHostID: "claimed-node", MaxObjectBytes: 1024}
}
func custodyTestClaim(data []byte) CustodyClaim {
	h := sha256.Sum256(data)
	return CustodyClaim{Name: "segment.incomplete", SHA256: hex.EncodeToString(h[:]), Bytes: int64(len(data))}
}

func TestAWSCustodyConditionalReplay(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		t.Run(fmt.Sprint("fallback=", fallback), func(t *testing.T) {
			d := custodyTestDestination()
			data := []byte("{raw-partial-tail")
			c := custodyTestClaim(data)
			var saved []byte
			var metadata map[string]string
			puts, heads, gets := 0, 0, 0
			client := newTestS3Client(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Amz-Expected-Bucket-Owner") != d.AccountID {
					t.Error("missing owner pin")
				}
				key, _ := d.ObjectKey(c)
				if r.URL.Path != "/"+d.Bucket+"/"+key {
					t.Error("destination/key mismatch")
				}
				switch r.Method {
				case http.MethodPut:
					puts++
					if r.Header.Get("If-None-Match") != "*" {
						t.Error("missing conditional create")
					}
					sum, _ := hex.DecodeString(c.SHA256)
					if r.Header.Get("X-Amz-Checksum-Sha256") != base64.StdEncoding.EncodeToString(sum) {
						t.Error("missing checksum")
					}
					if saved != nil {
						w.Header().Set("Content-Type", "application/xml")
						w.WriteHeader(412)
						fmt.Fprint(w, "<Error><Code>PreconditionFailed</Code></Error>")
						return
					}
					saved, _ = io.ReadAll(r.Body)
					metadata = s3MetadataFromHeaders(r.Header)
				case http.MethodHead, http.MethodGet:
					if r.Method == http.MethodHead {
						heads++
						if r.Header.Get("X-Amz-Checksum-Mode") != "ENABLED" {
							t.Error("missing checksum request")
						}
					} else {
						gets++
					}
					w.Header().Set("Content-Length", strconv.Itoa(len(saved)))
					w.Header().Set("ETag", `"exact-etag"`)
					w.Header().Set("X-Amz-Version-Id", "version-1")
					for k, v := range metadata {
						w.Header().Set("X-Amz-Meta-"+k, v)
					}
					if !fallback {
						sum := sha256.Sum256(saved)
						w.Header().Set("X-Amz-Checksum-Sha256", base64.StdEncoding.EncodeToString(sum[:]))
						w.Header().Set("X-Amz-Checksum-Type", "FULL_OBJECT")
					}
					if r.Method == http.MethodGet {
						if r.URL.Query().Get("versionId") != "version-1" || r.Header.Get("If-Match") != `"exact-etag"` {
							t.Error("GET not pinned")
						}
						w.Write(saved)
					}
				default:
					t.Error("unexpected method")
				}
			})
			store := &awsCustody{client: client, destination: d}
			for range 2 {
				receipt, e := store.CreateExact(context.Background(), c, data)
				if e != nil || !receipt.Matches(d, c) || receipt.VersionID != "version-1" {
					t.Fatalf("receipt: %+v %v", receipt, e)
				}
			}
			if !bytes.Equal(saved, data) || puts != 2 || heads != 2 || (fallback && gets != 2) {
				t.Fatalf("bad replay: puts=%d heads=%d gets=%d", puts, heads, gets)
			}
		})
	}
}

func TestAWSCustodyRejectsUnverifiedObjects(t *testing.T) {
	for _, fault := range []string{"owner-denied", "conflict", "length", "checksum", "composite", "metadata", "fallback-body", "fallback-metadata"} {
		t.Run(fault, func(t *testing.T) {
			d := custodyTestDestination()
			data := []byte("raw")
			c := custodyTestClaim(data)
			client := newTestS3Client(t, func(w http.ResponseWriter, r *http.Request) {
				if fault == "owner-denied" {
					w.WriteHeader(403)
					return
				}
				if r.Method == http.MethodPut {
					w.Header().Set("Content-Type", "application/xml")
					w.WriteHeader(412)
					fmt.Fprint(w, "<Error><Code>PreconditionFailed</Code></Error>")
					return
				}
				w.Header().Set("Content-Length", "3")
				for k, v := range custodyMetadata(d, c) {
					w.Header().Set("X-Amz-Meta-"+k, v)
				}
				sum := sha256.Sum256(data)
				w.Header().Set("X-Amz-Checksum-Sha256", base64.StdEncoding.EncodeToString(sum[:]))
				w.Header().Set("X-Amz-Checksum-Type", "FULL_OBJECT")
				switch fault {
				case "conflict", "checksum":
					w.Header().Set("X-Amz-Checksum-Sha256", "wrong")
				case "length":
					w.Header().Set("Content-Length", "4")
				case "composite":
					w.Header().Set("X-Amz-Checksum-Type", "COMPOSITE")
				case "metadata":
					w.Header().Set("X-Amz-Meta-claimed-host-id", "other")
				case "fallback-body", "fallback-metadata":
					w.Header().Del("X-Amz-Checksum-Sha256")
					if r.Method == http.MethodGet {
						if fault == "fallback-metadata" {
							w.Header().Set("X-Amz-Meta-claimed-host-id", "other")
							w.Write(data)
						} else {
							w.Write([]byte("bad"))
						}
					}
				}
			})
			store := &awsCustody{client: client, destination: d}
			if _, e := store.CreateExact(context.Background(), c, data); e == nil {
				t.Fatal("accepted unverifiable custody")
			}
		})
	}
}

func TestAWSCustodyRejectsInvalidInputBeforeRequest(t *testing.T) {
	client := newTestS3Client(t, func(http.ResponseWriter, *http.Request) { t.Error("invalid input reached network") })
	d := custodyTestDestination()
	store := &awsCustody{client: client, destination: d}
	c := custodyTestClaim([]byte("raw"))
	if _, e := store.CreateExact(context.Background(), c, []byte("bad")); e == nil {
		t.Fatal("accepted incorrect local hash")
	}
	for _, name := range []string{"../escape", "a/b", "..", ""} {
		bad := c
		bad.Name = name
		if _, e := store.VerifyExact(context.Background(), bad); e == nil {
			t.Fatal("accepted unsafe key")
		}
	}
	for _, prefix := range []string{"templates", "network-usage/v1/../x", "network-usage/v1/x/"} {
		bad := d
		bad.Prefix = prefix
		if e := bad.Validate(); e == nil {
			t.Fatal("accepted unsafe prefix")
		}
	}
	bad := d
	bad.AccountID = "ambient"
	if e := bad.Validate(); e == nil {
		t.Fatal("accepted unpinned owner")
	}
}

func TestAWSCustodyConcurrentReplay(t *testing.T) {
	d := custodyTestDestination()
	data := []byte("same")
	c := custodyTestClaim(data)
	var mu sync.Mutex
	exists := false
	client := newTestS3Client(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodPut {
			if exists {
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(412)
				fmt.Fprint(w, "<Error><Code>PreconditionFailed</Code></Error>")
			} else {
				exists = true
			}
			return
		}
		w.Header().Set("Content-Length", "4")
		for k, v := range custodyMetadata(d, c) {
			w.Header().Set("X-Amz-Meta-"+k, v)
		}
		sum := sha256.Sum256(data)
		w.Header().Set("X-Amz-Checksum-Sha256", base64.StdEncoding.EncodeToString(sum[:]))
	})
	store := &awsCustody{client: client, destination: d}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			r, e := store.CreateExact(context.Background(), c, data)
			if e != nil || !r.Matches(d, c) {
				t.Errorf("replay: %v", e)
			}
		})
	}
	wg.Wait()
}

func TestAWSCustodyConditionalConflictRetainsUncertainty(t *testing.T) {
	d := custodyTestDestination()
	data := []byte("raw")
	c := custodyTestClaim(data)
	client := newTestS3Client(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Error("409 must not be treated as verified custody")
		}
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusConflict)
		fmt.Fprint(w, "<Error><Code>ConditionalRequestConflict</Code></Error>")
	})
	store := &awsCustody{client: client, destination: d}
	if _, e := store.CreateExact(t.Context(), c, data); e == nil {
		t.Fatal("409 returned custody receipt")
	}
}
