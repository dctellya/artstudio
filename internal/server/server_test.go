package server_test

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"context"

	"github.com/dctellya/artstudio/internal/config"
	"github.com/dctellya/artstudio/internal/ports"
)

// error -> status map, including Retry-After.
func TestErrorMapping(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		status     int
		retryAfter string // "" = must be absent; "any" = positive integer seconds
	}{
		{"not found", ports.ErrNotFound, 404, ""},
		{"breaker open", ports.ErrBreakerOpen, 503, "any"},
		{"bulkhead full", ports.ErrBulkheadFull, 503, "any"},
		{"limiter rejected", ports.ErrLimiterRejected, 503, "any"},
		{"upstream rate limited", ports.ErrUpstreamRateLimited, 503, "any"},
		{"wrapped breaker open", fmt.Errorf("lookup: %w", ports.ErrBreakerOpen), 503, "any"},
		{"bad upstream", ports.ErrBadUpstream, 502, ""},
		{"budget exceeded", ports.ErrBudgetExceeded, 504, ""},
		{"retry-after honoured", fmt.Errorf("x: %w", errors.Join(ports.ErrBreakerOpen, retryErr{7 * time.Second})), 503, "7"},
		{"retry-after on rate limited", rateLimitedRetry{3 * time.Second}, 503, "3"},
		{"unknown error", errors.New("boom"), 500, ""},
	}
	for _, c := range cases {
		for _, target := range []string{"/art/monet", "/artworks/5"} {
			t.Run(c.name+" "+target, func(t *testing.T) {
				e := newEnv(t)
				e.lookup.err = c.err
				rec := e.get(t, target)
				requireProblem(t, rec, c.status)
				ra := rec.Header().Get("Retry-After")
				switch c.retryAfter {
				case "":
					assert.Empty(t, ra)
				case "any":
					assert.Regexp(t, `^[1-9][0-9]*$`, ra)
				default:
					assert.Equal(t, c.retryAfter, ra)
				}
			})
		}
	}
}

// The vendor payload (carried in error text) never reaches the client.
func TestProblemNeverContainsVendorBody(t *testing.T) {
	const secret = "VENDOR-BODY-SECRET-<html>stack trace</html>" //nolint:gosec // test sentinel, not a credential
	for _, err := range []error{
		fmt.Errorf("%w: %s", ports.ErrBadUpstream, secret),
		fmt.Errorf("%w: %s", ports.ErrNotFound, secret),
		fmt.Errorf("%w: %s", ports.ErrBreakerOpen, secret),
		fmt.Errorf("%w: %s", ports.ErrBudgetExceeded, secret),
		errors.New(secret),
	} {
		e := newEnv(t)
		e.lookup.err = err
		rec := e.get(t, "/art/monet")
		assert.NotContains(t, rec.Body.String(), "VENDOR-BODY-SECRET", "error %v", err)
		assert.NotContains(t, rec.Body.String(), "stack trace")
	}
}

func TestRequestID(t *testing.T) {
	t.Run("inbound id echoed and propagated", func(t *testing.T) {
		e := newEnv(t)
		rec := e.do(t, http.MethodGet, "/artworks/5", map[string]string{"X-Request-ID": "req-abc-123"})
		require.Equal(t, 200, rec.Code)
		assert.Equal(t, "req-abc-123", rec.Header().Get("X-Request-ID"))
		assert.Equal(t, []string{"req-abc-123"}, e.lookup.lastCtxIDs, "ports.RequestIDFrom(ctx) in lookup")
		meta := jsonOf(t, rec)["meta"].(map[string]any)
		assert.Equal(t, "req-abc-123", meta["request_id"])
	})
	t.Run("generated when absent, unique per request", func(t *testing.T) {
		e := newEnv(t)
		a, b := e.get(t, "/health"), e.get(t, "/health")
		ida, idb := a.Header().Get("X-Request-ID"), b.Header().Get("X-Request-ID")
		assert.NotEmpty(t, ida)
		assert.NotEmpty(t, idb)
		assert.NotEqual(t, ida, idb)
	})
	t.Run("hostile inbound id replaced", func(t *testing.T) {
		e := newEnv(t)
		rec := e.do(t, http.MethodGet, "/health", map[string]string{"X-Request-ID": strings.Repeat("x", 500)})
		id := rec.Header().Get("X-Request-ID")
		assert.NotEmpty(t, id)
		assert.LessOrEqual(t, len(id), 128)
	})
}

func TestPerIPRateLimit429(t *testing.T) {
	e := newEnv(t, withCfg(func(c *config.Config) { c.ClientRate, c.ClientBurst = 0.001, 2 }))
	require.Equal(t, 200, e.get(t, "/art/monet").Code)
	require.Equal(t, 200, e.get(t, "/art/monet").Code)
	rec := e.get(t, "/art/monet")
	requireProblem(t, rec, http.StatusTooManyRequests)
	assert.Regexp(t, `^[1-9][0-9]*$`, rec.Header().Get("Retry-After"))

	// another client IP has its own bucket (RemoteAddr only, no proxy header trusted)
	req := newReq("/art/monet", "198.51.100.9:4000")
	req.Header.Set("X-Forwarded-For", "192.0.2.1")
	other := serve(e, req)
	assert.Equal(t, 200, other.Code)
	req = newReq("/art/monet", "192.0.2.1:9999") // same IP, different port: same bucket
	assert.Equal(t, 429, serve(e, req).Code)
}

