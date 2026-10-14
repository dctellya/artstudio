package lookup_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dctellya/artstudio/internal/cache"
	"github.com/dctellya/artstudio/internal/config"
	"github.com/dctellya/artstudio/internal/lookup"
	"github.com/dctellya/artstudio/internal/ports"
)

type fakeVendor struct {
	mu        sync.Mutex
	searchFn  func() (ports.SearchResult, ports.Call, error)
	artworkFn func() (ports.Artwork, ports.Call, error)
	searchN   int
	artworkN  int
}

func (f *fakeVendor) Search(context.Context, ports.SearchQuery) (ports.SearchResult, ports.Call, error) {
	f.mu.Lock()
	f.searchN++
	f.mu.Unlock()
	return f.searchFn()
}

func (f *fakeVendor) Artwork(context.Context, int) (ports.Artwork, ports.Call, error) {
	f.mu.Lock()
	f.artworkN++
	f.mu.Unlock()
	return f.artworkFn()
}
func (f *fakeVendor) Probe(context.Context) (ports.Call, error) { return ports.Call{}, nil }
func (f *fakeVendor) BreakerState() string                      { return "closed" }
func (f *fakeVendor) calls() int                                { f.mu.Lock(); defer f.mu.Unlock(); return f.searchN + f.artworkN }

type fakeAuditor struct {
	mu   sync.Mutex
	recs []ports.VendorCall
}

func (a *fakeAuditor) Record(c ports.VendorCall) {
	a.mu.Lock()
	a.recs = append(a.recs, c)
	a.mu.Unlock()
}
func (a *fakeAuditor) Close(context.Context) error { return nil }
func (a *fakeAuditor) all() []ports.VendorCall {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]ports.VendorCall(nil), a.recs...)
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

var (
	art     = ports.Artwork{ID: 27992, Title: "A Sunday on La Grande Jatte", Artist: "Georges Seurat", Date: "1884-86", ImageID: "img", ImageURL: "https://x/img"}
	sq      = ports.SearchQuery{Q: "  Seurat ", Limit: 10, Page: 1}
	sres    = ports.SearchResult{Items: []ports.Artwork{art}, Total: 1, Limit: 10, Page: 1}
	goodCal = ports.Call{Operation: "search", Status: 200, Attempts: 2, Latency: 30 * time.Millisecond, Breaker: "closed", Body: []byte(`{"raw":1}`)}
)

type rig struct {
	svc *lookup.Service
	v   *fakeVendor
	a   *fakeAuditor
	c   *cache.LRU
	clk *clock
	ctx context.Context
}

// newRig: TTL search 10s, artwork 20s, stale window 60s, negative 5s.
func newRig(t *testing.T) *rig {
	t.Helper()
	env := map[string]string{"CONTACT_EMAIL": "ops@example.com"}
	cfg, err := config.LoadFrom(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	require.NoError(t, err)
	cfg.CacheTTLSearch, cfg.CacheTTLArtwork = 10*time.Second, 20*time.Second
	cfg.CacheTTLStale, cfg.CacheTTLNegative = 60*time.Second, 5*time.Second
	clk := &clock{t: time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)}
	c := cache.New(cfg, prometheus.NewRegistry(), cache.WithClock(clk.Now))
	v := &fakeVendor{
		searchFn: func() (ports.SearchResult, ports.Call, error) { return sres, goodCal, nil },
		artworkFn: func() (ports.Artwork, ports.Call, error) {
			cl := goodCal
			cl.Operation = "artwork"
			return art, cl, nil
		},
	}
	a := &fakeAuditor{}
	return &rig{
		svc: lookup.New(v, c, a, cfg, prometheus.NewRegistry()), v: v, a: a, c: c, clk: clk,
		ctx: ports.WithRequestID(context.Background(), "req-1"),
	}
}

func TestServiceImplementsLookup(t *testing.T) {
	var _ ports.Lookup = (*lookup.Service)(nil)
}

