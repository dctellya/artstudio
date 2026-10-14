// Package artic is the resilient client for the Art Institute of Chicago API:
// bulkhead, rate limiter, circuit breaker, per-attempt timeout inside a total
// budget, retries with full jitter and Retry-After, no redirects, typed errors.
package artic

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sony/gobreaker/v2"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/time/rate"

	"github.com/dctellya/artstudio/internal/config"
	"github.com/dctellya/artstudio/internal/ports"
)

// maxBody caps a vendor response; a search page is far smaller.
const maxBody = 8 << 20

// Client implements ports.Vendor.
type Client struct {
	cfg      config.Config
	base     string
	http     *http.Client
	rand     func() float64
	limiter  *rate.Limiter
	bulkhead chan struct{}
	breaker  *gobreaker.CircuitBreaker[struct{}]
	m        *metrics
	tracer   trace.Tracer
}

var _ ports.Vendor = (*Client)(nil)

// Option customises a Client.
type Option func(*Client)

// WithRand replaces the jitter source, returning values in [0,1) (tests).
func WithRand(f func() float64) Option { return func(c *Client) { c.rand = f } }

// WithHTTPClient replaces the HTTP client; redirects are still never followed.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// New builds a Client from cfg and registers its metrics on reg.
func New(cfg config.Config, reg prometheus.Registerer, opts ...Option) (*Client, error) {
	u, err := url.Parse(cfg.UpstreamBaseURL)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("artic: invalid upstream base URL %q", cfg.UpstreamBaseURL)
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	c := &Client{
		cfg:      cfg,
		base:     strings.TrimRight(cfg.UpstreamBaseURL, "/"),
		http:     &http.Client{Transport: otelhttp.NewTransport(tr)},
		rand:     rand.Float64,
		limiter:  rate.NewLimiter(rate.Limit(cfg.UpstreamRate), cfg.UpstreamBurst),
		bulkhead: make(chan struct{}, cfg.UpstreamBulkhead),
		m:        newMetrics(reg),
		tracer:   otel.Tracer("artstudio/artic"),
	}
	for _, o := range opts {
		o(c)
	}
	c.http.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c.m.initLabels(cfg.UpstreamMaxRetries)
	c.breaker = c.newBreaker()
	return c, nil
}

func (c *Client) newBreaker() *gobreaker.CircuitBreaker[struct{}] {
	return gobreaker.NewCircuitBreaker[struct{}](gobreaker.Settings{
		Name:        "artic",
		MaxRequests: halfOpenMax(c.cfg.BreakerHalfOpenMax),
		Interval:    c.cfg.BreakerInterval,
		Timeout:     c.cfg.BreakerOpenTimeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			if int(counts.ConsecutiveFailures) >= c.cfg.BreakerConsecutive {
				return true
			}
			return int(counts.Requests) >= c.cfg.BreakerMinRequests &&
				float64(counts.TotalFailures)/float64(counts.Requests) >= c.cfg.BreakerRatio
		},
		OnStateChange: func(_ string, from, to gobreaker.State) {
			c.m.transitions.WithLabelValues(from.String(), to.String()).Inc()
			c.m.breakerState.Set(breakerGauge(to))
		},
		// A 404 is a valid answer; a caller that gave up says nothing about the vendor.
		IsSuccessful: func(err error) bool { return err == nil || errors.Is(err, ports.ErrNotFound) },
		IsExcluded: func(err error) bool {
			return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
		},
	})
}

// halfOpenMax converts the validated (> 0) config value for gobreaker.
func halfOpenMax(n int) uint32 {
	if n < 1 || n > math.MaxUint32 {
		return 1
	}
	return uint32(n)
}

func breakerGauge(s gobreaker.State) float64 {
	switch s {
	case gobreaker.StateHalfOpen:
		return 1
	case gobreaker.StateOpen:
		return 2
	default:
		return 0
	}
}

// BreakerState is "closed", "half-open" or "open".
func (c *Client) BreakerState() string { return c.breaker.State().String() }

// Search queries the vendor's search endpoint.
func (c *Client) Search(ctx context.Context, q ports.SearchQuery) (ports.SearchResult, ports.Call, error) {
	var res ports.SearchResult
	v := url.Values{"q": {q.Q}, "limit": {strconv.Itoa(q.Limit)}, "page": {strconv.Itoa(q.Page)}, "fields": {fields}}
	call, err := c.do(ctx, "search", "/artworks/search", v, func(b []byte) (err error) {
		res, err = decodeSearch(b)
		return err
	})
	return res, call, err
}

// Artwork fetches one artwork.
func (c *Client) Artwork(ctx context.Context, id int) (ports.Artwork, ports.Call, error) {
	var art ports.Artwork
	v := url.Values{"fields": {fields}}
	call, err := c.do(ctx, "artwork", "/artworks/"+strconv.Itoa(id), v, func(b []byte) (err error) {
		art, err = decodeArtwork(b)
		return err
	})
	return art, call, err
}

// Probe makes the cheapest vendor call, through the same pipeline as real traffic.
func (c *Client) Probe(ctx context.Context) (ports.Call, error) {
	v := url.Values{"limit": {"1"}, "fields": {"id"}}
	return c.do(ctx, "probe", "/artworks", v, func(b []byte) error {
		if !json.Valid(b) {
			return errors.New("not JSON")
		}
		return nil
	})
}

