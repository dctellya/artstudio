package artic_test

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dctellya/artstudio/internal/artic"
	"github.com/dctellya/artstudio/internal/config"
	"github.com/dctellya/artstudio/internal/ports"
)

var bg = context.Background()

func hang(w http.ResponseWriter, r *http.Request) {
	select {
	case <-r.Context().Done():
	case <-time.After(3 * time.Second):
	}
}

func TestClientImplementsVendor(t *testing.T) {
	var _ ports.Vendor = (*artic.Client)(nil)
}

func TestAttemptTimeout(t *testing.T) {
	e := newEnv(t, hang, func(c *config.Config) {
		c.UpstreamAttemptTimeout = 50 * time.Millisecond
		c.UpstreamMaxRetries = 0
		c.UpstreamBudget = 2 * time.Second
	})
	start := time.Now()
	_, call, err := e.client.Artwork(bg, 27992)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ports.ErrBudgetExceeded, "the attempt timed out, not the budget")
	assert.Less(t, time.Since(start), 500*time.Millisecond)
	assert.Equal(t, 1, call.Attempts)
}

func TestBudgetExceeded(t *testing.T) {
	e := newEnv(t, hang, func(c *config.Config) {
		c.UpstreamAttemptTimeout = 100 * time.Millisecond
		c.UpstreamBudget = 250 * time.Millisecond
		c.UpstreamMaxRetries = 5
	})
	start := time.Now()
	_, _, err := e.client.Artwork(bg, 27992)
	require.ErrorIs(t, err, ports.ErrBudgetExceeded)
	assert.Less(t, time.Since(start), 700*time.Millisecond)
}

func TestRetryPolicy(t *testing.T) {
	cases := []struct {
		name     string
		h        http.HandlerFunc
		wantHits int64
		wantErr  error
	}{
		{"429 retried then rate limited", status(429), 3, ports.ErrUpstreamRateLimited},
		{"502 retried", status(502), 3, ports.ErrBadUpstream},
		{"503 retried", status(503), 3, ports.ErrBadUpstream},
		{"504 retried", status(504), 3, ports.ErrBadUpstream},
		{"500 not retried", status(500), 1, ports.ErrBadUpstream},
		{"403 not retried", status(403), 1, ports.ErrBadUpstream},
		{"undecodable body not retried", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>")) }, 1, ports.ErrBadUpstream},
		{"network error retried", func(w http.ResponseWriter, _ *http.Request) {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
		}, 3, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, tc.h, nil)
			_, call, err := e.client.Artwork(bg, 27992)
			require.Error(t, err)
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
			}
			assert.Equal(t, tc.wantHits, e.hits.Load())
			assert.EqualValues(t, tc.wantHits, call.Attempts)
		})
	}
}

func TestRetrySucceedsAfterTransientFailure(t *testing.T) {
	var n atomic.Int64
	e := newEnv(t, func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			w.WriteHeader(503)
			return
		}
		okArtwork(w, r)
	}, nil)
	art, call, err := e.client.Artwork(bg, 27992)
	require.NoError(t, err)
	assert.Equal(t, 27992, art.ID)
	assert.Equal(t, 2, call.Attempts)
	assert.EqualValues(t, 1, metric(t, e.reg, "artstudio_upstream_retries_total", map[string]string{"operation": "artwork"}))
}

func TestBackoffFullJitterCapped(t *testing.T) {
	mod := func(c *config.Config) {
		c.UpstreamBackoffBase = 40 * time.Millisecond
		c.UpstreamBackoffMax = 60 * time.Millisecond
		c.UpstreamMaxRetries = 2
	}
	// rand=0: full jitter lets the sleep collapse to ~0.
	e0 := newEnv(t, status(503), mod, artic.WithRand(func() float64 { return 0 }))
	start := time.Now()
	_, _, _ = e0.client.Artwork(bg, 1)
	assert.Less(t, time.Since(start), 60*time.Millisecond)

	// rand~1: sleeps are min(max, base*2^n) = 40ms then 60ms (cap), never 80ms.
	var calls atomic.Int64
	e1 := newEnv(t, status(503), mod, artic.WithRand(func() float64 { calls.Add(1); return 0.999 }))
	start = time.Now()
	_, _, _ = e1.client.Artwork(bg, 1)
	elapsed := time.Since(start)
	assert.GreaterOrEqual(t, elapsed, 90*time.Millisecond)
	assert.Less(t, elapsed, 180*time.Millisecond)
	assert.EqualValues(t, 2, calls.Load(), "one jitter draw per retry")
}