func TestLoadShedAtMaxInFlight(t *testing.T) {
	const limit = 3
	e := newEnv(t, withCfg(func(c *config.Config) { c.MaxInFlight = limit }))
	entered := make(chan struct{}, limit)
	release := make(chan struct{})
	e.lookup.onCall = func(ctx context.Context) {
		entered <- struct{}{}
		<-release
	}
	var wg sync.WaitGroup
	for i := 0; i < limit; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); e.get(t, "/art/monet") }()
	}
	for i := 0; i < limit; i++ {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("requests did not become in flight")
		}
	}
	rec := e.get(t, "/art/monet")
	requireProblem(t, rec, http.StatusServiceUnavailable)
	assert.Regexp(t, `^[1-9][0-9]*$`, rec.Header().Get("Retry-After"))
	close(release)
	wg.Wait()
	assert.Equal(t, 200, e.get(t, "/art/monet").Code, "capacity is released after the burst")
}

func TestPanicBecomes500Problem(t *testing.T) {
	e := newEnv(t)
	e.lookup.onCall = func(context.Context) { panic("kaboom-internal-detail") }
	rec := e.get(t, "/art/monet")
	requireProblem(t, rec, http.StatusInternalServerError)
	assert.NotContains(t, rec.Body.String(), "kaboom")
	e.lookup.onCall = nil
	assert.Equal(t, 200, e.get(t, "/health").Code, "server keeps serving after a panic")
}

func TestHTTPServerTimeoutsFromConfig(t *testing.T) {
	e := newEnv(t, withCfg(func(c *config.Config) {
		c.HTTPReadHeaderTimeout = 1 * time.Second
		c.HTTPReadTimeout = 2 * time.Second
		c.HTTPWriteTimeout = 3 * time.Second
		c.HTTPIdleTimeout = 4 * time.Second
	}))
	tc := &tls.Config{MinVersion: tls.VersionTLS13}
	hs := e.srv.HTTPServer(":8443", e.h, tc)
	assert.Equal(t, ":8443", hs.Addr)
	assert.Equal(t, 1*time.Second, hs.ReadHeaderTimeout)
	assert.Equal(t, 2*time.Second, hs.ReadTimeout)
	assert.Equal(t, 3*time.Second, hs.WriteTimeout)
	assert.Equal(t, 4*time.Second, hs.IdleTimeout)
	assert.Same(t, tc, hs.TLSConfig)
	assert.NotNil(t, hs.Handler)
}

func TestHealth(t *testing.T) {
	// checks nothing: even with every checker failing it is 200
	e := newEnv(t, withCheckers(
		fakeChecker{"db", ports.CheckResult{OK: false}},
		fakeChecker{"cache", ports.CheckResult{OK: false}},
	))
	rec := e.get(t, "/health")
	require.Equal(t, 200, rec.Code)
	assert.Equal(t, map[string]any{"status": "ok"}, jsonOf(t, rec))
}

func TestReady(t *testing.T) {
	t.Run("all ok", func(t *testing.T) {
		rec := newEnv(t).get(t, "/ready")
		require.Equal(t, 200, rec.Code)
		body := jsonOf(t, rec)
		assert.Equal(t, "ok", body["status"])
		checks := body["checks"].(map[string]any)
		for _, k := range []string{"config", "db", "cache", "vendor"} {
			require.Contains(t, checks, k)
			assert.Equal(t, true, checks[k].(map[string]any)["ok"])
		}
	})
	t.Run("vendor down does not fail readiness", func(t *testing.T) {
		e := newEnv(t, withCheckers(
			fakeChecker{"config", ports.CheckResult{OK: true}},
			fakeChecker{"db", ports.CheckResult{OK: true}},
			fakeChecker{"cache", ports.CheckResult{OK: true}},
			fakeChecker{"vendor", ports.CheckResult{OK: false, Detail: map[string]any{"state": "open", "last_error": "timeout"}}},
		))
		rec := e.get(t, "/ready")
		require.Equal(t, 200, rec.Code)
		body := jsonOf(t, rec)
		assert.Equal(t, "ok", body["status"])
		vendor := body["checks"].(map[string]any)["vendor"].(map[string]any)
		assert.Equal(t, false, vendor["ok"])
		assert.Equal(t, "open", vendor["state"], "Detail fields are inlined into the check object")
	})
	t.Run("db down gives 503", func(t *testing.T) {
		e := newEnv(t, withCheckers(
			fakeChecker{"config", ports.CheckResult{OK: true}},
			fakeChecker{"db", ports.CheckResult{OK: false}},
			fakeChecker{"cache", ports.CheckResult{OK: true}},
			fakeChecker{"vendor", ports.CheckResult{OK: true}},
		))
		rec := e.get(t, "/ready")
		require.Equal(t, 503, rec.Code)
		body := jsonOf(t, rec)
		assert.NotEqual(t, "ok", body["status"])
		assert.Equal(t, false, body["checks"].(map[string]any)["db"].(map[string]any)["ok"])
	})
}

