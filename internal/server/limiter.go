package server

import (
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type clientBucket struct {
	lim  *rate.Limiter
	seen time.Time
}

// clientLimiter is a token bucket per client IP. A bucket idle for longer than
// burst/rate is full again, so evicting it is indistinguishable from keeping it.
type clientLimiter struct {
	rate      rate.Limit
	burst     int
	idleAfter time.Duration
	now       func() time.Time

	mu        sync.Mutex
	buckets   map[string]*clientBucket
	lastSweep time.Time
}

func newClientLimiter(perSecond float64, burst int) *clientLimiter {
	return &clientLimiter{
		rate:      rate.Limit(perSecond),
		burst:     burst,
		idleAfter: time.Duration(float64(burst) / perSecond * float64(time.Second)),
		now:       time.Now,
		buckets:   make(map[string]*clientBucket),
	}
}

// allow reports whether the client may proceed; otherwise how long to wait.
func (l *clientLimiter) allow(key string) (bool, time.Duration) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweep(now)
	b, ok := l.buckets[key]
	if !ok {
		b = &clientBucket{lim: rate.NewLimiter(l.rate, l.burst)}
		l.buckets[key] = b
	}
	b.seen = now
	res := b.lim.ReserveN(now, 1)
	if delay := res.DelayFrom(now); delay > 0 {
		res.CancelAt(now)
		return false, delay
	}
	return true, 0
}

func (l *clientLimiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < l.idleAfter {
		return
	}
	l.lastSweep = now
	for k, b := range l.buckets {
		if now.Sub(b.seen) > l.idleAfter {
			delete(l.buckets, k)
		}
	}
}

// clientKey is the host of RemoteAddr; no proxy header is trusted.
func clientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
