package terminalreceipt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// TTL is long enough for the API close request and delayed reconciliation,
// while keeping the Redis side-channel bounded. The durable event remains the
// long-term record.
const TTL = 24 * time.Hour

func Key(sandboxID string) string { return "e2b:terminal-receipt:" + sandboxID }

type Envelope struct {
	Receipt map[string]any `json:"terminal_receipt"`
	SHA256  string         `json:"terminal_receipt_sha256"`
}

// Store publishes the provider receipt synchronously before gRPC Delete
// returns. This closes the transport gap while the event stream remains the
// durable audit record.
func Store(ctx context.Context, client redis.UniversalClient, sandboxID string, receipt map[string]any, sha256 string) error {
	if client == nil {
		return errors.New("redis client unavailable")
	}
	if sandboxID == "" || sha256 == "" {
		return errors.New("terminal receipt identity or digest missing")
	}
	payload, err := json.Marshal(Envelope{Receipt: receipt, SHA256: sha256})
	if err != nil {
		return fmt.Errorf("marshal terminal receipt: %w", err)
	}
	if err := client.Set(ctx, Key(sandboxID), payload, TTL).Err(); err != nil {
		return fmt.Errorf("persist terminal receipt: %w", err)
	}
	return nil
}

func Load(ctx context.Context, client redis.UniversalClient, sandboxID string) (Envelope, error) {
	if client == nil {
		return Envelope{}, errors.New("redis client unavailable")
	}
	data, err := client.Get(ctx, Key(sandboxID)).Bytes()
	if err != nil {
		return Envelope{}, err
	}
	var envelope Envelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return Envelope{}, fmt.Errorf("decode terminal receipt: %w", err)
	}
	if envelope.Receipt == nil || envelope.SHA256 == "" {
		return Envelope{}, errors.New("incomplete terminal receipt")
	}
	return envelope, nil
}
