package storage

import (
	"context"
	"errors"
	"regexp"
	"strings"
)

// ImmutableCustodyStore is optional: ordinary Blob.Put semantics are unchanged.
// Implementations establish exact remote bytes, not host identity or retention
// policy. Production use additionally requires authorized producers and a
// protected destination prefix. No implementation is enabled automatically.
type ImmutableCustodyStore interface {
	CreateExact(context.Context, CustodyClaim, []byte) (CustodyReceipt, error)
	VerifyExact(context.Context, CustodyClaim) (CustodyReceipt, error)
}
type CustodyClaim struct {
	Name, SHA256 string
	Bytes        int64
}
type CustodyDestination struct {
	AccountID, Region, Bucket, Prefix, ClaimedHostID string
	ProducerUserID                                   string // derived only by the protected constructor; not a host claim
	MaxObjectBytes                                   int64
}
type CustodyReceipt struct {
	Destination    CustodyDestination
	Claim          CustodyClaim
	Key, VersionID string
}

const MaxCustodyObjectBytes int64 = 64 * 1024 * 1024

var custodyAccount = regexp.MustCompile(`^[0-9]{12}$`)
var custodyRegion = regexp.MustCompile(`^[a-z]{2}-[a-z]+-[0-9]+$`)
var custodyBucket = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
var custodyToken = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
var custodyHash = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (d CustodyDestination) Validate() error {
	identityValid := custodyToken.MatchString(d.ClaimedHostID) && d.ProducerUserID == ""
	if d.ProducerUserID != "" {
		identityValid = d.ClaimedHostID == "" && ec2ProducerUserID.MatchString(d.ProducerUserID)
	}
	if !custodyAccount.MatchString(d.AccountID) || !custodyRegion.MatchString(d.Region) || !custodyBucket.MatchString(d.Bucket) || strings.Contains(d.Bucket, "..") || !identityValid || d.MaxObjectBytes < 1 || d.MaxObjectBytes > MaxCustodyObjectBytes {
		return errors.New("invalid custody destination")
	}
	if !strings.HasPrefix(d.Prefix, "network-usage/v1/") || len(d.Prefix) > 256 {
		return errors.New("invalid custody prefix")
	}
	for _, part := range strings.Split(d.Prefix, "/") {
		if !custodyToken.MatchString(part) || part == "." || part == ".." {
			return errors.New("invalid custody prefix component")
		}
	}
	return nil
}
func (d CustodyDestination) ObjectKey(c CustodyClaim) (string, error) {
	if err := d.Validate(); err != nil {
		return "", err
	}
	if !custodyToken.MatchString(c.Name) || c.Name == "." || c.Name == ".." || !custodyHash.MatchString(c.SHA256) || c.Bytes < 0 || c.Bytes > d.MaxObjectBytes {
		return "", errors.New("invalid custody claim")
	}
	identity := d.ClaimedHostID
	if d.ProducerUserID != "" {
		identity = d.ProducerUserID
	}
	return d.Prefix + "/" + d.AccountID + "/" + identity + "/" + c.Name, nil
}
func (r CustodyReceipt) Matches(d CustodyDestination, c CustodyClaim) bool {
	key, err := d.ObjectKey(c)
	return err == nil && r.Destination == d && r.Claim == c && r.Key == key
}
