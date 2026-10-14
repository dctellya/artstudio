package artic_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/dctellya/artstudio/internal/artic"
	"github.com/dctellya/artstudio/internal/config"
)

const (
	iiifURL     = "https://www.artic.edu/iiif/2"
	imageID     = "2d484387-2509-5e8e-2c43-22f9981972eb"
	artworkBody = `{"data":{"id":27992,"title":"A Sunday on La Grande Jatte","artist_title":"Georges Seurat","date_display":"1884-86","image_id":"` + imageID + `"},"config":{"iiif_url":"` + iiifURL + `"}}`
	searchBody  = `{"pagination":{"total":42,"limit":10,"offset":10,"total_pages":5,"current_page":2},"data":[{"id":27992,"title":"A Sunday on La Grande Jatte","artist_title":"Georges Seurat","date_display":"1884-86","image_id":"` + imageID + `"}],"config":{"iiif_url":"` + iiifURL + `"}}`
)

// testConfig is the default config pointed at srvURL with test-sized durations.
func testConfig(t *testing.T, srvURL string, mod func(*config.Config)) config.Config {
	t.Helper()
	env := map[string]string{"CONTACT_EMAIL": "ops@example.com", "UPSTREAM_BASE_URL": srvURL + "/api/v1"}
	cfg, err := config.LoadFrom(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	require.NoError(t, err)
	cfg.UpstreamAttemptTimeout = 200 * time.Millisecond
	cfg.UpstreamBudget = 2 * time.Second
	cfg.UpstreamMaxRetries = 2
	cfg.UpstreamBackoffBase = time.Millisecond
	cfg.UpstreamBackoffMax = 5 * time.Millisecond
	cfg.UpstreamRate = 1000
	cfg.UpstreamBurst = 1000
	cfg.UpstreamLimiterMaxWait = 100 * time.Millisecond
	cfg.ProbeInterval = 20 * time.Millisecond
	if mod != nil {
		mod(&cfg)
	}
	return cfg
}

type env struct {
	client *artic.Client
	reg    *prometheus.Registry
	srv    *httptest.Server
	hits   *atomic.Int64
}

// newEnv starts a vendor stub running h and builds a client against it.
func newEnv(t *testing.T, h http.HandlerFunc, mod func(*config.Config), opts ...artic.Option) env {
	t.Helper()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	reg := prometheus.NewRegistry()
	c, err := artic.New(testConfig(t, srv.URL, mod), reg, opts...)
	require.NoError(t, err)
	return env{client: c, reg: reg, srv: srv, hits: &hits}
}

func status(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }
}

func okArtwork(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(artworkBody))
}

// metric sums the samples of the named family whose labels include want.
func metric(t *testing.T, reg *prometheus.Registry, name string, want map[string]string) float64 {
	t.Helper()
	mfs, err := reg.Gather()
	require.NoError(t, err)
	var sum float64
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
	next:
		for _, m := range mf.GetMetric() {
			have := map[string]string{}
			for _, l := range m.GetLabel() {
				have[l.GetName()] = l.GetValue()
			}
			for k, v := range want {
				if have[k] != v {
					continue next
				}
			}
			sum += m.GetCounter().GetValue() + m.GetGauge().GetValue()
		}
	}
	return sum
}