func TestRetryAfterHonouredWhenItFitsBudget(t *testing.T) {
	var n atomic.Int64
	e := newEnv(t, func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			return
		}
		okArtwork(w, r)
	}, func(c *config.Config) {
		c.UpstreamBudget = 3 * time.Second
		c.UpstreamAttemptTimeout = time.Second
	})
	start := time.Now()
	_, call, err := e.client.Artwork(bg, 27992)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, time.Since(start), 900*time.Millisecond)
	assert.Equal(t, 2, call.Attempts)
}

func TestRetryAfterExceedingBudgetStopsAndIsReported(t *testing.T) {
	e := newEnv(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(429)
	}, func(c *config.Config) { c.UpstreamBudget = 300 * time.Millisecond })
	start := time.Now()
	_, _, err := e.client.Artwork(bg, 27992)
	require.ErrorIs(t, err, ports.ErrUpstreamRateLimited)
	var ra ports.RetryAfterError
	require.ErrorAs(t, err, &ra)
	assert.Equal(t, 120*time.Second, ra.RetryAfter())
	assert.Less(t, time.Since(start), 250*time.Millisecond, "must not wait for a Retry-After that cannot fit")
	assert.EqualValues(t, 1, e.hits.Load())
}

func TestRedirectNotFollowed(t *testing.T) {
	var other atomic.Int64
	e := newEnv(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/elsewhere" {
			other.Add(1)
			okArtwork(w, r)
			return
		}
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	}, nil)
	_, call, err := e.client.Artwork(bg, 27992)
	require.ErrorIs(t, err, ports.ErrBadUpstream)
	assert.Zero(t, other.Load(), "redirect target must never be requested")
	assert.Equal(t, 1, call.Attempts, "3xx is not retried")
	assert.Equal(t, 302, call.Status)
}

func TestBreakerOpensAfterConsecutiveFailures(t *testing.T) {
	e := newEnv(t, status(503), func(c *config.Config) { c.UpstreamMaxRetries = 0 })
	for i := 0; i < 5; i++ {
		require.Equal(t, "closed", e.client.BreakerState(), "call %d", i)
		_, _, err := e.client.Artwork(bg, 1)
		require.ErrorIs(t, err, ports.ErrBadUpstream)
	}
	assert.Equal(t, "open", e.client.BreakerState())
	_, call, err := e.client.Artwork(bg, 1)
	require.ErrorIs(t, err, ports.ErrBreakerOpen)
	assert.EqualValues(t, 5, e.hits.Load(), "open breaker must not reach the vendor")
	assert.Equal(t, "open", call.Breaker)
	assert.EqualValues(t, 1, metric(t, e.reg, "artstudio_upstream_breaker_transitions_total", map[string]string{"from": "closed", "to": "open"}))
	assert.EqualValues(t, 2, metric(t, e.reg, "artstudio_upstream_breaker_state", nil))
}

func TestBreakerOpensOnFailureRatio(t *testing.T) {
	var n atomic.Int64
	e := newEnv(t, func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) <= 8 {
			okArtwork(w, r) // 8 successes first, so the consecutive rule cannot be what trips
			return
		}
		w.WriteHeader(503)
	}, func(c *config.Config) {
		c.UpstreamMaxRetries = 0
		c.BreakerConsecutive = 1000
		c.BreakerRatio = 0.5
		c.BreakerMinRequests = 20
	})
	for i := 0; i < 19; i++ {
		_, _, _ = e.client.Artwork(bg, 1)
	}
	assert.Equal(t, "closed", e.client.BreakerState(), "11/19 failed but below the 20-request minimum")
	_, _, _ = e.client.Artwork(bg, 1) // 20th request, 12/20 = 60% failed
	assert.Equal(t, "open", e.client.BreakerState())
	_, _, err := e.client.Artwork(bg, 1)
	assert.ErrorIs(t, err, ports.ErrBreakerOpen)
}