// do runs one vendor operation: bulkhead, limiter, breaker around the budgeted retry loop.
func (c *Client) do(ctx context.Context, op, path string, q url.Values, decode func([]byte) error) (ports.Call, error) {
	start := time.Now()
	call := ports.Call{Operation: op}
	err := c.guarded(ctx, op, path, q, decode, &call)
	call.Latency = time.Since(start)
	call.Breaker = c.BreakerState()
	call.Err = err
	c.m.duration.WithLabelValues(op).Observe(call.Latency.Seconds())
	return call, err
}

func (c *Client) guarded(ctx context.Context, op, path string, q url.Values, decode func([]byte) error, call *ports.Call) error {
	select {
	case c.bulkhead <- struct{}{}:
		c.m.bulkheadInUse.Inc()
		defer func() { <-c.bulkhead; c.m.bulkheadInUse.Dec() }()
	default:
		c.m.bulkheadReject.Inc()
		return &upstreamError{kind: ports.ErrBulkheadFull}
	}
	if c.breaker.State() == gobreaker.StateOpen { // do not spend limiter tokens on a call that cannot go out
		return &upstreamError{kind: ports.ErrBreakerOpen}
	}
	if err := c.waitLimiter(ctx); err != nil {
		return err
	}
	budget, cancel := context.WithTimeout(ctx, c.cfg.UpstreamBudget)
	defer cancel()
	_, err := c.breaker.Execute(func() (struct{}, error) {
		return struct{}{}, c.attempts(ctx, budget, op, path, q, decode, call)
	})
	if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
		return &upstreamError{kind: ports.ErrBreakerOpen}
	}
	return err
}

func (c *Client) waitLimiter(ctx context.Context) error {
	r := c.limiter.Reserve()
	delay := r.Delay()
	if !r.OK() || delay > c.cfg.UpstreamLimiterMaxWait {
		r.Cancel()
		c.m.limiterReject.Inc()
		return &upstreamError{kind: ports.ErrLimiterRejected}
	}
	c.m.limiterWait.Observe(delay.Seconds())
	if delay <= 0 {
		return nil
	}
	t := time.NewTimer(delay)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		r.Cancel()
		return ctx.Err()
	}
}

// attempts is the retry loop. parent is the caller's context, budget the
// derived total-budget context.
func (c *Client) attempts(parent, budget context.Context, op, path string, q url.Values, decode func([]byte) error, call *ports.Call) error {
	for attempt := 1; ; attempt++ {
		call.Attempts = attempt
		status, body, hdr, err := c.once(budget, op, attempt, path, q)
		call.Status = status

		var last error
		var reason string
		switch {
		case err != nil:
			if parent.Err() != nil {
				return parent.Err()
			}
			if budget.Err() != nil {
				return &upstreamError{kind: ports.ErrBudgetExceeded}
			}
			last, reason = badUpstream("request failed: %v", err), "network"
		case status == http.StatusOK:
			call.Body = body
			if derr := decode(body); derr != nil {
				return badUpstream("undecodable response: %v", derr)
			}
			return nil
		case status == http.StatusNotFound:
			return &upstreamError{kind: ports.ErrNotFound}
		case status == http.StatusTooManyRequests:
			last = &upstreamError{kind: ports.ErrUpstreamRateLimited, detail: "status 429", retryAfter: parseRetryAfter(hdr, time.Now())}
			reason = "429"
		case status == http.StatusBadGateway, status == http.StatusServiceUnavailable, status == http.StatusGatewayTimeout:
			last, reason = badUpstream("status %d", status), strconv.Itoa(status)
		default: // 3xx (never followed), 403, other 4xx, 500: final
			return badUpstream("status %d", status)
		}

		if attempt > c.cfg.UpstreamMaxRetries {
			return last
		}
		delay := c.backoff(attempt - 1)
		if ra := parseRetryAfter(hdr, time.Now()); ra > 0 {
			if dl, ok := budget.Deadline(); ok && ra > time.Until(dl) {
				return last // the vendor asked for longer than we may wait
			}
			delay = max(delay, ra)
		}
		c.m.retries.WithLabelValues(op, reason).Inc()
		if err := sleep(budget, delay); err != nil {
			if parent.Err() != nil {
				return parent.Err()
			}
			return &upstreamError{kind: ports.ErrBudgetExceeded}
		}
	}
}

// backoff is full jitter: rand * min(max, base * 2^n).
func (c *Client) backoff(n int) time.Duration {
	d := c.cfg.UpstreamBackoffBase
	for i := 0; i < n && d < c.cfg.UpstreamBackoffMax; i++ {
		d *= 2
	}
	d = min(d, c.cfg.UpstreamBackoffMax)
	return time.Duration(c.rand() * float64(d))
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// once makes a single HTTP attempt inside its own timeout and span.
func (c *Client) once(budget context.Context, op string, attempt int, path string, q url.Values) (int, []byte, http.Header, error) {
	ctx, cancel := context.WithTimeout(budget, c.cfg.UpstreamAttemptTimeout)
	defer cancel()
	ctx, span := c.tracer.Start(ctx, "vendor.attempt", trace.WithAttributes(attribute.Int("attempt", attempt)))
	defer span.End()

	label := func(status string) {
		c.m.requests.WithLabelValues(op, status, strconv.Itoa(attempt)).Inc()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path+"?"+q.Encode(), nil)
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("AIC-User-Agent", "artstudio ("+c.cfg.ContactEmail+")")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "request failed")
		label("error")
		return 0, nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	span.SetAttributes(attribute.Int("http.status_code", resp.StatusCode))
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		span.RecordError(err)
		label("error")
		return resp.StatusCode, nil, resp.Header, err
	}
	label(strconv.Itoa(resp.StatusCode))
	return resp.StatusCode, body, resp.Header, nil
}
