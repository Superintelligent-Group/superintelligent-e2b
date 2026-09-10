package storage

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Destination returns a value copy of the authenticated producer namespace.
func (r *ProtectedCustodyReader) Destination() CustodyDestination {
	return r.store.destination
}

// ReadVersion returns independently hashed, bounded bytes from one protected
// version. It grants no authority over their meaning or completeness. Callers
// traversing manifests must separately bound their total objects and bytes.
func (r *ProtectedCustodyReader) ReadVersion(ctx context.Context, claim CustodyClaim, version string) ([]byte, CustodyReceipt, error) {
	if version == "" || version == "null" {
		return nil, CustodyReceipt{}, errors.New("exact custody version required")
	}
	if r == nil || r.store == nil {
		return nil, CustodyReceipt{}, errors.New("protected reader required")
	}
	key, err := r.store.destination.ObjectKey(claim)
	if err != nil {
		return nil, CustodyReceipt{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err = ctx.Err(); err != nil {
		return nil, CustodyReceipt{}, err
	}
	if err = verifyProtectedBucket(ctx, r.store.client, r.config); err != nil {
		return nil, CustodyReceipt{}, err
	}
	bucket, owner := aws.String(r.store.destination.Bucket), aws.String(r.store.destination.AccountID)
	head, err := r.store.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: bucket, Key: aws.String(key), ExpectedBucketOwner: owner, VersionId: aws.String(version), ChecksumMode: types.ChecksumModeEnabled})
	if err != nil {
		return nil, CustodyReceipt{}, err
	}
	if err = r.validateReadResponse(claim, version, head.VersionId, head.ContentLength, head.Metadata, head.ChecksumSHA256, head.ChecksumType, head.ObjectLockMode, head.LastModified, head.ObjectLockRetainUntilDate); err != nil {
		return nil, CustodyReceipt{}, err
	}
	if aws.ToString(head.ETag) == "" {
		return nil, CustodyReceipt{}, errors.New("custody HEAD ETag missing")
	}
	object, err := r.store.client.GetObject(ctx, &s3.GetObjectInput{Bucket: bucket, Key: aws.String(key), ExpectedBucketOwner: owner, VersionId: aws.String(version), IfMatch: head.ETag, ChecksumMode: types.ChecksumModeEnabled})
	if err != nil {
		return nil, CustodyReceipt{}, err
	}
	if object.Body == nil {
		return nil, CustodyReceipt{}, errors.New("custody GET body missing")
	}
	// Cancellation also closes a body blocked in Read. Exactly one Close runs;
	// the normal path joins an already-running cancellation close before return.
	var once sync.Once
	var closeErr error
	closeBody := func() { once.Do(func() { closeErr = object.Body.Close() }) }
	stopClose := context.AfterFunc(ctx, closeBody)
	defer stopClose()
	defer closeBody()
	err = r.validateReadResponse(claim, version, object.VersionId, object.ContentLength, object.Metadata, object.ChecksumSHA256, object.ChecksumType, object.ObjectLockMode, object.LastModified, object.ObjectLockRetainUntilDate)
	if err == nil && (aws.ToString(object.ETag) != aws.ToString(head.ETag) || !object.LastModified.Equal(*head.LastModified) || !object.ObjectLockRetainUntilDate.Equal(*head.ObjectLockRetainUntilDate)) {
		err = errors.New("custody HEAD/GET identity changed")
	}
	if err != nil {
		closeBody()
		return nil, CustodyReceipt{}, errors.Join(err, closeErr, ctx.Err())
	}
	// The configured claim cap was checked before requests or allocation. One
	// extra byte distinguishes an oversized stream even when headers lie.
	data := make([]byte, int(claim.Bytes)+1)
	n, readErr := io.ReadFull(object.Body, data)
	closeBody()
	if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
		readErr = nil
	}
	if err = errors.Join(readErr, closeErr, ctx.Err()); err != nil {
		return nil, CustodyReceipt{}, err
	}
	if int64(n) != claim.Bytes {
		return nil, CustodyReceipt{}, errors.New("custody GET body length mismatch")
	}
	data = data[:n]
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != claim.SHA256 {
		return nil, CustodyReceipt{}, errors.New("custody GET body hash mismatch")
	}
	return data, CustodyReceipt{Destination: r.store.destination, Claim: claim, Key: key, VersionID: version}, nil
}

func (r *ProtectedCustodyReader) validateReadResponse(c CustodyClaim, version string, actualVersion *string, length *int64, metadata map[string]string, checksum *string, checksumType types.ChecksumType, mode types.ObjectLockMode, modified, retained *time.Time) error {
	if aws.ToString(actualVersion) != version || length == nil || *length != c.Bytes {
		return errors.New("custody response version or length mismatch")
	}
	if modified == nil || retained == nil || mode != types.ObjectLockModeGovernance || !retained.After(time.Now()) || retained.Before(modified.Add(time.Duration(r.config.RetentionDays)*24*time.Hour)) {
		return errors.New("custody response retention missing")
	}
	for k, v := range custodyMetadata(r.store.destination, c) {
		if metadata[k] != v {
			return errors.New("custody response metadata mismatch")
		}
	}
	digest, _ := hex.DecodeString(c.SHA256) // ObjectKey already validated the claim.
	if (checksumType != "" && checksumType != types.ChecksumTypeFullObject) || (checksum != nil && *checksum != base64.StdEncoding.EncodeToString(digest)) {
		return errors.New("custody response checksum mismatch")
	}
	return nil
}
