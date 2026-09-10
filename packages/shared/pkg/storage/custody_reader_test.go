package storage

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

func readerTestConfig() ProtectedReaderConfig {
	return ProtectedReaderConfig{Policy: protectedTestConfig(), ReaderRoleARN: "arn:aws:iam::123456789012:role/reader", ReaderRoleID: "AROA98765432109876543", ExpectedProducerUserID: "AROA12345678901234567:i-0123456789abcdef0"}
}
func readerTestIdentity() *sts.GetCallerIdentityOutput {
	return &sts.GetCallerIdentityOutput{Account: aws.String("123456789012"), UserId: aws.String("AROA98765432109876543:sig-verifier"), Arn: aws.String("arn:aws:sts::123456789012:assumed-role/reader/sig-verifier")}
}
func TestProtectedReaderIdentityBoundary(t *testing.T) {
	c := readerTestConfig()
	if e := validateReaderIdentity(c, readerTestIdentity()); e != nil {
		t.Fatal(e)
	}
	if e := validateReaderIdentity(c, protectedTestIdentity()); e == nil {
		t.Fatal("producer accepted as independent reader")
	}
	wrong := readerTestIdentity()
	wrong.Account = aws.String("999999999999")
	if e := validateReaderIdentity(c, wrong); e == nil {
		t.Fatal("wrong reader account accepted")
	}
	wrong = readerTestIdentity()
	wrong.Arn = aws.String("arn:aws:sts::123456789012:assumed-role/admin/sig-verifier")
	if e := validateReaderIdentity(c, wrong); e == nil {
		t.Fatal("wrong reader role accepted")
	}
	bad := c
	bad.ExpectedProducerUserID = "AROA00000000000000000:i-0123456789abcdef0"
	if e := bad.Validate(); e == nil {
		t.Fatal("other producer role accepted")
	}
	var reader ProtectedCustodyReader
	if _, ok := any(&reader).(ImmutableCustodyStore); ok {
		t.Fatal("reader exposes write/latest custody API")
	}
	for _, version := range []string{"", "null"} {
		if _, e := reader.VerifyVersion(t.Context(), custodyTestClaim([]byte("raw")), version); e == nil {
			t.Fatal("missing exact version accepted")
		}
	}
}
func TestProtectedReaderConstructsWithReaderCredentials(t *testing.T) {
	c := readerTestConfig()
	template, err := os.ReadFile("../../../../iac/provider-aws/modules/network-evidence/policy.json.tftpl")
	if err != nil {
		t.Fatal(err)
	}
	policy := strings.NewReplacer("$${aws:userid}", "__USER_ID__", "${bucket}", c.Policy.Destination.Bucket, "${account}", c.Policy.Destination.AccountID, "${prefix}", c.Policy.Destination.Prefix, "${producer}", c.Policy.ProducerRoleARN, "${reader}", c.ReaderRoleARN).Replace(string(template))
	policy = strings.ReplaceAll(policy, "__USER_ID__", "${aws:userid}")
	c.Policy.PolicySHA256, err = canonicalPolicyHash(policy)
	if err != nil {
		t.Fatal(err)
	}
	fault := ""
	objectReads := 0
	claim := custodyTestClaim([]byte("raw"))
	destination := c.Policy.Destination
	destination.ProducerUserID = c.ExpectedProducerUserID
	cfg := aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("fake", "fake", "")}
	cfg.HTTPClient = custodyHTTPDo(func(r *http.Request) (*http.Response, error) {
		body := ""
		if r.URL.Host == "sts.us-east-1.amazonaws.com" {
			identity := readerTestIdentity()
			body = fmt.Sprintf("<GetCallerIdentityResponse><GetCallerIdentityResult><Account>%s</Account><Arn>%s</Arn><UserId>%s</UserId></GetCallerIdentityResult></GetCallerIdentityResponse>", *identity.Account, *identity.Arn, *identity.UserId)
		} else {
			if r.Header.Get("X-Amz-Expected-Bucket-Owner") != c.Policy.Destination.AccountID {
				t.Error("missing owner pin")
			}
			switch {
			case r.URL.Query().Has("policy"):
				body = policy
			case r.URL.Query().Has("versioning"):
				body = "<VersioningConfiguration><Status>Enabled</Status></VersioningConfiguration>"
			case r.URL.Query().Has("object-lock"):
				body = "<ObjectLockConfiguration><ObjectLockEnabled>Enabled</ObjectLockEnabled><Rule><DefaultRetention><Mode>GOVERNANCE</Mode><Days>30</Days></DefaultRetention></Rule></ObjectLockConfiguration>"
			default:
				objectReads++
				key, _ := destination.ObjectKey(claim)
				if r.URL.Query().Get("versionId") != "exact-version" || !strings.HasSuffix(r.URL.Path, key) {
					t.Error("reader did not pin producer key/version")
				}
				h := make(http.Header)
				h.Set("Content-Length", "3")
				h.Set("X-Amz-Version-Id", "exact-version")
				now := time.Now().UTC().Truncate(time.Second)
				h.Set("Last-Modified", now.Format(http.TimeFormat))
				h.Set("X-Amz-Object-Lock-Mode", "GOVERNANCE")
				h.Set("X-Amz-Object-Lock-Retain-Until-Date", now.Add(30*24*time.Hour).Format(time.RFC3339))
				for k, v := range custodyMetadata(destination, claim) {
					h.Set("X-Amz-Meta-"+k, v)
				}
				if fault == "wrong-version" {
					h.Set("X-Amz-Version-Id", "other")
				}
				if fault == "expired" {
					h.Set("X-Amz-Object-Lock-Retain-Until-Date", now.Add(-time.Hour).Format(time.RFC3339))
				}
				if fault == "wrong-producer" {
					h.Set("X-Amz-Meta-Producer-User-Id", "other")
				}
				if r.Method == http.MethodGet {
					body = "raw"
					if fault == "wrong-bytes" {
						body = "bad"
					}
				}
				return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(body))}, nil
			}
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	reader, e := newProtectedReaderConfigured(t.Context(), c, cfg)
	if e != nil {
		t.Fatal(e)
	}
	if reader.store.destination.ProducerUserID != c.ExpectedProducerUserID {
		t.Fatal("reader rebound source to its own identity")
	}
	for _, f := range []string{"", "wrong-version", "expired", "wrong-producer", "wrong-bytes"} {
		fault = f
		objectReads = 0
		_, e := reader.VerifyVersion(t.Context(), claim, "exact-version")
		if (e == nil) != (f == "") {
			t.Fatalf("reader response %q: %v", f, e)
		}
		want := 1
		if f == "" || f == "wrong-bytes" {
			want = 2
		}
		if objectReads != want {
			t.Fatalf("%s: object reads %d, want %d", f, objectReads, want)
		}
	}
}