func TestVersion(t *testing.T) {
	rec := newEnv(t).get(t, "/version")
	require.Equal(t, 200, rec.Code)
	assert.Equal(t, map[string]any{
		"version": "1.2.3", "commit": "deadbeef", "go_version": "go1.27.2", "build_date": "2026-10-14T00:00:00Z",
	}, jsonOf(t, rec))
}

func TestDocsAndOpenAPIServed(t *testing.T) {
	e := newEnv(t)
	docs := e.get(t, "/docs")
	require.Equal(t, 200, docs.Code)
	assert.Contains(t, docs.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, strings.ToLower(docs.Body.String()), "swagger")
	assert.NotContains(t, docs.Body.String(), "unpkg.com", "no CDN: assets are embedded")
	assert.NotContains(t, docs.Body.String(), "cdn.")

	spec := e.get(t, "/openapi.yaml")
	require.Equal(t, 200, spec.Code)
	assert.Contains(t, spec.Header().Get("Content-Type"), "yaml")
	assert.Contains(t, spec.Body.String(), "openapi: 3.0.3")
	assert.Contains(t, spec.Body.String(), "/art/{query}")
}

func TestMetaAndCacheHeaders(t *testing.T) {
	e := newEnv(t)
	e.lookup.meta = ports.Meta{Cache: ports.CacheHit, UpstreamLatency: 42 * time.Millisecond, Attempts: 1}
	e.lookup.searchRes = ports.SearchResult{
		Items: []ports.Artwork{
			{ID: 1, Title: "T", Artist: "A", Date: "1900", ImageID: "img", ImageURL: "https://iiif.example/img/full/843,/0/default.jpg"},
			{ID: 2, Title: "No image"},
		},
		Total: 40, Limit: 2, Page: 3,
	}
	rec := e.do(t, http.MethodGet, "/art/monet?limit=2&page=3", map[string]string{"X-Request-ID": "rid-1"})
	require.Equal(t, 200, rec.Code)
	assert.Equal(t, "hit", rec.Header().Get("X-Cache"))
	assert.Equal(t, "upstream;dur=42", rec.Header().Get("Server-Timing"))
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")

	body := jsonOf(t, rec)
	assert.Equal(t, map[string]any{"request_id": "rid-1", "cache": "hit", "upstream_latency_ms": float64(42)}, body["meta"])
	assert.Equal(t, map[string]any{"total": float64(40), "limit": float64(2), "page": float64(3)}, body["pagination"])
	data := body["data"].([]any)
	require.Len(t, data, 2)
	first := data[0].(map[string]any)
	assert.Equal(t, map[string]any{
		"id": float64(1), "title": "T", "artist": "A", "date": "1900", "image_id": "img",
		"image_url": "https://iiif.example/img/full/843,/0/default.jpg",
	}, first)
	assert.Nil(t, data[1].(map[string]any)["image_url"], "image_url is null without image")
	assert.Contains(t, data[1], "image_url")

	t.Run("artwork", func(t *testing.T) {
		e.lookup.meta = ports.Meta{Cache: ports.CacheStale, UpstreamLatency: 0}
		rec := e.get(t, "/artworks/27992")
		require.Equal(t, 200, rec.Code)
		assert.Equal(t, "stale", rec.Header().Get("X-Cache"))
		body := jsonOf(t, rec)
		assert.Equal(t, float64(27992), body["data"].(map[string]any)["id"])
		assert.Equal(t, "stale", body["meta"].(map[string]any)["cache"])
		assert.NotContains(t, body, "pagination")
	})
}

func TestAccessLogLine(t *testing.T) {
	e := newEnv(t)
	rec := e.do(t, http.MethodGet, "/artworks/27992", map[string]string{"X-Request-ID": "log-rid-9"})
	require.Equal(t, 200, rec.Code)

	var lines []map[string]any
	sc := bufio.NewScanner(e.logs)
	for sc.Scan() {
		var m map[string]any
		require.NoError(t, json.Unmarshal(sc.Bytes(), &m))
		if m["msg"] == "request" {
			lines = append(lines, m)
		}
	}
	require.Len(t, lines, 1, "exactly one access-log line per request")
	l := lines[0]
	assert.Equal(t, rec.Header().Get("X-Request-ID"), l["request_id"])
	assert.Equal(t, "log-rid-9", l["request_id"])
	assert.Contains(t, l, "trace_id")
	assert.EqualValues(t, 200, l["status"])
	assert.Contains(t, l, "duration_ms")
}

func newReq(target, remote string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, target, nil)
	r.RemoteAddr = remote
	return r
}
