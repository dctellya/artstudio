//go:build acceptance

// Package acceptance runs the SPEC §6 acceptance checks against a running
// stack (make up). Env: API_URL (default https://localhost:8443), CA_FILE
// (default deploy/certs/ca.pem relative to the repo root), METRICS_URL
// (default https://localhost:9090).
package acceptance

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func notYet(t *testing.T, what string) {
	t.Helper()
	t.Fatalf("not yet: %s", what)
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

func TestSearchMissThenHit(t *testing.T) {
	resp, body := get(t, baseURL()+"/art/monet?limit=3", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var out struct {
		Data       []map[string]any `json:"data"`
		Pagination map[string]any   `json:"pagination"`
		Meta       map[string]any   `json:"meta"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	require.Len(t, out.Data, 3)
	require.NotNil(t, out.Pagination)
	require.NotEmpty(t, out.Meta["request_id"])
	// The skeleton has no cache: the first call is "bypass". Real behaviour: miss, then hit.
	first := resp.Header.Get("X-Cache")
	if first == "bypass" {
		// accepted for the skeleton search check; the miss->hit part is red below
		t.Log("skeleton: X-Cache=bypass")
	}
}

func TestSearchMissThenHitCached(t *testing.T) {
	q := baseURL() + "/art/monet?limit=2&page=1"
	r1, _ := get(t, q, nil)
	require.Equal(t, http.StatusOK, r1.StatusCode)
	r2, _ := get(t, q, nil)
	if r2.Header.Get("X-Cache") != "hit" {
		notYet(t, "cache: second identical search must be X-Cache: hit (got "+r2.Header.Get("X-Cache")+")")
	}
}

func TestArtworkWithImageURL(t *testing.T) {
	resp, _ := get(t, baseURL()+"/artworks/27992", nil)
	if resp.StatusCode != http.StatusOK {
		notYet(t, "GET /artworks/{id} (team B routes), got status "+resp.Status)
	}
}

func TestNotFoundProblemAndNegativeCache(t *testing.T) {
	resp, _ := get(t, baseURL()+"/artworks/999999999", nil)
	if resp.StatusCode != http.StatusNotFound || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/problem+json") {
		notYet(t, "404 application/problem+json and negative cache for unknown artwork, got "+resp.Status)
	}
}

func TestLimitOutOfRange(t *testing.T) {
	resp, _ := get(t, baseURL()+"/art/monet?limit=51", nil)
	if resp.StatusCode != http.StatusBadRequest || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/problem+json") {
		notYet(t, "request validation: limit=51 must be 400 problem+json, got "+resp.Status)
	}
}

func TestRequestIDEcho(t *testing.T) {
	const id = "acceptance-req-id-1"
	resp, _ := get(t, baseURL()+"/health", map[string]string{"X-Request-ID": id})
	require.Equal(t, id, resp.Header.Get("X-Request-ID"))
	resp2, _ := get(t, baseURL()+"/health", nil)
	require.NotEmpty(t, resp2.Header.Get("X-Request-ID"), "generated when absent")
}

func TestPerIPRateLimit(t *testing.T) {
	const burst = 60 // above CLIENT_BURST default (20)
	for i := 0; i < burst; i++ {
		resp, _ := get(t, baseURL()+"/health", nil)
		if resp.StatusCode == http.StatusTooManyRequests {
			require.NotEmpty(t, resp.Header.Get("Retry-After"))
			return
		}
	}
	notYet(t, "per-IP rate limit middleware: expected 429 + Retry-After within a burst of 60")
}

func TestReadyVendorNeverBlocks(t *testing.T) {
	resp, _ := get(t, baseURL()+"/ready", nil)
	if resp.StatusCode != http.StatusOK {
		notYet(t, "/ready with config, db, cache, vendor checks (team B), got "+resp.Status)
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
		// Vector families with no observations yet only appear after traffic; HELP lines are always emitted once registered.
		if !strings.Contains(string(body), "artstudio_"+f) {
			missing = append(missing, "artstudio_"+f)
		}
	}
	if len(missing) > 0 {
		notYet(t, "metric families missing: "+strings.Join(missing, ", "))
	}
}

func TestAuditRowsInPostgres(t *testing.T) {
	out, err := exec.Command("docker", "compose", "exec", "-T", "postgres", "psql", "-U", "artstudio", "-d", "artstudio",
		"-tAc", "select count(*) from vendor_calls").CombinedOutput()
	if err != nil {
		notYet(t, "audit writer and vendor_calls table (team D): "+strings.TrimSpace(string(out)))
	}
	require.NotEqual(t, "0", strings.TrimSpace(string(out)))
}

func TestHealthcheckSubcommand(t *testing.T) {
	out, err := exec.Command("docker", "compose", "exec", "-T", "api", "/artstudio", "healthcheck").CombinedOutput()
	require.NoError(t, err, string(out))
}

func TestChaosBreakerOpenThenStale(t *testing.T) {
	notYet(t, "chaos: stub server (team D), breaker + stale cache (team A), wiring (C)")
}

func TestGracefulStopExitsZero(t *testing.T) {
	notYet(t, "graceful stop check on a throwaway container (SIGTERM -> exit 0); run after wiring")
}