func TestBreakerHalfOpenAdmitsThreeTrials(t *testing.T) {
	var healthy atomic.Bool
	var inHandler atomic.Int64
	release := make(chan struct{})
	e := newEnv(t, func(w http.ResponseWriter, r *http.Request) {
		if !healthy.Load() {
			w.WriteHeader(503)
			return
		}
		inHandler.Add(1)
		<-release
		okArtwork(w, r)
	}, func(c *config.Config) {
		c.UpstreamMaxRetries = 0
		c.BreakerConsecutive = 2
		c.BreakerOpenTimeout = 100 * time.Millisecond
		c.BreakerHalfOpenMax = 3
		c.UpstreamAttemptTimeout = 3 * time.Second
		c.UpstreamBudget = 5 * time.Second
	})
	for i := 0; i < 2; i++ {
		_, _, _ = e.client.Artwork(bg, 1)
	}
	require.Equal(t, "open", e.client.BreakerState())
	time.Sleep(150 * time.Millisecond)
	healthy.Store(true)

	errs := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func() { _, _, err := e.client.Artwork(bg, 1); errs <- err }()
	}
	require.Eventually(t, func() bool { return inHandler.Load() == 3 }, 2*time.Second, 5*time.Millisecond, "3 trials admitted")
	_, _, err := e.client.Artwork(bg, 1)
	assert.ErrorIs(t, err, ports.ErrBreakerOpen, "4th concurrent trial rejected")
	close(release)
	for i := 0; i < 3; i++ {
		assert.NoError(t, <-errs)
	}
	assert.Equal(t, "closed", e.client.BreakerState(), "3 successful trials close the breaker")
}

func TestLimiterRejectsWhenWaitExceedsMax(t *testing.T) {
	e := newEnv(t, okArtwork, func(c *config.Config) {
		c.UpstreamRate = 5 // a token every 200ms
		c.UpstreamBurst = 1
		c.UpstreamLimiterMaxWait = 100 * time.Millisecond
	})
	_, _, err := e.client.Artwork(bg, 1)
	require.NoError(t, err)
	start := time.Now()
	_, _, err = e.client.Artwork(bg, 1)
	require.ErrorIs(t, err, ports.ErrLimiterRejected)
	assert.Less(t, time.Since(start), 150*time.Millisecond, "reject fast instead of waiting")
	assert.EqualValues(t, 1, e.hits.Load())
	assert.EqualValues(t, 1, metric(t, e.reg, "artstudio_upstream_limiter_rejected_total", nil))
}

func TestLimiterWaitsWithinMax(t *testing.T) {
	e := newEnv(t, okArtwork, func(c *config.Config) {
		c.UpstreamRate = 20 // a token every 50ms
		c.UpstreamBurst = 1
		c.UpstreamLimiterMaxWait = 500 * time.Millisecond
	})
	_, _, err := e.client.Artwork(bg, 1)
	require.NoError(t, err)
	start := time.Now()
	_, _, err = e.client.Artwork(bg, 1)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, time.Since(start), 30*time.Millisecond)
	assert.EqualValues(t, 2, e.hits.Load())
}

