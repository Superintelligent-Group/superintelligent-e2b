package networkusage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/e2b-dev/infra/packages/shared/pkg/storage"
)

type EvidenceConsumer struct {
	reader        evidenceVersionReader
	options       ConsumerOptions
	identity      VerifierIdentity
	dir           string
	lock          *os.File
	gate          chan struct{}
	failed        error
	closed        bool
	used          int64
	entries       int
	syncFile      func(*os.File) error
	syncDirectory func() error
}
type consumerBinding struct {
	Schema   string
	Root     closingClaim
	Verifier VerifierIdentity
}
type consumerOutputClaim struct {
	Schema, SlotSHA256, RootIdentitySHA256 string
	Receipt                                storage.CustodyClaim
	Verifier                               VerifierIdentity
}

func consumerSlot(root closingClaim) string {
	data, _ := json.Marshal(struct {
		AccountID, Region, Bucket, ProducerUserID, Key string
	}{root.Receipt.Destination.AccountID, root.Receipt.Destination.Region, root.Receipt.Destination.Bucket, root.Receipt.Destination.ProducerUserID, root.Receipt.Key})
	return closingObjectClaim("slot", data).SHA256
}

var consumerControlName = regexp.MustCompile(`^[0-9a-f]{64}\.(binding|receipt|claim)(\.tmp)?$`)