func TestSearchMissThenHit(t *testing.T) {
	r := newRig(t)
	res, meta, err := r.svc.Search(r.ctx, sq)
	require.NoError(t, err)
	assert.Equal(t, sres, res)
	assert.Equal(t, ports.CacheMiss, meta.Cache)
	assert.Equal(t, "req-1", meta.RequestID)
	assert.Equal(t, 2, meta.Attempts)
	assert.Equal(t, 30*time.Millisecond, meta.UpstreamLatency)
	_, st := r.c.Get(r.ctx, cache.SearchKey(sq))
	assert.Equal(t, ports.CacheHit, st, "result stored under cache.SearchKey")

	res, meta, err = r.svc.Search(r.ctx, sq)
	require.NoError(t, err)
	assert.Equal(t, sres, res)
	assert.Equal(t, ports.CacheHit, meta.Cache)
	assert.Equal(t, "req-1", meta.RequestID)
	assert.Equal(t, 1, r.v.calls(), "hit does not reach the vendor")
}

func TestArtworkMissThenHit(t *testing.T) {
	r := newRig(t)
	got, meta, err := r.svc.Artwork(r.ctx, art.ID)
	require.NoError(t, err)
	assert.Equal(t, art, got)
	assert.Equal(t, ports.CacheMiss, meta.Cache)
	_, st := r.c.Get(r.ctx, cache.ArtworkKey(art.ID))
	assert.Equal(t, ports.CacheHit, st)

	r.clk.Advance(15 * time.Second) // artwork TTL is 20s, search TTL would have expired
	got, meta, err = r.svc.Artwork(r.ctx, art.ID)
	require.NoError(t, err)
	assert.Equal(t, art, got)
	assert.Equal(t, ports.CacheHit, meta.Cache)
	assert.Equal(t, 1, r.v.calls())
}

func TestExpiredEntryWithHealthyVendorIsRefreshed(t *testing.T) {
	r := newRig(t)
	_, _, err := r.svc.Search(r.ctx, sq)
	require.NoError(t, err)
	r.clk.Advance(11 * time.Second)
	_, meta, err := r.svc.Search(r.ctx, sq)
	require.NoError(t, err)
	assert.Equal(t, ports.CacheMiss, meta.Cache, "fresh vendor answer beats stale")
	assert.Equal(t, 2, r.v.calls())
}

func TestStaleServedOnVendorError(t *testing.T) {
	r := newRig(t)
	_, _, err := r.svc.Search(r.ctx, sq)
	require.NoError(t, err)
	r.clk.Advance(11 * time.Second)
	r.v.searchFn = func() (ports.SearchResult, ports.Call, error) {
		return ports.SearchResult{}, ports.Call{Operation: "search", Status: 503, Attempts: 3, Breaker: "closed", Err: ports.ErrBadUpstream}, fmt.Errorf("vendor: %w", ports.ErrBadUpstream)
	}
	res, meta, err := r.svc.Search(r.ctx, sq)
	require.NoError(t, err, "stale is a success")
	assert.Equal(t, sres, res)
	assert.Equal(t, ports.CacheStale, meta.Cache)
	assert.Equal(t, 3, meta.Attempts)

	recs := r.a.all()
	require.Len(t, recs, 2, "the failed vendor call is audited too")
	assert.Equal(t, 503, recs[1].UpstreamStatus)
	assert.NotEmpty(t, recs[1].Error)
}

func TestStaleServedOnBreakerOpen(t *testing.T) {
	r := newRig(t)
	_, _, err := r.svc.Artwork(r.ctx, art.ID)
	require.NoError(t, err)
	r.clk.Advance(21 * time.Second)
	r.v.artworkFn = func() (ports.Artwork, ports.Call, error) {
		return ports.Artwork{}, ports.Call{Operation: "artwork", Breaker: "open", Err: ports.ErrBreakerOpen}, ports.ErrBreakerOpen
	}
	got, meta, err := r.svc.Artwork(r.ctx, art.ID)
	require.NoError(t, err)
	assert.Equal(t, art, got)
	assert.Equal(t, ports.CacheStale, meta.Cache)
}

