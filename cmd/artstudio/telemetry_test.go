package main

import (
	"context"
	"encoding/pem"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"

	"github.com/dctellya/artstudio/internal/config"
)

// The OTLP exporters must post to the standard signal paths on the configured https endpoint.
func TestSetupTelemetryOTLPPaths(t *testing.T) {
	var (
		mu    sync.Mutex
		paths = map[string]bool{}
	)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths[r.URL.Path] = true
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ca := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600))

	cfg := config.Config{OTLPEndpoint: srv.URL, OTLPCertificate: ca, OTelServiceName: "artstudio-test"}
	ctx := context.Background()
	h, flush, err := setupTelemetry(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, h)

	_, span := otel.Tracer("test").Start(ctx, "probe")
	span.End()
	slog.New(h).InfoContext(ctx, "probe log")

	fctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	require.NoError(t, flush(fctx))

	mu.Lock()
	defer mu.Unlock()
	got := make([]string, 0, len(paths))
	for p := range paths {
		got = append(got, p)
	}
	sort.Strings(got)
	require.Equal(t, []string{"/v1/logs", "/v1/traces"}, got)
}
