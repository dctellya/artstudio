// Package ports holds the frozen interfaces, types and sentinel errors shared
// by every artstudio package (SPEC §5).
package ports

import (
	"context"
	"errors"
	"time"
)

// Artwork is one artwork as returned to clients.
type Artwork struct {
	ID                                     int
	Title, Artist, Date, ImageID, ImageURL string
}

// SearchQuery is a search request; Q is normalised: trim, lower, collapse spaces.
type SearchQuery struct {
	Q           string
	Limit, Page int
}

// SearchResult is one page of search results.
type SearchResult struct {
	Items              []Artwork
	Total, Limit, Page int
}

// CacheStatus is "hit" | "miss" | "stale" | "negative" | "bypass".
type CacheStatus string

// Cache status values.
const (
	CacheHit      CacheStatus = "hit"
	CacheMiss     CacheStatus = "miss"
	CacheStale    CacheStatus = "stale"
	CacheNegative CacheStatus = "negative"
	CacheBypass   CacheStatus = "bypass"
)

// Meta describes how a response was produced.
type Meta struct {
	RequestID       string
	Cache           CacheStatus
	UpstreamLatency time.Duration
	Attempts        int
}

// Lookup is the cache-first, vendor-backed read path used by the HTTP layer.
type Lookup interface {
	Search(ctx context.Context, q SearchQuery) (SearchResult, Meta, error)
	Artwork(ctx context.Context, id int) (Artwork, Meta, error)
}

// Vendor is the resilient client for the third-party API.
type Vendor interface {
	Search(ctx context.Context, q SearchQuery) (SearchResult, Call, error)
	Artwork(ctx context.Context, id int) (Artwork, Call, error)
	Probe(ctx context.Context) (Call, error)
	BreakerState() string
}

// Call is the outcome of one vendor operation (all attempts).
type Call struct {
	Operation string
	Status    int
	Attempts  int
	Latency   time.Duration
	Breaker   string
	Body      []byte
	Err       error
}

// Cache is a byte cache with stale serving, negative entries and singleflight.
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, CacheStatus)
	Set(ctx context.Context, key string, v []byte, ttl time.Duration)
	SetNegative(ctx context.Context, key string)
	Do(ctx context.Context, key string, fn func() ([]byte, error)) ([]byte, bool, error)
	Len() int
}

// Auditor records every vendor call. Record never blocks; it drops and counts when full.
type Auditor interface {
	Record(VendorCall)
	Close(ctx context.Context) error
}

// VendorCall is one append-only audit row.
type VendorCall struct {
	TS                                      time.Time
	RequestID, TraceID, Operation, CacheKey string
	CacheStatus                             CacheStatus
	UpstreamStatus, Attempts                int
	LatencyMS                               int64
	Breaker, Error                          string
	Response                                []byte
}

// Checker is one /ready dependency check.
type Checker interface {
	Name() string
	Check(ctx context.Context) CheckResult
}

// CheckResult is a Checker outcome; vendor detail carries state, latency_ms,
// last_check, last_error, breaker and never gates readiness.
type CheckResult struct {
	OK     bool
	Detail map[string]any
}

// Sentinel errors returned by Vendor and Lookup.
var (
	ErrNotFound            = errors.New("not found")
	ErrUpstreamRateLimited = errors.New("upstream rate limited")
	ErrBreakerOpen         = errors.New("breaker open")
	ErrBulkheadFull        = errors.New("bulkhead full")
	ErrLimiterRejected     = errors.New("limiter rejected")
	ErrBadUpstream         = errors.New("bad upstream response")
	ErrBudgetExceeded      = errors.New("upstream budget exceeded")
)

// RetryAfterError is an error that tells the caller when to retry.
type RetryAfterError interface {
	error
	RetryAfter() time.Duration
}
