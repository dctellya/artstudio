// Package config loads and validates the process configuration from the
// environment (SPEC §5 Config). Secrets are redacted when logged.
package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Redacted replaces secret values in logs.
const Redacted = "[redacted]"

// Config holds every setting of the service.
type Config struct {
	ListenAddr  string
	MetricsAddr string
	TLSCertFile string
	TLSKeyFile  string
	TLSCAFile   string

	ContactEmail    string
	UpstreamBaseURL string

	UpstreamAttemptTimeout time.Duration
	UpstreamBudget         time.Duration
	UpstreamMaxRetries     int
	UpstreamBackoffBase    time.Duration
	UpstreamBackoffMax     time.Duration
	UpstreamRate           float64
	UpstreamBurst          int
	UpstreamLimiterMaxWait time.Duration
	UpstreamBulkhead       int

	BreakerConsecutive int
	BreakerRatio       float64
	BreakerMinRequests int
	BreakerInterval    time.Duration
	BreakerOpenTimeout time.Duration
	BreakerHalfOpenMax int

	ProbeInterval time.Duration

	CacheMaxEntries  int
	CacheTTLSearch   time.Duration
	CacheTTLArtwork  time.Duration
	CacheTTLNegative time.Duration
	CacheTTLStale    time.Duration

	HTTPReadHeaderTimeout time.Duration
	HTTPReadTimeout       time.Duration
	HTTPWriteTimeout      time.Duration
	HTTPIdleTimeout       time.Duration

	ClientRate  float64
	ClientBurst int
	MaxInFlight int

	ShutdownTimeout time.Duration

	DatabaseURL    string // secret
	DBMaxConns     int
	AuditQueueSize int
	AuditBatch     int
	AuditFlush     time.Duration

	OTLPEndpoint    string
	OTLPCertificate string
	OTelServiceName string
	LogLevel        string

	GrafanaAdminPassword string // secret
}

// Load reads the environment, applies defaults and validates.
func Load() (Config, error) { return LoadFrom(os.LookupEnv) }

