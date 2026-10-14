package artic

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dctellya/artstudio/internal/ports"
)

// upstreamError is a typed vendor failure: errors.Is matches kind, and
// RetryAfter satisfies ports.RetryAfterError.
type upstreamError struct {
	kind       error
	detail     string
	retryAfter time.Duration
}

var _ ports.RetryAfterError = (*upstreamError)(nil)

func (e *upstreamError) Error() string {
	if e.detail == "" {
		return e.kind.Error()
	}
	return fmt.Sprintf("%s: %s", e.kind, e.detail)
}
func (e *upstreamError) Unwrap() error             { return e.kind }
func (e *upstreamError) RetryAfter() time.Duration { return e.retryAfter }

func badUpstream(format string, args ...any) error {
	return &upstreamError{kind: ports.ErrBadUpstream, detail: fmt.Sprintf(format, args...)}
}

// parseRetryAfter reads delta-seconds or an HTTP date; 0 when absent or invalid.
func parseRetryAfter(h http.Header, now time.Time) time.Duration {
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0
	}
	if s, err := strconv.Atoi(v); err == nil && s > 0 {
		return time.Duration(s) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil && t.After(now) {
		return t.Sub(now)
	}
	return 0
}
