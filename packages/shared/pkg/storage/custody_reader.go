package storage

import (
	"context"
	"errors"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"strings"
	"time"
)

type ProtectedReaderConfig struct {
	Policy                                              ProtectedCustodyConfig
	ReaderRoleARN, ReaderRoleID, ExpectedProducerUserID string
}

// ProtectedCustodyReader deliberately exposes neither writes nor latest reads.
type ProtectedCustodyReader struct {
	store  *awsCustody
	config ProtectedCustodyConfig
}

func (c ProtectedReaderConfig) Validate() error {
	if err := c.Policy.Validate(); err != nil {
		return err
	}
	if !producerRoleARN.MatchString(c.ReaderRoleARN) || !strings.HasPrefix(c.ReaderRoleARN, "arn:aws:iam::"+c.Policy.Destination.AccountID+":role/") || c.ReaderRoleARN == c.Policy.ProducerRoleARN || !producerRoleID.MatchString(c.ReaderRoleID) || c.ReaderRoleID == c.Policy.ProducerRoleID || !ec2ProducerUserID.MatchString(c.ExpectedProducerUserID) || !strings.HasPrefix(c.ExpectedProducerUserID, c.Policy.ProducerRoleID+":") {
		return errors.New("explicit distinct reader and expected producer identities required")
	}
	return nil
}
func validateReaderIdentity(c ProtectedReaderConfig, identity *sts.GetCallerIdentityOutput) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if identity == nil {
		return errors.New("missing reader STS identity")
	}
	parts := strings.SplitN(aws.ToString(identity.UserId), ":", 2)
	if aws.ToString(identity.Account) != c.Policy.Destination.AccountID || len(parts) != 2 || parts[0] != c.ReaderRoleID || parts[1] == "" {
		return errors.New("reader account or role identity mismatch")
	}
	roleName := c.ReaderRoleARN[strings.LastIndex(c.ReaderRoleARN, "/")+1:]
	if aws.ToString(identity.Arn) != "arn:aws:sts::"+c.Policy.Destination.AccountID+":assumed-role/"+roleName+"/"+parts[1] {
		return errors.New("reader role session ARN mismatch")
	}
	return nil
}
func NewProtectedCustodyReader(ctx context.Context, c ProtectedReaderConfig) (*ProtectedCustodyReader, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(c.Policy.Destination.Region))
	if err != nil {
		return nil, err
	}
	return newProtectedReaderConfigured(ctx, c, cfg)
}
func newProtectedReaderConfigured(ctx context.Context, c ProtectedReaderConfig, cfg aws.Config) (*ProtectedCustodyReader, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	d := c.Policy.Destination
	d.ClaimedHostID = "unbound"
	base, err := newAWSCustodyConfigured(ctx, d, cfg)
	if err != nil {
		return nil, err
	}
	store := base.(*awsCustody)
	if err = validateReaderIdentity(c, store.identity); err != nil {
		return nil, err
	}
	if err = verifyProtectedBucket(ctx, store.client, c.Policy); err != nil {
		return nil, err
	}
	store.destination = c.Policy.Destination
	store.destination.ProducerUserID = c.ExpectedProducerUserID
	store.retentionDays = c.Policy.RetentionDays
	return &ProtectedCustodyReader{store: store, config: c.Policy}, nil
}
func (r *ProtectedCustodyReader) VerifyVersion(ctx context.Context, c CustodyClaim, version string) (CustodyReceipt, error) {
	if version == "" || version == "null" {
		return CustodyReceipt{}, errors.New("exact custody version required")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := verifyProtectedBucket(ctx, r.store.client, r.config); err != nil {
		return CustodyReceipt{}, err
	}
	return r.store.verifyExactVersion(ctx, c, version)
}
