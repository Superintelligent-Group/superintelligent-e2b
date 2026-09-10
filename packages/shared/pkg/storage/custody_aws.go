package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
)

type awsCustody struct {
	client        *s3.Client
	destination   CustodyDestination
	identity      *sts.GetCallerIdentityOutput
	retentionDays int
}

var _ ImmutableCustodyStore = (*awsCustody)(nil)

// NewAWSCustody reuses the existing AWS credential chain, but rejects a caller
// account mismatch and uses AWS regional endpoints rather than custom endpoints.
// Account equality authenticates the credential account, not ClaimedHostID.
func NewAWSCustody(ctx context.Context, d CustodyDestination) (ImmutableCustodyStore, error) {
	if d.ProducerUserID != "" {
		return nil, errors.New("producer namespace requires protected custody constructor")
	}
	if err := d.Validate(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(d.Region))
	if err != nil {
		return nil, err
	}
	return newAWSCustodyConfigured(ctx, d, cfg)
}

func newAWSCustodyConfigured(ctx context.Context, d CustodyDestination, cfg aws.Config) (ImmutableCustodyStore, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	cfg.Region = d.Region
	// Service functional options below have precedence over configured/global
	// endpoint URLs. Clear legacy resolver hooks as well.
	cfg.BaseEndpoint = nil
	cfg.EndpointResolver = nil
	cfg.EndpointResolverWithOptions = nil
	identityClient := sts.NewFromConfig(cfg, func(o *sts.Options) { o.BaseEndpoint = nil; o.EndpointResolverV2 = sts.NewDefaultEndpointResolverV2() })
	identity, err := identityClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, err
	}
	if aws.ToString(identity.Account) != d.AccountID {
		return nil, errors.New("custody caller account mismatch")
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = nil
		o.EndpointResolverV2 = s3.NewDefaultEndpointResolverV2()
		o.UsePathStyle = false
	})
	return &awsCustody{client: client, destination: d, identity: identity}, nil
}
func custodyMetadata(d CustodyDestination, c CustodyClaim) map[string]string {
	if d.ProducerUserID != "" {
		return map[string]string{"custody-schema": "raw-segment-v1", "sha256": c.SHA256, "producer-user-id": d.ProducerUserID}
	}
	return map[string]string{"custody-schema": "raw-segment-v1", "sha256": c.SHA256, "claimed-host-id": d.ClaimedHostID}
}
func (s *awsCustody) CreateExact(ctx context.Context, c CustodyClaim, data []byte) (CustodyReceipt, error) {
	key, err := s.destination.ObjectKey(c)
	if err != nil {
		return CustodyReceipt{}, err
	}
	digest := sha256.Sum256(data)
	if int64(len(data)) != c.Bytes || hex.EncodeToString(digest[:]) != c.SHA256 {
		return CustodyReceipt{}, errors.New("custody local bytes mismatch")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.destination.Bucket), Key: aws.String(key), ExpectedBucketOwner: aws.String(s.destination.AccountID), IfNoneMatch: aws.String("*"), Body: bytes.NewReader(data), ContentLength: aws.Int64(c.Bytes), ChecksumSHA256: aws.String(base64.StdEncoding.EncodeToString(digest[:])), ChecksumAlgorithm: types.ChecksumAlgorithmSha256, Metadata: custodyMetadata(s.destination, c)})
	if err != nil {
		var api smithy.APIError
		if !errors.As(err, &api) || api.ErrorCode() != "PreconditionFailed" {
			return CustodyReceipt{}, err
		}
	}
	// Even a successful PUT is verified before giving the caller a retention receipt.
	return s.VerifyExact(ctx, c)
}
func (s *awsCustody) VerifyExact(ctx context.Context, c CustodyClaim) (CustodyReceipt, error) {
	return s.verifyExactVersion(ctx, c, "")
}
func (s *awsCustody) verifyExactVersion(ctx context.Context, c CustodyClaim, versionID string) (CustodyReceipt, error) {
	key, err := s.destination.ObjectKey(c)
	if err != nil {
		return CustodyReceipt{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var version *string
	if versionID != "" {
		version = aws.String(versionID)
	}
	head, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.destination.Bucket), Key: aws.String(key), ExpectedBucketOwner: aws.String(s.destination.AccountID), ChecksumMode: types.ChecksumModeEnabled, VersionId: version})
	if err != nil {
		return CustodyReceipt{}, err
	}
	if versionID != "" && aws.ToString(head.VersionId) != versionID {
		return CustodyReceipt{}, errors.New("custody exact version mismatch")
	}
	if head.ContentLength == nil || *head.ContentLength != c.Bytes {
		return CustodyReceipt{}, errors.New("custody remote length mismatch")
	}
	if s.retentionDays > 0 {
		if aws.ToString(head.VersionId) == "" || aws.ToString(head.VersionId) == "null" || head.LastModified == nil || head.ObjectLockMode != types.ObjectLockModeGovernance || head.ObjectLockRetainUntilDate == nil || !head.ObjectLockRetainUntilDate.After(time.Now()) || head.ObjectLockRetainUntilDate.Before(head.LastModified.Add(time.Duration(s.retentionDays)*24*time.Hour)) {
			return CustodyReceipt{}, errors.New("protected custody version or retention evidence missing")
		}
	}
	for k, v := range custodyMetadata(s.destination, c) {
		if head.Metadata[k] != v {
			return CustodyReceipt{}, errors.New("custody remote metadata mismatch")
		}
	}
	sum, err := hex.DecodeString(c.SHA256)
	if err != nil {
		return CustodyReceipt{}, err
	}
	if head.ChecksumSHA256 != nil {
		if *head.ChecksumSHA256 != base64.StdEncoding.EncodeToString(sum) || head.ChecksumType == types.ChecksumTypeComposite {
			return CustodyReceipt{}, errors.New("custody remote checksum mismatch")
		}
	} else {
		// Metadata alone is not integrity evidence. Read exact bounded bytes instead.
		// Pin the HEAD version when available; otherwise compare response metadata too.
		object, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.destination.Bucket), Key: aws.String(key), ExpectedBucketOwner: aws.String(s.destination.AccountID), VersionId: head.VersionId, IfMatch: head.ETag})
		if err != nil {
			return CustodyReceipt{}, err
		}
		h := sha256.New()
		n, copyErr := io.Copy(h, io.LimitReader(object.Body, c.Bytes+1))
		closeErr := object.Body.Close()
		if err = errors.Join(copyErr, closeErr); err != nil {
			return CustodyReceipt{}, err
		}
		if n != c.Bytes || hex.EncodeToString(h.Sum(nil)) != c.SHA256 {
			return CustodyReceipt{}, errors.New("custody remote bytes mismatch")
		}
		for k, v := range custodyMetadata(s.destination, c) {
			if object.Metadata[k] != v {
				return CustodyReceipt{}, errors.New("custody GET metadata mismatch")
			}
		}
	}
	return CustodyReceipt{Destination: s.destination, Claim: c, Key: key, VersionID: aws.ToString(head.VersionId)}, nil
}

func (s *awsCustody) String() string {
	return fmt.Sprintf("AWS raw custody: %s/%s", s.destination.Bucket, s.destination.Prefix)
}
