//go:build linux

package fc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/e2b-dev/infra/packages/orchestrator/pkg/sandbox/networkusage"
)

// This extension deliberately does not cast unknown actions into the legacy
// generated client enum. Its schema is pinned to the reviewed producer binary.
type measurementClient struct{ http *http.Client }
type measurementConfig struct {
	Path        string `json:"metrics_path"`
	Incarnation string `json:"measurement_incarnation"`
}
type measurementAction struct {
	Action      string                       `json:"action_type"`
	Measurement networkusage.ProducerRequest `json:"measurement"`
}
type measurementHTTPError struct {
	status int
	body   string
}

func (e *measurementHTTPError) Error() string {
	return fmt.Sprintf("measurement API status %d: %s", e.status, e.body)
}

func newMeasurementClient(socket string) *measurementClient {
	return &measurementClient{http: &http.Client{
		Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "unix", socket)
		}, DisableKeepAlives: true},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}}
}
func (c *measurementClient) put(ctx context.Context, path string, value any, status int) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, "http://firecracker"+path, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, networkusage.MaxProducerReceiptBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > networkusage.MaxProducerReceiptBytes {
		return nil, fmt.Errorf("measurement response exceeds bound")
	}
	if res.StatusCode != status {
		return nil, &measurementHTTPError{res.StatusCode, string(body)}
	}
	return body, nil
}
func (c *measurementClient) configure(ctx context.Context, path, incarnation string) error {
	_, err := c.put(ctx, "/metrics", measurementConfig{path, incarnation}, http.StatusNoContent)
	return err
}
func (c *measurementClient) request(ctx context.Context, request networkusage.ProducerRequest, terminal bool) ([]byte, error) {
	action := "FlushMeasurement"
	if terminal {
		action = "FinalizeMeasurement"
	}
	return c.put(ctx, "/actions", measurementAction{action, request}, http.StatusOK)
}
