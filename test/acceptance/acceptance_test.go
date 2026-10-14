//go:build acceptance

// Package acceptance runs the SPEC §6 acceptance checks against a running
// stack (make up). Env: API_URL (default https://localhost:8443), CA_FILE
// (default deploy/certs/ca.pem relative to the repo root), METRICS_URL
// (default https://localhost:9090).
package acceptance

import (
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const timeout = 15 * time.Second

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func caPath() string {
	if v := os.Getenv("CA_FILE"); v != "" {
		return v
	}
	return filepath.Join("..", "..", "deploy", "certs", "ca.pem")
}

func client(t *testing.T) *http.Client {
	t.Helper()
	pem, err := os.ReadFile(caPath())
	require.NoError(t, err, "CA file")
	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(pem))
	return &http.Client{
		Timeout:       timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport:     &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS13}},
	}
}

func baseURL() string    { return strings.TrimRight(env("API_URL", "https://localhost:8443"), "/") }
func metricsURL() string { return strings.TrimRight(env("METRICS_URL", "https://localhost:9090"), "/") }

func get(t *testing.T, url string, hdr map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	require.NoError(t, err)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := client(t).Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp, body
}

func TestHealth(t *testing.T) {
	resp, body := get(t, baseURL()+"/health", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.JSONEq(t, `{"status":"ok"}`, string(body))
	require.NotEmpty(t, resp.Header.Get("Strict-Transport-Security"))
}

func TestTLSOnly(t *testing.T) {
	addr := strings.TrimPrefix(baseURL(), "https://")
	plain := &http.Client{Timeout: timeout}
	resp, err := plain.Get("http://" + addr + "/health")
	if err == nil {
		_ = resp.Body.Close()
		if resp.StatusCode < http.StatusBadRequest {
			t.Fatalf("plaintext HTTP succeeded with %d", resp.StatusCode)
		}
	}
	pem, err := os.ReadFile(caPath())
	require.NoError(t, err)
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(pem)
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: timeout}, "tcp", addr,
		&tls.Config{RootCAs: pool, ServerName: "localhost", MaxVersion: tls.VersionTLS12}) //nolint:gosec // probing that TLS 1.2 is refused
	if err == nil {
		_ = conn.Close()
		t.Fatal("TLS 1.2 handshake succeeded")
	}
}

func randWord() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func isProblem(resp *http.Response) bool {
	return strings.HasPrefix(resp.Header.Get("Content-Type"), "application/problem+json")
}