// LoadFrom is Load with an injectable environment lookup.
func LoadFrom(lookup func(string) (string, bool)) (Config, error) {
	l := &loader{lookup: lookup}
	c := Config{
		ListenAddr:  l.str("LISTEN_ADDR", ":8443"),
		MetricsAddr: l.str("METRICS_ADDR", ":9090"),
		TLSCertFile: l.str("TLS_CERT_FILE", "/certs/api.pem"),
		TLSKeyFile:  l.str("TLS_KEY_FILE", "/certs/api-key.pem"),
		TLSCAFile:   l.str("TLS_CA_FILE", "/certs/ca.pem"),

		ContactEmail:    l.str("CONTACT_EMAIL", ""),
		UpstreamBaseURL: l.str("UPSTREAM_BASE_URL", "https://api.artic.edu/api/v1"),

		UpstreamAttemptTimeout: l.dur("UPSTREAM_ATTEMPT_TIMEOUT", 2*time.Second),
		UpstreamBudget:         l.dur("UPSTREAM_BUDGET", 5*time.Second),
		UpstreamMaxRetries:     l.num("UPSTREAM_MAX_RETRIES", 2),
		UpstreamBackoffBase:    l.dur("UPSTREAM_BACKOFF_BASE", 100*time.Millisecond),
		UpstreamBackoffMax:     l.dur("UPSTREAM_BACKOFF_MAX", time.Second),
		UpstreamRate:           l.float("UPSTREAM_RATE", 1),
		UpstreamBurst:          l.num("UPSTREAM_BURST", 5),
		UpstreamLimiterMaxWait: l.dur("UPSTREAM_LIMITER_MAX_WAIT", time.Second),
		UpstreamBulkhead:       l.num("UPSTREAM_BULKHEAD", 10),

		BreakerConsecutive: l.num("BREAKER_CONSECUTIVE", 5),
		BreakerRatio:       l.float("BREAKER_RATIO", 0.5),
		BreakerMinRequests: l.num("BREAKER_MIN_REQUESTS", 20),
		BreakerInterval:    l.dur("BREAKER_INTERVAL", 60*time.Second),
		BreakerOpenTimeout: l.dur("BREAKER_OPEN_TIMEOUT", 30*time.Second),
		BreakerHalfOpenMax: l.num("BREAKER_HALF_OPEN_MAX", 3),

		ProbeInterval: l.dur("PROBE_INTERVAL", 30*time.Second),

		CacheMaxEntries:  l.num("CACHE_MAX_ENTRIES", 10000),
		CacheTTLSearch:   l.dur("CACHE_TTL_SEARCH", 10*time.Minute),
		CacheTTLArtwork:  l.dur("CACHE_TTL_ARTWORK", 60*time.Minute),
		CacheTTLNegative: l.dur("CACHE_TTL_NEGATIVE", 60*time.Second),
		CacheTTLStale:    l.dur("CACHE_TTL_STALE", 60*time.Minute),

		HTTPReadHeaderTimeout: l.dur("HTTP_READ_HEADER_TIMEOUT", 5*time.Second),
		HTTPReadTimeout:       l.dur("HTTP_READ_TIMEOUT", 10*time.Second),
		HTTPWriteTimeout:      l.dur("HTTP_WRITE_TIMEOUT", 15*time.Second),
		HTTPIdleTimeout:       l.dur("HTTP_IDLE_TIMEOUT", 60*time.Second),

		ClientRate:  l.float("CLIENT_RATE", 10),
		ClientBurst: l.num("CLIENT_BURST", 20),
		MaxInFlight: l.num("MAX_IN_FLIGHT", 200),

		ShutdownTimeout: l.dur("SHUTDOWN_TIMEOUT", 10*time.Second),

		DatabaseURL:    l.str("DATABASE_URL", ""),
		DBMaxConns:     l.num("DB_MAX_CONNS", 10),
		AuditQueueSize: l.num("AUDIT_QUEUE_SIZE", 1000),
		AuditBatch:     l.num("AUDIT_BATCH", 100),
		AuditFlush:     l.dur("AUDIT_FLUSH", time.Second),

		OTLPEndpoint:    l.str("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		OTLPCertificate: l.str("OTEL_EXPORTER_OTLP_CERTIFICATE", "/certs/ca.pem"),
		OTelServiceName: l.str("OTEL_SERVICE_NAME", "artstudio"),
		LogLevel:        l.str("LOG_LEVEL", "info"),

		GrafanaAdminPassword: l.str("GRAFANA_ADMIN_PASSWORD", ""),
	}
	if l.err == nil {
		l.err = c.validate()
	}
	if l.err != nil {
		return Config{}, l.err
	}
	return c, nil
}

func (c Config) validate() error {
	if !strings.Contains(c.ContactEmail, "@") {
		return fmt.Errorf("CONTACT_EMAIL is required and must contain '@'")
	}
	u, err := url.Parse(c.UpstreamBaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("UPSTREAM_BASE_URL must be an http or https URL")
	}
	durations := map[string]time.Duration{
		"UPSTREAM_ATTEMPT_TIMEOUT": c.UpstreamAttemptTimeout, "UPSTREAM_BUDGET": c.UpstreamBudget,
		"UPSTREAM_BACKOFF_BASE": c.UpstreamBackoffBase, "UPSTREAM_BACKOFF_MAX": c.UpstreamBackoffMax,
		"UPSTREAM_LIMITER_MAX_WAIT": c.UpstreamLimiterMaxWait, "BREAKER_INTERVAL": c.BreakerInterval,
		"BREAKER_OPEN_TIMEOUT": c.BreakerOpenTimeout, "PROBE_INTERVAL": c.ProbeInterval,
		"CACHE_TTL_SEARCH": c.CacheTTLSearch, "CACHE_TTL_ARTWORK": c.CacheTTLArtwork,
		"CACHE_TTL_NEGATIVE": c.CacheTTLNegative, "CACHE_TTL_STALE": c.CacheTTLStale,
		"HTTP_READ_HEADER_TIMEOUT": c.HTTPReadHeaderTimeout, "HTTP_READ_TIMEOUT": c.HTTPReadTimeout,
		"HTTP_WRITE_TIMEOUT": c.HTTPWriteTimeout, "HTTP_IDLE_TIMEOUT": c.HTTPIdleTimeout,
		"SHUTDOWN_TIMEOUT": c.ShutdownTimeout, "AUDIT_FLUSH": c.AuditFlush,
	}
	for k, v := range durations {
		if v <= 0 {
			return fmt.Errorf("%s must be > 0", k)
		}
	}
	positives := map[string]float64{
		"UPSTREAM_RATE": c.UpstreamRate, "UPSTREAM_BURST": float64(c.UpstreamBurst),
		"UPSTREAM_BULKHEAD": float64(c.UpstreamBulkhead), "BREAKER_CONSECUTIVE": float64(c.BreakerConsecutive),
		"BREAKER_RATIO": c.BreakerRatio, "BREAKER_MIN_REQUESTS": float64(c.BreakerMinRequests),
		"BREAKER_HALF_OPEN_MAX": float64(c.BreakerHalfOpenMax), "CACHE_MAX_ENTRIES": float64(c.CacheMaxEntries),
		"CLIENT_RATE": c.ClientRate, "CLIENT_BURST": float64(c.ClientBurst), "MAX_IN_FLIGHT": float64(c.MaxInFlight),
		"DB_MAX_CONNS": float64(c.DBMaxConns), "AUDIT_QUEUE_SIZE": float64(c.AuditQueueSize), "AUDIT_BATCH": float64(c.AuditBatch),
	}
	for k, v := range positives {
		if v <= 0 {
			return fmt.Errorf("%s must be > 0", k)
		}
	}
	if c.BreakerRatio > 1 {
		return fmt.Errorf("BREAKER_RATIO must be <= 1")
	}
	if c.UpstreamMaxRetries < 0 {
		return fmt.Errorf("UPSTREAM_MAX_RETRIES must be >= 0")
	}
	return nil
}

// LogValue implements slog.LogValuer; secrets are redacted.
func (c Config) LogValue() slog.Value {
	return slog.StringValue(c.String())
}

// String renders the config with DATABASE_URL and GRAFANA_ADMIN_PASSWORD redacted.
func (c Config) String() string {
	r := c
	if r.DatabaseURL != "" {
		r.DatabaseURL = Redacted
	}
	if r.GrafanaAdminPassword != "" {
		r.GrafanaAdminPassword = Redacted
	}
	return fmt.Sprintf("%+v", plainConfig(r))
}

// plainConfig has no methods, so %+v does not recurse into String.
type plainConfig Config

type loader struct {
	lookup func(string) (string, bool)
	err    error
}

func (l *loader) raw(key string) (string, bool) {
	v, ok := l.lookup(key)
	v = strings.TrimSpace(v)
	return v, ok && v != ""
}

func (l *loader) str(key, def string) string {
	if v, ok := l.raw(key); ok {
		return v
	}
	return def
}

func (l *loader) dur(key string, def time.Duration) time.Duration {
	v, ok := l.raw(key)
	if !ok {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil && l.err == nil {
		l.err = fmt.Errorf("%s: %w", key, err)
	}
	return d
}

func (l *loader) num(key string, def int) int {
	v, ok := l.raw(key)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil && l.err == nil {
		l.err = fmt.Errorf("%s: %w", key, err)
	}
	return n
}

func (l *loader) float(key string, def float64) float64 {
	v, ok := l.raw(key)
	if !ok {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil && l.err == nil {
		l.err = fmt.Errorf("%s: %w", key, err)
	}
	return f
}
