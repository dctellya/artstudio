package artic_test

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dctellya/artstudio/internal/artic"
)

func TestMetricFamiliesExportedFromStart(t *testing.T) {
	reg := prometheus.NewRegistry()
	_, err := artic.New(testConfig(t, "http://unused", nil), reg)
	require.NoError(t, err)
	mfs, err := reg.Gather()
	require.NoError(t, err)
	have := map[string]bool{}
	for _, mf := range mfs {
		have[mf.GetName()] = true
	}
	for _, name := range []string{
		"artstudio_upstream_retries_total", "artstudio_upstream_breaker_transitions_total",
		"artstudio_upstream_requests_total", "artstudio_upstream_request_duration_seconds",
	} {
		assert.True(t, have[name], name)
	}
}
