//go:build linux

package fc_test

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/e2b-dev/infra/packages/orchestrator/pkg/sandbox/fc"
	"github.com/stretchr/testify/require"
)

//go:embed measurement_deferred_rx_test.py
var deferredRXPython string

func deferredRXProgram() string {
	// Reuse the frozen completion-aware trace parser and control-frame decoder.
	return strings.Replace(calibrationPython, "if __name__=='__main__':", "if False:", 1) + "\n" + deferredRXPython
}

func runDeferredRXCalibration(t *testing.T, ctx context.Context, p *fc.Process, base, socketPath, iface string) {
	t.Helper()
	program := deferredRXProgram()
	runCalibrationCapture(t, ctx, p, base, "deferred-rx", program, func(dir string, writeJSON func(string, any), input io.Writer, lines *bufio.Scanner) {
		start, err := fc.CalibrationSample(p, ctx)
		require.NoError(t, err)
		writeJSON("start.json", start)
		body := map[string]any{"iface_id": iface, "rx_rate_limiter": map[string]any{"operations": map[string]uint64{"size": 1, "one_time_burst": 0, "refill_time": 3600000}}}
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		client := &http.Client{Transport: &http.Transport{DialContext: func(c context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(c, "unix", socketPath)
		}}, Timeout: 3 * time.Second}
		defer client.CloseIdleConnections()
		request, err := http.NewRequestWithContext(ctx, http.MethodPatch, "http://localhost/network-interfaces/"+iface, bytes.NewReader(raw))
		require.NoError(t, err)
		request.Header.Set("Content-Type", "application/json")
		response, err := client.Do(request)
		require.NoError(t, err)
		responseBody, err := io.ReadAll(io.LimitReader(response.Body, 4097))
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		writeJSON("limiter.json", map[string]any{"method": request.Method, "path": request.URL.Path, "request": body, "status": response.StatusCode, "response": string(responseBody)})
		require.Equal(t, http.StatusNoContent, response.StatusCode)
		require.Empty(t, responseBody)
		_, err = fmt.Fprintln(input, "traffic")
		require.NoError(t, err)
		require.True(t, lines.Scan(), "RX traffic readiness missing")
		require.Equal(t, "traffic-complete", lines.Text())
		held, err := fc.CalibrationSample(p, ctx)
		require.NoError(t, err)
		writeJSON("held.json", held)
		// Online inspection is only a prerequisite to requesting terminal. The
		// detached complete trace is independently re-parsed after raw reopen.
		output, err := exec.CommandContext(ctx, "python3", "-c", program, "held", dir).CombinedOutput()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "held-verifier.txt"), output, 0600))
		require.NoError(t, err, "%s", output)
		terminal, err := fc.CalibrationTerminal(p, ctx)
		require.NoError(t, err)
		writeJSON("terminal.json", terminal)
		require.NoError(t, p.FinalizeMeasurement(ctx)) // same production replay
		_, err = fmt.Fprintln(input, "post-terminal")
		require.NoError(t, err)
		require.True(t, lines.Scan(), "postterminal observation missing")
		require.Equal(t, "post-terminal-complete", lines.Text())
	})
}

func verifyDeferredRXCalibration(t *testing.T, base string) {
	t.Helper()
	output, err := exec.Command("python3", "-c", deferredRXProgram(), "verify-deferred", base).CombinedOutput()
	require.NoError(t, os.WriteFile(filepath.Join(base, "deferred-rx-verifier.txt"), output, 0600))
	require.NoError(t, err, "%s", output)
}

func TestDeferredRXParserContract(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("Python3 required for calibration parser contract")
	}
	output, err := exec.Command("python3", "-c", deferredRXProgram(), "selftest-deferred").CombinedOutput()
	require.NoError(t, err, "%s", output)
}
