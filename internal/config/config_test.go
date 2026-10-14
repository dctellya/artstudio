package config

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func env(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestDefaults(t *testing.T) {
	c, err := LoadFrom(env(map[string]string{"CONTACT_EMAIL": "a@b.io"}))
	require.NoError(t, err)
	require.Equal(t, ":8443", c.ListenAddr)
	require.Equal(t, ":9090", c.MetricsAddr)
	require.Equal(t, "/certs/api.pem", c.TLSCertFile)
	require.Equal(t, "https://api.artic.edu/api/v1", c.UpstreamBaseURL)
	require.Equal(t, 2*time.Second, c.UpstreamAttemptTimeout)
	require.Equal(t, 5*time.Second, c.UpstreamBudget)
	require.Equal(t, 2, c.UpstreamMaxRetries)
	require.Equal(t, 100*time.Millisecond, c.UpstreamBackoffBase)
	require.Equal(t, 1.0, c.UpstreamRate)
	require.Equal(t, 5, c.UpstreamBurst)
	require.Equal(t, 10, c.UpstreamBulkhead)
	require.Equal(t, 5, c.BreakerConsecutive)
	require.Equal(t, 0.5, c.BreakerRatio)
	require.Equal(t, 20, c.BreakerMinRequests)
	require.Equal(t, 30*time.Second, c.BreakerOpenTimeout)
	require.Equal(t, 3, c.BreakerHalfOpenMax)
	require.Equal(t, 10000, c.CacheMaxEntries)
	require.Equal(t, 10*time.Minute, c.CacheTTLSearch)
	require.Equal(t, 60*time.Second, c.CacheTTLNegative)
	require.Equal(t, 5*time.Second, c.HTTPReadHeaderTimeout)
	require.Equal(t, 10.0, c.ClientRate)
	require.Equal(t, 200, c.MaxInFlight)
	require.Equal(t, 10*time.Second, c.ShutdownTimeout)
	require.Empty(t, c.DatabaseURL)
	require.Equal(t, 1000, c.AuditQueueSize)
	require.Equal(t, "artstudio", c.OTelServiceName)
	require.Equal(t, "info", c.LogLevel)
}

func TestValidation(t *testing.T) {
	base := func(k, v string) map[string]string {
		return map[string]string{"CONTACT_EMAIL": "a@b.io", k: v}
	}
	cases := map[string]map[string]string{
		"missing email":  {},
		"email no at":    {"CONTACT_EMAIL": "nope"},
		"bad scheme":     base("UPSTREAM_BASE_URL", "ftp://x"),
		"no host":        base("UPSTREAM_BASE_URL", "https://"),
		"zero duration":  base("UPSTREAM_BUDGET", "0s"),
		"neg duration":   base("CACHE_TTL_STALE", "-1m"),
		"bad duration":   base("PROBE_INTERVAL", "soon"),
		"bad number":     base("UPSTREAM_BULKHEAD", "ten"),
		"zero number":    base("MAX_IN_FLIGHT", "0"),
		"ratio over one": base("BREAKER_RATIO", "1.5"),
	}
	for name, m := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := LoadFrom(env(m))
			require.Error(t, err)
		})
	}
}

func TestRedaction(t *testing.T) {
	c, err := LoadFrom(env(map[string]string{ //nolint:gosec // test fixture
		"CONTACT_EMAIL": "a@b.io", "DATABASE_URL": "postgres://u:hunter2@db/x", "GRAFANA_ADMIN_PASSWORD": "s3cret",
	}))
	require.NoError(t, err)
	for _, s := range []string{c.String(), c.LogValue().String()} {
		require.NotContains(t, s, "hunter2")
		require.NotContains(t, s, "s3cret")
		require.True(t, strings.Contains(s, Redacted))
	}
}
