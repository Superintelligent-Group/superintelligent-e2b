//go:build linux

package fc

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// These tests exercise the production API helpers and generated SDK transport,
// not a hand-built request. Pinned Firecracker treats omitted PATCH buckets as
// no update, and explicit size/refill_time zeros as Disabled (SUP925 spec).
func TestRateLimiterPatchGeneratedWire(t *testing.T) {
	disabled := TokenBucketConfig{BucketSize: -1, OneTimeBurst: 987, RefillTimeMs: 654}
	ops := TokenBucketConfig{BucketSize: 13, OneTimeBurst: 7, RefillTimeMs: 500}
	bandwidth := TokenBucketConfig{BucketSize: 8192, RefillTimeMs: 1000}
	zero := `{"size":0,"refill_time":0}`
	opsJSON := `{"size":13,"refill_time":500,"one_time_burst":7}`
	bandwidthJSON := `{"size":8192,"refill_time":1000}`
	cases := []struct {
		name    string
		config  RateLimiterConfig
		ops, bw string
	}{
		{"both_disabled", RateLimiterConfig{Ops: disabled, Bandwidth: disabled}, zero, zero},
		{"ops_enabled_bandwidth_disabled", RateLimiterConfig{Ops: ops, Bandwidth: disabled}, opsJSON, zero},
		{"ops_disabled_bandwidth_enabled", RateLimiterConfig{Ops: disabled, Bandwidth: bandwidth}, zero, bandwidthJSON},
		{"both_enabled", RateLimiterConfig{Ops: ops, Bandwidth: bandwidth}, opsJSON, bandwidthJSON},
		{"explicit_zero", RateLimiterConfig{}, zero, zero},
	}
	for _, test := range cases {
		for _, device := range []string{"network", "drive"} {
			t.Run(test.name+"/"+device, func(t *testing.T) {
				type observed struct {
					method, path string
					body         []byte
					err          error
				}
				requests := make(chan observed, 1)
				socket := filepath.Join(t.TempDir(), "fc.sock")
				listener, err := net.Listen("unix", socket)
				require.NoError(t, err)
				server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, err := io.ReadAll(io.LimitReader(r.Body, 4097))
					_ = r.Body.Close()
					requests <- observed{r.Method, r.URL.Path, body, err}
					w.WriteHeader(http.StatusNoContent)
				})}
				done := make(chan error, 1)
				go func() { done <- server.Serve(listener) }()
				t.Cleanup(func() { require.NoError(t, server.Close()); require.ErrorIs(t, <-done, http.ErrServerClosed) })
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				client := newApiClient(socket)
				before := test.config
				var key, path, idKey string
				if device == "network" {
					key, path, idKey = "tx_rate_limiter", "/network-interfaces/eth0", "iface_id"
					err = client.setTxRateLimit(ctx, "eth0", test.config)
				} else {
					key, path, idKey = "rate_limiter", "/drives/rootfs", "drive_id"
					err = client.setDriveRateLimit(ctx, "rootfs", test.config)
				}
				require.NoError(t, err)
				require.Equal(t, before, test.config, "caller configuration mutated")
				var request observed
				select {
				case request = <-requests:
				case <-ctx.Done():
					t.Fatal("no generated request")
				}
				require.NoError(t, request.err)
				require.LessOrEqual(t, len(request.body), 4096)
				require.Equal(t, http.MethodPatch, request.method)
				require.Equal(t, path, request.path)
				var body map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(request.body, &body))
				require.Len(t, body, 2, "must not alter RX, path_on_host or other device properties")
				require.Contains(t, body, idKey)
				expectedID := `"eth0"`
				if device == "drive" {
					expectedID = `"rootfs"`
				}
				require.JSONEq(t, expectedID, string(body[idKey]))
				var limiter map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(body[key], &limiter))
				require.Len(t, limiter, 2)
				require.JSONEq(t, test.ops, string(limiter["ops"]))
				require.JSONEq(t, test.bw, string(limiter["bandwidth"]))
			})
		}
	}
}

func TestRateLimiterCreationKeepsOmissionSemantics(t *testing.T) {
	disabled := TokenBucketConfig{BucketSize: -1}
	require.Nil(t, buildRateLimiter(RateLimiterConfig{Ops: disabled, Bandwidth: disabled}))
	config := RateLimiterConfig{Ops: disabled, Bandwidth: TokenBucketConfig{BucketSize: 123, RefillTimeMs: 456}}
	limiter := buildRateLimiter(config)
	raw, err := json.Marshal(limiter)
	require.NoError(t, err)
	require.JSONEq(t, `{"bandwidth":{"size":123,"refill_time":456}}`, string(raw))
}
