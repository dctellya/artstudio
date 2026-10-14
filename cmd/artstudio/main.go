// Command artstudio is the Art Institute of Chicago gateway.
package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/dctellya/artstudio/internal/config"
	"github.com/dctellya/artstudio/internal/ports"
	"github.com/dctellya/artstudio/internal/skeleton"
)

// Set by -ldflags "-X main.version=... -X main.commit=...".
var (
	version = "dev"
	commit  = "none"
)

const (
	requestIDHeader   = "X-Request-ID"
	cacheHeader       = "X-Cache"
	hstsValue         = "max-age=63072000; includeSubDomains"
	defaultListen     = ":8443"
	healthcheckTO     = 3 * time.Second
	defaultLimit      = 12
	defaultPage       = 1
	maxQueryLen       = 100
	exitFailure       = 1
	maxRequestIDChars = 128
	requestIDBytes    = 16
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := healthcheck(); err != nil {
			fmt.Fprintln(os.Stderr, "healthcheck:", err)
			os.Exit(exitFailure)
		}
		return
	}
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(exitFailure)
	}
}

// healthcheck GETs /health on the local TLS listener, trusting TLS_CA_FILE.
func healthcheck() error {
	caFile := envOr("TLS_CA_FILE", "/certs/ca.pem")
	pemBytes, err := os.ReadFile(caFile) //nolint:gosec // operator-supplied CA path
	if err != nil {
		return err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		return errors.New("no CA certificate in " + caFile)
	}
	_, port, err := net.SplitHostPort(envOr("LISTEN_ADDR", defaultListen))
	if err != nil {
		return err
	}
	client := &http.Client{
		Timeout:   healthcheckTO,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS13}},
	}
	resp, err := client.Get("https://localhost:" + port + "/health") //nolint:noctx // bounded by client timeout
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck // read-only body
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		return fmt.Errorf("LOG_LEVEL: %w", err)
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(log)
	log.Info("starting", "version", version, "commit", commit, "config", cfg)

	cert, err := tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile)
	if err != nil {
		return fmt.Errorf("load TLS keypair: %w", err)
	}
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}}

	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	buildInfo := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "artstudio", Name: "build_info", Help: "Build information.",
		ConstLabels: prometheus.Labels{"version": version, "commit": commit, "go_version": runtime.Version()},
	})
	buildInfo.Set(1)
	reg.MustRegister(buildInfo)

	s := &app{lookup: skeleton.NewLookup(cfg), log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /art/{query}", s.search)

	api := newServer(cfg.ListenAddr, hsts(requestID(mux)), tlsCfg, cfg)
	metricsMux := http.NewServeMux()
	metricsMux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	metrics := newServer(cfg.MetricsAddr, metricsMux, tlsCfg, cfg)

	errs := make(chan error, 2)
	for _, srv := range []*http.Server{api, metrics} {
		go func() {
			log.Info("listening", "addr", srv.Addr)
			// Certificates come from tlsCfg.
			if err := srv.ListenAndServeTLS("", ""); !errors.Is(err, http.ErrServerClosed) {
				errs <- err
			}
		}()
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	var shutErr error
	for _, srv := range []*http.Server{api, metrics} {
		shutErr = errors.Join(shutErr, srv.Shutdown(shutCtx))
	}
	return shutErr
}

func newServer(addr string, h http.Handler, tlsCfg *tls.Config, cfg config.Config) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		TLSConfig:         tlsCfg,
		ReadHeaderTimeout: cfg.HTTPReadHeaderTimeout,
		ReadTimeout:       cfg.HTTPReadTimeout,
		WriteTimeout:      cfg.HTTPWriteTimeout,
		IdleTimeout:       cfg.HTTPIdleTimeout,
	}
}

func hsts(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Strict-Transport-Security", hstsValue)
		next.ServeHTTP(w, r)
	})
}

func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if id == "" || len(id) > maxRequestIDChars {
			id = newRequestID()
		}
		r.Header.Set(requestIDHeader, id)
		w.Header().Set(requestIDHeader, id)
		next.ServeHTTP(w, r)
	})
}

func newRequestID() string {
	b := make([]byte, requestIDBytes)
	_, _ = rand.Read(b) // never fails on supported platforms
	return hex.EncodeToString(b)
}

type app struct {
	lookup ports.Lookup
	log    *slog.Logger
}

func (a *app) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type artworkJSON struct {
	ID       int     `json:"id"`
	Title    string  `json:"title"`
	Artist   string  `json:"artist"`
	Date     string  `json:"date"`
	ImageID  *string `json:"image_id"`
	ImageURL *string `json:"image_url"`
}

func (a *app) search(w http.ResponseWriter, r *http.Request) {
	rid := r.Header.Get(requestIDHeader)
	q := ports.SearchQuery{
		Q:     strings.ToLower(strings.Join(strings.Fields(r.PathValue("query")), " ")),
		Limit: intParam(r, "limit", defaultLimit),
		Page:  intParam(r, "page", defaultPage),
	}
	if q.Q == "" || len(q.Q) > maxQueryLen {
		a.problem(w, rid, http.StatusBadRequest, "invalid query")
		return
	}
	res, meta, err := a.lookup.Search(r.Context(), q)
	if err != nil {
		a.log.Error("search failed", "request_id", rid, "error", err)
		a.problem(w, rid, statusFor(err), "upstream error")
		return
	}
	items := make([]artworkJSON, 0, len(res.Items))
	for _, it := range res.Items {
		items = append(items, artworkJSON{ID: it.ID, Title: it.Title, Artist: it.Artist, Date: it.Date, ImageID: nilIfEmpty(it.ImageID), ImageURL: nilIfEmpty(it.ImageURL)})
	}
	w.Header().Set(cacheHeader, string(meta.Cache))
	writeJSON(w, http.StatusOK, map[string]any{
		"data":       items,
		"pagination": map[string]int{"total": res.Total, "limit": res.Limit, "page": res.Page},
		"meta": map[string]any{
			"request_id":          rid,
			"cache":               meta.Cache,
			"upstream_latency_ms": meta.UpstreamLatency.Milliseconds(),
		},
	})
}

func (a *app) problem(w http.ResponseWriter, rid string, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "about:blank", "title": http.StatusText(status), "status": status, "detail": detail, "request_id": rid,
	})
}

func statusFor(err error) int {
	switch {
	case errors.Is(err, ports.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, ports.ErrUpstreamRateLimited):
		return http.StatusServiceUnavailable
	default:
		return http.StatusBadGateway
	}
}

func intParam(r *http.Request, key string, def int) int {
	if n, err := strconv.Atoi(r.URL.Query().Get(key)); err == nil && n > 0 {
		return n
	}
	return def
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