func TestErrorWhenNothingStale(t *testing.T) {
	cases := map[string]error{
		"breaker open": ports.ErrBreakerOpen,
		"bad upstream": ports.ErrBadUpstream,
		"budget":       ports.ErrBudgetExceeded,
	}
	for name, verr := range cases {
		t.Run(name+" cold cache", func(t *testing.T) {
			r := newRig(t)
			r.v.searchFn = func() (ports.SearchResult, ports.Call, error) {
				return ports.SearchResult{}, ports.Call{Operation: "search", Err: verr}, fmt.Errorf("wrapped: %w", verr)
			}
			_, _, err := r.svc.Search(r.ctx, sq)
			require.ErrorIs(t, err, verr)
		})
	}
	t.Run("entry beyond stale window", func(t *testing.T) {
		r := newRig(t)
		_, _, err := r.svc.Search(r.ctx, sq)
		require.NoError(t, err)
		r.clk.Advance(10*time.Second + 61*time.Second)
		r.v.searchFn = func() (ports.SearchResult, ports.Call, error) {
			return ports.SearchResult{}, ports.Call{Err: ports.ErrBreakerOpen}, ports.ErrBreakerOpen
		}
		_, _, err = r.svc.Search(r.ctx, sq)
		require.ErrorIs(t, err, ports.ErrBreakerOpen)
	})
}

func TestNegativeCaching(t *testing.T) {
	r := newRig(t)
	r.v.artworkFn = func() (ports.Artwork, ports.Call, error) {
		return ports.Artwork{}, ports.Call{Operation: "artwork", Status: 404, Attempts: 1, Err: ports.ErrNotFound}, ports.ErrNotFound
	}
	_, _, err := r.svc.Artwork(r.ctx, 999)
	require.ErrorIs(t, err, ports.ErrNotFound)
	require.Equal(t, 1, r.v.calls())

	_, meta, err := r.svc.Artwork(r.ctx, 999)
	require.ErrorIs(t, err, ports.ErrNotFound)
	assert.Equal(t, ports.CacheNegative, meta.Cache)
	assert.Equal(t, 1, r.v.calls(), "negative entry short-circuits the vendor")
	assert.Len(t, r.a.all(), 1, "no audit record for a negative hit")

	r.clk.Advance(6 * time.Second) // negative TTL 5s
	_, _, err = r.svc.Artwork(r.ctx, 999)
	require.ErrorIs(t, err, ports.ErrNotFound)
	assert.Equal(t, 2, r.v.calls(), "vendor asked again once the negative entry expired")
}

func TestOneAuditRecordPerVendorCallNoneOnHit(t *testing.T) {
	r := newRig(t)
	_, _, err := r.svc.Search(r.ctx, sq)
	require.NoError(t, err)
	recs := r.a.all()
	require.Len(t, recs, 1)
	rec := recs[0]
	assert.Equal(t, "search", rec.Operation)
	assert.Equal(t, "req-1", rec.RequestID)
	assert.Equal(t, cache.SearchKey(sq), rec.CacheKey)
	assert.Equal(t, ports.CacheMiss, rec.CacheStatus)
	assert.Equal(t, 200, rec.UpstreamStatus)
	assert.Equal(t, 2, rec.Attempts)
	assert.EqualValues(t, 30, rec.LatencyMS)
	assert.Equal(t, "closed", rec.Breaker)
	assert.Empty(t, rec.Error)
	assert.False(t, rec.TS.IsZero())
	assert.True(t, json.Valid(rec.Response) || len(rec.Response) > 0, "response body recorded")
	assert.Equal(t, goodCal.Body, rec.Response)

	_, meta, err := r.svc.Search(r.ctx, sq)
	require.NoError(t, err)
	require.Equal(t, ports.CacheHit, meta.Cache)
	assert.Len(t, r.a.all(), 1, "cache hit writes no audit record")

	_, _, err = r.svc.Artwork(r.ctx, art.ID)
	require.NoError(t, err)
	recs = r.a.all()
	require.Len(t, recs, 2)
	assert.Equal(t, "artwork", recs[1].Operation)
	assert.Equal(t, cache.ArtworkKey(art.ID), recs[1].CacheKey)
}
