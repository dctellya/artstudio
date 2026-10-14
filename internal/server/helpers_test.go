package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/dctellya/artstudio/internal/config"
	"github.com/dctellya/artstudio/internal/ports"
	"github.com/dctellya/artstudio/internal/server"
)

// fakeLookup is a canned ports.Lookup; hooks override the canned results.
type fakeLookup struct {
	mu         sync.Mutex
	searchRes  ports.SearchResult
	artwork    ports.Artwork
	meta       ports.Meta
	err        error
	onCall     func(ctx context.Context)
	lastCtxIDs []string
}

func (f *fakeLookup) enter(ctx context.Context) {
	f.mu.Lock()
	f.lastCtxIDs = append(f.lastCtxIDs, ports.RequestIDFrom(ctx))
	f.mu.Unlock()
	if f.onCall != nil {
		f.onCall(ctx)
	}
}

func (f *fakeLookup) Search(ctx context.Context, _ ports.SearchQuery) (ports.SearchResult, ports.Meta, error) {
	f.enter(ctx)
	return f.searchRes, f.meta, f.err
}

func (f *fakeLookup) Artwork(ctx context.Context, _ int) (ports.Artwork, ports.Meta, error) {
	f.enter(ctx)
	return f.artwork, f.meta, f.err
}

// retryErr is a ports.RetryAfterError.
type retryErr struct{ after time.Duration }

func (e retryErr) Error() string             { return "retry later" }
func (e retryErr) RetryAfter() time.Duration { return e.after }

// rateLimitedRetry is ports.ErrUpstreamRateLimited that also implements ports.RetryAfterError.
type rateLimitedRetry struct{ after time.Duration }

func (e rateLimitedRetry) Error() string             { return "upstream rate limited, retry later" }
func (e rateLimitedRetry) RetryAfter() time.Duration { return e.after }
func (e rateLimitedRetry) Unwrap() error             { return ports.ErrUpstreamRateLimited }

type fakeChecker struct {
	name string
	res  ports.CheckResult
}

func (c fakeChecker) Name() string                            { return c.name }
func (c fakeChecker) Check(context.Context) ports.CheckResult { return c.res }

func okCheckers() []ports.Checker {
	return []ports.Checker{
		fakeChecker{"config", ports.CheckResult{OK: true}},
		fakeChecker{"db", ports.CheckResult{OK: true}},
		fakeChecker{"cache", ports.CheckResult{OK: true}},
		fakeChecker{"vendor", ports.CheckResult{OK: true, Detail: map[string]any{"state": "closed"}}},
	}
}

func testConfig(t *testing.T) config.Config {
	t.Helper()
	env := map[string]string{"CONTACT_EMAIL": "ops@example.com"}
	cfg, err := config.LoadFrom(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	require.NoError(t, err)
	cfg.ClientRate, cfg.ClientBurst = 1000, 1000 // keep the per-IP limiter out of the way unless a test sets it
	return cfg
}

type env struct {
	srv    *server.Server
	h      http.Handler
	logs   *bytes.Buffer
	lookup *fakeLookup
}

type option func(*config.Config, *server.Deps)

func withCfg(f func(*config.Config)) option { return func(c *config.Config, _ *server.Deps) { f(c) } }
func withCheckers(cs ...ports.Checker) option {
	return func(_ *config.Config, d *server.Deps) { d.Checkers = cs }
}

func newEnv(t *testing.T, opts ...option) *env {
	t.Helper()
	cfg := testConfig(t)
	logs := &bytes.Buffer{}
	lk := &fakeLookup{
		searchRes: ports.SearchResult{
			Items: []ports.Artwork{{ID: 27992, Title: "A Sunday on La Grande Jatte", Artist: "Georges Seurat", Date: "1884", ImageID: "abc"}},
			Total: 1, Limit: 12, Page: 1,
		},
		artwork: ports.Artwork{ID: 27992, Title: "A Sunday on La Grande Jatte", Artist: "Georges Seurat", Date: "1884"},
		meta:    ports.Meta{Cache: ports.CacheMiss, UpstreamLatency: 42 * time.Millisecond, Attempts: 1},
	}
	d := server.Deps{
		Lookup:   lk,
		Checkers: okCheckers(),
		Registry: prometheus.NewRegistry(),
		Logger:   slog.New(slog.NewJSONHandler(logs, nil)),
		Version:  server.VersionInfo{Version: "1.2.3", Commit: "deadbeef", GoVersion: "go1.27.2", BuildDate: "2026-10-14T00:00:00Z"},
	}
	for _, o := range opts {
		o(&cfg, &d)
	}
	srv, err := server.New(cfg, d)
	require.NoError(t, err)
	return &env{srv: srv, h: srv.Handler(), logs: logs, lookup: lk}
}

func (e *env) do(t *testing.T, method, target string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func (e *env) get(t *testing.T, target string) *httptest.ResponseRecorder {
	return e.do(t, http.MethodGet, target, nil)
}

func bodyOf(t *testing.T, rec *httptest.ResponseRecorder) []byte {
	t.Helper()
	b, err := io.ReadAll(rec.Result().Body)
	require.NoError(t, err)
	return b
}

func jsonOf(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(bodyOf(t, rec), &m), "body: %s", rec.Body.String())
	return m
}

// requireProblem asserts an RFC 9457 problem+json response with the given status.
func requireProblem(t *testing.T, rec *httptest.ResponseRecorder, status int) map[string]any {
	t.Helper()
	require.Equal(t, status, rec.Code, "body: %s", rec.Body.String())
	require.True(t, strings.HasPrefix(rec.Header().Get("Content-Type"), "application/problem+json"),
		"content-type %q", rec.Header().Get("Content-Type"))
	p := jsonOf(t, rec)
	for _, k := range []string{"type", "title", "status", "detail", "instance", "request_id"} {
		require.Contains(t, p, k)
	}
	require.EqualValues(t, status, p["status"])
	require.NotEmpty(t, p["request_id"])
	require.Equal(t, rec.Header().Get("X-Request-ID"), p["request_id"])
	return p
}

func serve(e *env, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}