// OpenProtectedEvidenceConsumer constructs only the distinct authenticated
// protected reader. A clean Go VCS build identity is required; it is recorded
// as build metadata, never represented as a cryptographic attestation.
func OpenProtectedEvidenceConsumer(ctx context.Context, directory string, options ConsumerOptions, config storage.ProtectedReaderConfig) (*EvidenceConsumer, error) {
	if err := options.validate(); err != nil {
		return nil, err
	}
	identity, err := ConsumerBuildIdentity()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	reader, err := storage.NewProtectedCustodyReader(ctx, config)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return newEvidenceConsumer(directory, options, reader, identity)
}
func newEvidenceConsumer(directory string, options ConsumerOptions, reader evidenceVersionReader, identity VerifierIdentity) (*EvidenceConsumer, error) {
	if err := options.validate(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(directory) || reader == nil {
		return nil, errors.New("explicit reader and absolute output directory required")
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("invalid consumer output directory")
	}
	lock, err := lockSpool(filepath.Join(directory, ".consumer.lock"))
	if err != nil {
		return nil, err
	}
	c := &EvidenceConsumer{reader: reader, options: options, identity: identity, dir: directory, lock: lock, gate: make(chan struct{}, 1)}
	c.gate <- struct{}{}
	if err = c.recover(); err != nil {
		unlockSpool(lock)
		return nil, err
	}
	return c, nil
}
func (c *EvidenceConsumer) recover() error {
	entries, err := boundedClosingDirectory(c.dir, c.options.MaxOutputEntries+2)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == ".consumer.lock" {
			continue
		}
		if !consumerControlName.MatchString(name) {
			return errors.New("unexpected consumer output control")
		}
		data, err := c.read(name)
		if err != nil {
			return err
		}
		if strings.HasSuffix(name, ".tmp") {
			if err = os.Remove(filepath.Join(c.dir, name)); err != nil {
				return err
			}
			continue
		}
		if strings.HasSuffix(name, ".claim") {
			if _, err = c.retained(strings.TrimSuffix(name, ".claim")); err != nil {
				return err
			}
		} else if strings.HasSuffix(name, ".receipt") {
			var receipt NonMonetaryReceipt
			if err = decodeConsumerJSON(data, &receipt); err != nil {
				return err
			}
			if receipt.Schema != "sig.network-nonmonetary-evidence.v1" || receipt.Complete || receipt.SessionMapping != "unresolved" || receipt.RootSlotSHA256 != strings.TrimSuffix(name, ".receipt") {
				return errors.New("invalid retained consumer receipt")
			}
		} else {
			var binding consumerBinding
			if err = decodeConsumerJSON(data, &binding); err != nil {
				return err
			}
			if binding.Schema != "sig.consumer-root-binding.v1" {
				return errors.New("invalid consumer root binding")
			}
		}
		c.used += int64(len(data))
		c.entries++
		if c.used > c.options.MaxOutputBytes || c.entries > c.options.MaxOutputEntries {
			return errors.New("retained output budget")
		}
	}
	// Recovery establishes a durability boundary before any visible receipt can
	// be returned. A prior unsynced rename is not itself a successful ack.
	return c.syncDir()
}
func (c *EvidenceConsumer) read(name string) ([]byte, error) {
	f, err := openSpoolControl(filepath.Join(c.dir, name), os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, c.options.MaxReceiptBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > c.options.MaxReceiptBytes {
		return nil, errors.New("consumer control byte budget")
	}
	return data, nil
}
func (c *EvidenceConsumer) syncDir() error {
	if c.syncDirectory != nil {
		return c.syncDirectory()
	}
	f, err := os.Open(c.dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
func (c *EvidenceConsumer) persist(name string, data []byte) (err error) {
	if c.failed != nil {
		return c.failed
	}
	defer func() {
		if err != nil {
			c.failed = err
		}
	}()
	if !consumerControlName.MatchString(name) || strings.HasSuffix(name, ".tmp") {
		return errors.New("invalid consumer output name")
	}
	prior, err := c.read(name)
	if err == nil {
		if !bytes.Equal(prior, data) {
			return errors.New("immutable consumer output conflict")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if int64(len(data)) > c.options.MaxReceiptBytes || int64(len(data)) > c.options.MaxOutputBytes-c.used || c.entries >= c.options.MaxOutputEntries {
		return errors.New("consumer output budget")
	}
	f, err := openSpoolControl(filepath.Join(c.dir, name+".tmp"), os.O_CREATE|os.O_EXCL|os.O_WRONLY)
	if err != nil {
		return err
	}
	n, writeErr := f.Write(data)
	if writeErr == nil && n != len(data) {
		writeErr = io.ErrShortWrite
	}
	syncFile := c.syncFile
	if syncFile == nil {
		syncFile = func(file *os.File) error { return file.Sync() }
	}
	err = errors.Join(writeErr, syncFile(f), f.Close())
	if err != nil {
		return err
	}
	if err = os.Rename(filepath.Join(c.dir, name+".tmp"), filepath.Join(c.dir, name)); err != nil {
		return err
	}
	if err = c.syncDir(); err != nil {
		return err
	}
	c.used += int64(len(data))
	c.entries++
	return nil
}

func (c *EvidenceConsumer) retained(key string) (*NonMonetaryReceipt, error) {
	data, err := c.read(key + ".claim")
	if err != nil {
		return nil, err
	}
	var claim consumerOutputClaim
	if err = decodeConsumerJSON(data, &claim); err != nil {
		return nil, err
	}
	if claim.Schema != "sig.consumer-output-claim.v1" || claim.SlotSHA256 != key || claim.Receipt.Name != key+".receipt" {
		return nil, errors.New("invalid immutable output claim")
	}
	raw, err := c.read(key + ".receipt")
	if err != nil {
		return nil, errors.New("claimed output receipt unavailable")
	}
	if closingObjectClaim(key+".receipt", raw) != claim.Receipt {
		return nil, errors.New("immutable output content conflict")
	}
	var result NonMonetaryReceipt
	if err = decodeConsumerJSON(raw, &result); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(result.RootClaim)
	if err != nil {
		return nil, err
	}
	if result.Schema != "sig.network-nonmonetary-evidence.v1" || result.Complete || result.SessionMapping != "unresolved" || result.Verifier != claim.Verifier || result.RootSlotSHA256 != key || consumerSlot(result.RootClaim) != key || result.RootIdentitySHA256 != claim.RootIdentitySHA256 || closingObjectClaim("root", canonical).SHA256 != result.RootIdentitySHA256 {
		return nil, errors.New("immutable output identity conflict")
	}
	bound, err := c.read(key + ".binding")
	if err != nil {
		return nil, errors.New("claimed output binding unavailable")
	}
	expected, _ := json.Marshal(consumerBinding{Schema: "sig.consumer-root-binding.v1", Root: result.RootClaim, Verifier: result.Verifier})
	if !bytes.Equal(bound, expected) {
		return nil, errors.New("immutable output binding conflict")
	}
	return &result, nil
}

// Consume acknowledges only immutable output that crossed file and directory
// sync. Exact replay returns the historical receipt, not renewed remote proof.
// Filesystem IO may block; cancellation forbids acknowledgment but cannot make
// fsync interruptible. The consumer keeps ownership until Close joins it.
func (c *EvidenceConsumer) Consume(ctx context.Context, claimJSON []byte) (*NonMonetaryReceipt, error) {
	ctx, cancel := context.WithTimeout(ctx, c.options.Timeout)
	defer cancel()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.gate:
	}
	defer func() { c.gate <- struct{}{} }()
	if c.closed {
		return nil, errors.New("consumer closed")
	}
	if c.failed != nil {
		return nil, c.failed
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(claimJSON) > c.options.MaxClaimBytes {
		return nil, errors.New("terminal claim input budget")
	}
	var root closingClaim
	if err := decodeConsumerJSON(claimJSON, &root); err != nil {
		return nil, err
	}
	if root.Schema != 1 || root.Complete || !tokenOK(root.JournalIncarnation) || root.Receipt.Claim.Name != "manifest-"+root.JournalIncarnation+"-final" || root.Receipt.Destination != c.reader.Destination() || !validClosingReceipt(root.Receipt, c.reader.Destination(), root.Receipt.Claim.Name) || root.Receipt.VersionID == "latest" {
		return nil, errors.New("invalid explicit root identity")
	}
	canonical, err := json.Marshal(root)
	if err != nil {
		return nil, err
	}
	rootIdentity := closingObjectClaim("root", canonical).SHA256
	key := consumerSlot(root)
	binding := consumerBinding{Schema: "sig.consumer-root-binding.v1", Root: root, Verifier: c.identity}
	bound, err := json.Marshal(binding)
	if err != nil {
		return nil, err
	}
	if prior, readErr := c.read(key + ".binding"); readErr == nil {
		if !bytes.Equal(prior, bound) {
			return nil, errors.New("root/verifier source conflict; explicit migration required")
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		c.failed = readErr
		return nil, readErr
	}
	if err = c.persist(key+".binding", bound); err != nil {
		return nil, err
	}
	if result, readErr := c.retained(key); readErr == nil {
		if result.Verifier != c.identity || !equalClosingJSON(result.RootClaim, root) || result.RootIdentitySHA256 != rootIdentity || result.RootSlotSHA256 != key || result.Complete || result.SessionMapping != "unresolved" {
			return nil, errors.New("immutable consumer replay conflict")
		}
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		return result, nil
	} else if !errors.Is(readErr, os.ErrNotExist) {
		c.failed = readErr
		return nil, readErr
	}
	result, err := verifyConsumerChain(ctx, c.reader, c.options, c.identity, root)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = c.persist(key+".receipt", data); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	outputClaim := consumerOutputClaim{Schema: "sig.consumer-output-claim.v1", SlotSHA256: key, RootIdentitySHA256: rootIdentity, Receipt: closingObjectClaim(key+".receipt", data), Verifier: c.identity}
	claimBytes, err := json.Marshal(outputClaim)
	if err != nil {
		return nil, err
	}
	if err = c.persist(key+".claim", claimBytes); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	// Return a freshly decoded value so callers cannot mutate retained aliases.
	var returned NonMonetaryReceipt
	if err = decodeConsumerJSON(data, &returned); err != nil {
		return nil, err
	}
	return &returned, nil
}
func (c *EvidenceConsumer) Close(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.gate:
	}
	defer func() { c.gate <- struct{}{} }()
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.closed {
		return c.failed
	}
	c.closed = true
	c.failed = errors.Join(c.failed, unlockSpool(c.lock))
	return c.failed
}