// compose runs docker compose from the repo root.
func compose(args ...string) (string, error) {
	cmd := exec.Command("docker", append([]string{"compose"}, args...)...)
	cmd.Dir = filepath.Join("..", "..")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestSearchMissThenHit(t *testing.T) {
	q := baseURL() + "/art/monet" + randWord() + "?limit=3&page=1"
	r1, b1 := get(t, q, nil)
	require.Equal(t, http.StatusOK, r1.StatusCode, string(b1))
	require.Equal(t, "miss", r1.Header.Get("X-Cache"))
	var out struct {
		Data       []map[string]any `json:"data"`
		Pagination map[string]any   `json:"pagination"`
		Meta       map[string]any   `json:"meta"`
	}
	require.NoError(t, json.Unmarshal(b1, &out))
	require.NotNil(t, out.Pagination)
	require.NotEmpty(t, out.Meta["request_id"])
	r2, _ := get(t, q, nil)
	require.Equal(t, http.StatusOK, r2.StatusCode)
	require.Equal(t, "hit", r2.Header.Get("X-Cache"))
}

func TestArtworkWithImageURL(t *testing.T) {
	resp, body := get(t, baseURL()+"/artworks/27992", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var out struct {
		Data struct {
			ID       int     `json:"id"`
			ImageURL *string `json:"image_url"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	require.Equal(t, 27992, out.Data.ID)
	require.NotNil(t, out.Data.ImageURL)
	require.True(t, strings.HasSuffix(*out.Data.ImageURL, "/full/843,/0/default.jpg"), *out.Data.ImageURL)
}

func TestNotFoundProblemAndNegativeCache(t *testing.T) {
	url := fmt.Sprintf("%s/artworks/%d", baseURL(), 900000000+int(time.Now().UnixNano()%99999999))
	const rid = "acceptance-404-1"
	resp, body := get(t, url, map[string]string{"X-Request-ID": rid})
	require.Equal(t, http.StatusNotFound, resp.StatusCode, string(body))
	require.True(t, isProblem(resp), resp.Header.Get("Content-Type"))
	var p map[string]any
	require.NoError(t, json.Unmarshal(body, &p))
	require.EqualValues(t, 404, p["status"])
	require.Equal(t, resp.Header.Get("X-Request-ID"), p["request_id"])
	require.Equal(t, rid, p["request_id"])
	resp2, _ := get(t, url, nil)
	require.Equal(t, http.StatusNotFound, resp2.StatusCode)
	require.Equal(t, "negative", resp2.Header.Get("X-Cache"))
}

func TestLimitOutOfRange(t *testing.T) {
	for _, path := range []string{"/art/monet?limit=51", "/art/monet?limit=50&page=21"} {
		resp, body := get(t, baseURL()+path, nil)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode, path+" "+string(body))
		require.True(t, isProblem(resp), path+" "+resp.Header.Get("Content-Type"))
	}
}

func TestRequestIDEcho(t *testing.T) {
	const id = "acceptance-req-id-1"
	resp, _ := get(t, baseURL()+"/health", map[string]string{"X-Request-ID": id})
	require.Equal(t, id, resp.Header.Get("X-Request-ID"))
	resp2, _ := get(t, baseURL()+"/health", nil)
	require.NotEmpty(t, resp2.Header.Get("X-Request-ID"), "generated when absent")
}

func TestReadyVendorNeverBlocks(t *testing.T) {
	resp, body := get(t, baseURL()+"/ready", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var out struct {
		Checks map[string]map[string]any `json:"checks"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	for _, name := range []string{"config", "vendor", "db", "cache"} {
		require.Contains(t, out.Checks, name)
	}
}

// Metric families frozen in SPEC §5 (namespace artstudio_).
var families = []string{
	"http_requests_total", "http_request_duration_seconds", "http_in_flight", "http_shed_total", "http_rate_limited_total",
	"upstream_requests_total", "upstream_request_duration_seconds", "upstream_retries_total", "upstream_breaker_state",
	"upstream_breaker_transitions_total", "upstream_limiter_wait_seconds", "upstream_limiter_rejected_total",
	"upstream_bulkhead_in_use", "upstream_bulkhead_rejected_total", "upstream_probe_up", "upstream_probe_latency_seconds",
	"cache_requests_total", "cache_entries", "cache_evictions_total", "cache_singleflight_dedupes_total",
	"audit_queue_depth", "audit_writes_total", "audit_dropped_total", "audit_write_duration_seconds",
	"db_pool_total_conns", "db_pool_idle_conns", "db_pool_acquired_conns", "db_pool_max_conns",
	"db_pool_acquire_total", "db_pool_acquire_duration_seconds_total", "build_info",
}

func TestMetricsOverTLS(t *testing.T) {
	resp, body := get(t, metricsURL()+"/metrics", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var missing []string
	for _, f := range families {
		if !strings.Contains(string(body), "artstudio_"+f) {
			missing = append(missing, "artstudio_"+f)
		}
	}
	require.Empty(t, missing, "metric families missing")
}

func TestAuditRowsInPostgres(t *testing.T) {
	// Make sure at least one vendor call exists, then give the batch writer time to flush (AUDIT_FLUSH 1s).
	get(t, baseURL()+"/art/audit"+randWord()+"?limit=1", nil)
	var out string
	require.Eventually(t, func() bool {
		o, err := compose("exec", "-T", "postgres", "psql", "-U", "artstudio", "-d", "artstudio",
			"-tAc", "select count(*) from vendor_calls")
		out = strings.TrimSpace(o)
		n, convErr := strconv.Atoi(out)
		return err == nil && convErr == nil && n > 0
	}, 20*time.Second, time.Second, "vendor_calls count: %s", out)
}

func TestHealthcheckSubcommand(t *testing.T) {
	out, err := compose("exec", "-T", "api", "/artstudio", "healthcheck")
	require.NoError(t, err, out)
}

// TestPerIPRateLimit runs late: it drains this client's token bucket.
func TestPerIPRateLimit(t *testing.T) {
	const burst = 120 // above CLIENT_BURST default (20), fired concurrently so refill (10/s) cannot keep up
	var (
		wg         sync.WaitGroup
		mu         sync.Mutex
		limited    *http.Response
		retryAfter string
	)
	c := client(t)
	for i := 0; i < burst; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := c.Get(baseURL() + "/health")
			if err != nil {
				return
			}
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, resp.Body)
			if resp.StatusCode == http.StatusTooManyRequests {
				mu.Lock()
				limited, retryAfter = resp, resp.Header.Get("Retry-After")
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	require.NotNil(t, limited, "expected a 429 within a burst of %d", burst)
	require.NotEmpty(t, retryAfter)
	require.True(t, isProblem(limited), limited.Header.Get("Content-Type"))
	time.Sleep(3 * time.Second) // let the bucket refill for later tests
}

func breakerState(t *testing.T) string {
	t.Helper()
	_, body := get(t, baseURL()+"/ready", nil)
	var out struct {
		Checks map[string]map[string]any `json:"checks"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	s, _ := out.Checks["vendor"]["breaker"].(string)
	return s
}

// TestChaosBreakerOpenThenStale needs `make chaos-up` with CACHE_TTL_SEARCH=1s (so the warmed entry
// expires into the stale window within the test).
func TestChaosBreakerOpenThenStale(t *testing.T) {
	if os.Getenv("ACCEPTANCE_CHAOS") != "1" {
		t.Skip("set ACCEPTANCE_CHAOS=1 with the stack started by make chaos-up")
	}
	_, _ = compose("exec", "-T", "stub", "/stubserver", "ctl", "clear")
	t.Cleanup(func() { _, _ = compose("exec", "-T", "stub", "/stubserver", "ctl", "clear") })

	warm := baseURL() + "/art/chaoswarm" + randWord() + "?limit=2&page=1"
	r, b := get(t, warm, nil)
	require.Equal(t, http.StatusOK, r.StatusCode, string(b))
	require.Equal(t, "miss", r.Header.Get("X-Cache"))
	time.Sleep(2500 * time.Millisecond) // CACHE_TTL_SEARCH=1s: now expired, still within CACHE_TTL_STALE

	out, err := compose("exec", "-T", "stub", "/stubserver", "ctl", "503")
	require.NoError(t, err, out)

	// Distinct uncached queries each fail upstream; 5 consecutive failures open the breaker.
	for i := 0; i < 20 && breakerState(t) != "open"; i++ {
		get(t, baseURL()+"/art/chaosfail"+randWord()+"?limit=1", nil)
		time.Sleep(300 * time.Millisecond)
	}
	require.Equal(t, "open", breakerState(t))

	rs, bs := get(t, warm, nil)
	require.Equal(t, http.StatusOK, rs.StatusCode, string(bs))
	require.Equal(t, "stale", rs.Header.Get("X-Cache"))

	rn, bn := get(t, baseURL()+"/art/chaosnever"+randWord()+"?limit=1", nil)
	require.Equal(t, http.StatusServiceUnavailable, rn.StatusCode, string(bn))
	require.NotEmpty(t, rn.Header.Get("Retry-After"))
	require.True(t, isProblem(rn))

	out, err = compose("exec", "-T", "stub", "/stubserver", "ctl", "clear")
	require.NoError(t, err, out)
}

// TestGracefulStopExitsZero stops the api container, so it is opt-in. The api must log
// msg="shutdown" or msg="draining" when it starts draining and msg="audit flushed" after the final flush.
func TestGracefulStopExitsZero(t *testing.T) {
	if os.Getenv("ACCEPTANCE_STOP") != "1" {
		t.Skip("set ACCEPTANCE_STOP=1 (stops and restarts the api container)")
	}
	t.Cleanup(func() { _, _ = compose("start", "api") })
	cid, err := compose("ps", "-q", "api")
	require.NoError(t, err, cid)
	cid = strings.TrimSpace(cid)
	require.NotEmpty(t, cid, "api container not running")

	out, err := compose("stop", "api")
	require.NoError(t, err, out)
	code, err := exec.Command("docker", "inspect", "-f", "{{.State.ExitCode}}", cid).CombinedOutput()
	require.NoError(t, err, string(code))
	require.Equal(t, "0", strings.TrimSpace(string(code)))

	logs, err := compose("logs", "--no-log-prefix", "api")
	require.NoError(t, err, logs)
	require.True(t, strings.Contains(logs, `"msg":"shutdown"`) || strings.Contains(logs, `"msg":"draining"`), "no drain line in logs")
	require.Contains(t, logs, `"msg":"audit flushed"`)

	out, err = compose("start", "api")
	require.NoError(t, err, out)
	require.Eventually(t, func() bool {
		st, _ := exec.Command("docker", "inspect", "-f", "{{.State.Health.Status}}", cid).CombinedOutput()
		return strings.TrimSpace(string(st)) == "healthy"
	}, 60*time.Second, time.Second, "api did not become healthy")
}
