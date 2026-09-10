package storage

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

func protectedTestConfig() ProtectedCustodyConfig {
	d := custodyTestDestination()
	d.ClaimedHostID = ""
	hash, _ := canonicalPolicyHash(`{"Version":"2012-10-17","Statement":[]}`)
	return ProtectedCustodyConfig{Destination: d, ProducerRoleARN: "arn:aws:iam::123456789012:role/client", ProducerRoleID: "AROA12345678901234567", PolicySHA256: hash, RetentionDays: 30}
}
func protectedTestIdentity() *sts.GetCallerIdentityOutput {
	return &sts.GetCallerIdentityOutput{Account: aws.String("123456789012"), UserId: aws.String("AROA12345678901234567:i-0123456789abcdef0"), Arn: aws.String("arn:aws:sts::123456789012:assumed-role/client/i-0123456789abcdef0")}
}

func TestProtectedCustodyIdentityRejectsClaims(t *testing.T) {
	c := protectedTestConfig()
	identity := protectedTestIdentity()
	p, e := deriveEC2Producer(c, identity)
	if e != nil || p.InstanceID != "i-0123456789abcdef0" {
		t.Fatal(e)
	}
	for _, fault := range []string{"account", "role-id", "role-name", "non-ec2", "mismatched-session"} {
		t.Run(fault, func(t *testing.T) {
			bad := protectedTestIdentity()
			switch fault {
			case "account":
				bad.Account = aws.String("999999999999")
			case "role-id":
				bad.UserId = aws.String("AROA00000000000000000:i-0123456789abcdef0")
			case "role-name":
				bad.Arn = aws.String("arn:aws:sts::123456789012:assumed-role/other/i-0123456789abcdef0")
			case "non-ec2":
				bad.UserId = aws.String("AROA12345678901234567:admin")
			case "mismatched-session":
				bad.Arn = aws.String("arn:aws:sts::123456789012:assumed-role/client/i-11111111111111111")
			}
			if _, e := deriveEC2Producer(c, bad); e == nil {
				t.Fatal("accepted incorrect producer identity")
			}
		})
	}
	for _, fault := range []string{"policy", "retention", "claimed-host", "claimed-user"} {
		bad := c
		switch fault {
		case "policy":
			bad.PolicySHA256 = ""
		case "retention":
			bad.RetentionDays = 0
		case "claimed-host":
			bad.Destination.ClaimedHostID = "pretend"
		case "claimed-user":
			bad.Destination.ProducerUserID = p.UserID
		}
		if e := bad.Validate(); e == nil {
			t.Fatal("accepted missing authority or caller claim")
		}
	}
	d := c.Destination
	d.ProducerUserID = p.UserID
	key, e := d.ObjectKey(custodyTestClaim([]byte("raw")))
	if e != nil || !strings.Contains(key, "/"+p.UserID+"/") {
		t.Fatal("producer namespace mismatch")
	}
	if _, e := NewAWSCustody(context.Background(), d); e == nil {
		t.Fatal("legacy constructor accepted protected namespace")
	}
}

func TestProtectedCustodyRequiresBucketEvidence(t *testing.T) {
	for _, fault := range []string{"", "policy", "versioning", "lock", "duration", "denied"} {
		t.Run(fault, func(t *testing.T) {
			c := protectedTestConfig()
			client := newTestS3Client(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Amz-Expected-Bucket-Owner") != c.Destination.AccountID {
					t.Error("missing expected owner")
				}
				if fault == "denied" {
					w.WriteHeader(403)
					return
				}
				switch {
				case r.URL.Query().Has("policy"):
					if fault == "policy" {
						fmt.Fprint(w, `{"Statement":[{"Effect":"Allow"}]}`)
					} else {
						fmt.Fprint(w, `{"Version":"2012-10-17","Statement":[]}`)
					}
				case r.URL.Query().Has("versioning"):
					status := "Enabled"
					if fault == "versioning" {
						status = "Suspended"
					}
					fmt.Fprintf(w, "<VersioningConfiguration><Status>%s</Status></VersioningConfiguration>", status)
				case r.URL.Query().Has("object-lock"):
					if fault == "lock" {
						fmt.Fprint(w, "<ObjectLockConfiguration/>")
						return
					}
					days := 30
					if fault == "duration" {
						days = 1
					}
					fmt.Fprintf(w, "<ObjectLockConfiguration><ObjectLockEnabled>Enabled</ObjectLockEnabled><Rule><DefaultRetention><Mode>GOVERNANCE</Mode><Days>%d</Days></DefaultRetention></Rule></ObjectLockConfiguration>", days)
				default:
					t.Error("unexpected bucket request")
				}
			})
			e := verifyProtectedBucket(t.Context(), client, c)
			if (e == nil) != (fault == "") {
				t.Fatalf("evidence %q: %v", fault, e)
			}
		})
	}
}

func TestProtectedCustodyRequiresRetainedExactVersion(t *testing.T) {
	for _, fault := range []string{"", "no-version", "null-version", "wrong-version", "expired", "short-retention", "no-lock"} {
		t.Run(fault, func(t *testing.T) {
			d := custodyTestDestination()
			c := custodyTestClaim([]byte("raw"))
			now := time.Now().UTC().Truncate(time.Second)
			client := newTestS3Client(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("versionId") != "expected-version" {
					t.Error("reader did not pin exact version")
				}
				w.Header().Set("Content-Length", "3")
				w.Header().Set("Last-Modified", now.Format(http.TimeFormat))
				w.Header().Set("X-Amz-Version-Id", "expected-version")
				w.Header().Set("X-Amz-Object-Lock-Mode", "GOVERNANCE")
				w.Header().Set("X-Amz-Object-Lock-Retain-Until-Date", now.Add(30*24*time.Hour).Format(time.RFC3339))
				for k, v := range custodyMetadata(d, c) {
					w.Header().Set("X-Amz-Meta-"+k, v)
				}
				// This fixture exercises retained HEAD rejection before checksum fallback.
				switch fault {
				case "no-version":
					w.Header().Del("X-Amz-Version-Id")
				case "null-version":
					w.Header().Set("X-Amz-Version-Id", "null")
				case "wrong-version":
					w.Header().Set("X-Amz-Version-Id", "other")
				case "expired":
					w.Header().Set("X-Amz-Object-Lock-Retain-Until-Date", now.Add(-time.Hour).Format(time.RFC3339))
				case "short-retention":
					w.Header().Set("X-Amz-Object-Lock-Retain-Until-Date", now.Add(time.Hour).Format(time.RFC3339))
				case "no-lock":
					w.Header().Del("X-Amz-Object-Lock-Mode")
				}
				if r.Method == http.MethodGet {
					fmt.Fprint(w, "raw")
				}
			})
			store := &awsCustody{client: client, destination: d, retentionDays: 30}
			_, e := store.verifyExactVersion(t.Context(), c, "expected-version")
			if (e == nil) != (fault == "") {
				t.Fatalf("retention %q: %v", fault, e)
			}
		})
	}
}