func TestBulkheadRejects11thConcurrentCall(t *testing.T) {
	var inHandler atomic.Int64
	release := make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	t.Cleanup(free)
	e := newEnv(t, func(w http.ResponseWriter, r *http.Request) {
		inHandler.Add(1)
		<-release
		okArtwork(w, r)
	}, func(c *config.Config) {
		c.UpstreamBulkhead = 10
		c.UpstreamAttemptTimeout = 5 * time.Second
		c.UpstreamBudget = 10 * time.Second
	})
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func() { _, _, err := e.client.Artwork(bg, 1); errs <- err }()
	}
	require.Eventually(t, func() bool { return inHandler.Load() == 10 }, 3*time.Second, 5*time.Millisecond)
	assert.EqualValues(t, 10, metric(t, e.reg, "artstudio_upstream_bulkhead_in_use", nil))
	_, _, err := e.client.Artwork(bg, 1)
	require.ErrorIs(t, err, ports.ErrBulkheadFull)
	assert.EqualValues(t, 1, metric(t, e.reg, "artstudio_upstream_bulkhead_rejected_total", nil))
	assert.EqualValues(t, 10, e.hits.Load())
	free()
	for i := 0; i < 10; i++ {
		assert.NoError(t, <-errs)
	}
	assert.EqualValues(t, 0, metric(t, e.reg, "artstudio_upstream_bulkhead_in_use", nil))
}

func TestNotFound(t *testing.T) {
	e := newEnv(t, status(404), nil)
	for i := 0; i < 6; i++ {
		_, call, err := e.client.Artwork(bg, 999)
		require.ErrorIs(t, err, ports.ErrNotFound)
		assert.Equal(t, 404, call.Status)
		assert.Equal(t, "artwork", call.Operation)
	}
	assert.EqualValues(t, 6, e.hits.Load(), "404 is final, never retried")
	assert.Equal(t, "closed", e.client.BreakerState(), "404 is not a breaker failure")
}

func TestContactHeaderSent(t *testing.T) {
	var got atomic.Value
	e := newEnv(t, func(w http.ResponseWriter, r *http.Request) {
		got.Store(r.Header.Get("AIC-User-Agent"))
		okArtwork(w, r)
	}, nil)
	_, _, err := e.client.Artwork(bg, 27992)
	require.NoError(t, err)
	assert.Equal(t, "artstudio (ops@example.com)", got.Load())
}

func TestSearchRequestAndImageURL(t *testing.T) {
	var path, q, limit, page atomic.Value
	e := newEnv(t, func(w http.ResponseWriter, r *http.Request) {
		path.Store(r.URL.Path)
		q.Store(r.URL.Query().Get("q"))
		limit.Store(r.URL.Query().Get("limit"))
		page.Store(r.URL.Query().Get("page"))
		_, _ = w.Write([]byte(searchBody))
	}, nil)
	res, call, err := e.client.Search(bg, ports.SearchQuery{Q: "monet", Limit: 10, Page: 2})
	require.NoError(t, err)
	assert.Equal(t, "/api/v1/artworks/search", path.Load())
	assert.Equal(t, "monet", q.Load())
	assert.Equal(t, "10", limit.Load())
	assert.Equal(t, "2", page.Load())
	assert.Equal(t, "search", call.Operation)
	assert.Equal(t, 200, call.Status)
	assert.NotEmpty(t, call.Body)
	assert.Equal(t, 42, res.Total)
	assert.Equal(t, 10, res.Limit)
	assert.Equal(t, 2, res.Page)
	require.Len(t, res.Items, 1)
	assert.Equal(t, ports.Artwork{
		ID: 27992, Title: "A Sunday on La Grande Jatte", Artist: "Georges Seurat", Date: "1884-86",
		ImageID:  imageID,
		ImageURL: iiifURL + "/" + imageID + "/full/843,/0/default.jpg",
	}, res.Items[0])
}

func TestArtworkDecoded(t *testing.T) {
	var path atomic.Value
	e := newEnv(t, func(w http.ResponseWriter, r *http.Request) { path.Store(r.URL.Path); okArtwork(w, r) }, nil)
	art, _, err := e.client.Artwork(bg, 27992)
	require.NoError(t, err)
	assert.Equal(t, "/api/v1/artworks/27992", path.Load())
	assert.Equal(t, 27992, art.ID)
	assert.Equal(t, iiifURL+"/"+imageID+"/full/843,/0/default.jpg", art.ImageURL)
}
