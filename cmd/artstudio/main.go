// Command artstudio is the Art Institute of Chicago gateway.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"

	"github.com/dctellya/artstudio/internal/artic"
	"github.com/dctellya/artstudio/internal/audit"
	"github.com/dctellya/artstudio/internal/cache"
	"github.com/dctellya/artstudio/internal/config"
	"github.com/dctellya/artstudio/internal/lookup"
	"github.com/dctellya/artstudio/internal/ports"
	"github.com/dctellya/artstudio/internal/server"
	"github.com/dctellya/artstudio/internal/tlsconf"
)

// Set by -ldflags "-X main.version=... -X main.commit=...".
var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

const (
	defaultListen = ":8443"
	defaultCAFile = "/certs/ca.pem"
	healthcheckTO = 3 * time.Second
	exitFailure   = 1
	listenerCount = 2
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
	tlsCfg, err := tlsconf.Client(envOr("TLS_CA_FILE", defaultCAFile))
	if err != nil {
		return err
	}
	_, port, err := net.SplitHostPort(envOr("LISTEN_ADDR", defaultListen))
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: healthcheckTO, Transport: &http.Transport{TLSClientConfig: tlsCfg}}
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

func newLogger(cfg config.Config, extra slog.Handler) (*slog.Logger, error) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		return nil, fmt.Errorf("LOG_LEVEL: %w", err)
	}
	var h slog.Handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	if extra != nil {
		h = slog.NewMultiHandler(h, extra)
	}
	return slog.New(h), nil
}

func newRegistry() *prometheus.Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	buildInfo := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "artstudio", Name: "build_info", Help: "Build information.",
		ConstLabels: prometheus.Labels{"version": version, "commit": commit, "go_version": runtime.Version()},
	})
	buildInfo.Set(1)
	reg.MustRegister(buildInfo)
	return reg
}

// newAuditor returns the Postgres writer (with its readiness checker) or a noop when DATABASE_URL is empty.
func newAuditor(ctx context.Context, cfg config.Config, reg prometheus.Registerer) (ports.Auditor, *audit.Writer, error) {
	if cfg.DatabaseURL == "" {
		return audit.Noop(), nil, nil
	}
	w, err := audit.New(ctx, cfg, reg)
	if err != nil {
		return nil, nil, fmt.Errorf("audit: %w", err)
	}
	return w, w, nil
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	extra, flushTelemetry, err := setupTelemetry(ctx, cfg)
	if err != nil {
		return err
	}
	log, err := newLogger(cfg, extra)
	if err != nil {
		return err
	}
	slog.SetDefault(log)
	log.Info("starting", "version", version, "commit", commit, "config", cfg)

	tlsCfg, err := tlsconf.Server(cfg.TLSCertFile, cfg.TLSKeyFile)
	if err != nil {
		return err
	}

	reg := newRegistry()
	auditor, writer, err := newAuditor(ctx, cfg, reg)
	if err != nil {
		return err
	}
	store := cache.New(cfg, reg)
	vendor, err := artic.New(cfg, reg)
	if err != nil {
		return fmt.Errorf("artic: %w", err)
	}
	prober := artic.NewProber(vendor, cfg, reg)
	proberCtx, stopProber := context.WithCancel(ctx)
	defer stopProber()
	proberDone := make(chan struct{})
	go func() {
		defer close(proberDone)
		prober.Run(proberCtx)
	}()

	checkers := []ports.Checker{store.Checker(), prober}
	if writer != nil {
		checkers = append([]ports.Checker{writer}, checkers...)
	}
	srv, err := server.New(cfg, server.Deps{
		Lookup:   lookup.New(vendor, store, auditor, cfg, reg),
		Checkers: checkers,
		Registry: reg,
		Logger:   log,
		Version:  server.VersionInfo{Version: version, Commit: commit, GoVersion: runtime.Version(), BuildDate: buildDate},
	})
	if err != nil {
		return err
	}
	api := srv.HTTPServer(cfg.ListenAddr, tlsconf.HSTS(srv.Handler()), tlsCfg)
	metrics := srv.HTTPServer(cfg.MetricsAddr, srv.MetricsHandler(), tlsCfg)
	servers := []*http.Server{api, metrics}

	errs := make(chan error, listenerCount)
	for _, s := range servers {
		go func() {
			log.Info("listening", "addr", s.Addr)
			// Certificates come from tlsCfg.
			if err := s.ListenAndServeTLS("", ""); !errors.Is(err, http.ErrServerClosed) {
				errs <- err
			}
		}()
	}

	var runErr error
	select {
	case runErr = <-errs:
	case <-ctx.Done():
	}
	log.Info("shutdown: draining")
	shutCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	for _, s := range servers {
		runErr = errors.Join(runErr, s.Shutdown(shutCtx))
	}
	stopProber()
	<-proberDone
	runErr = errors.Join(runErr, auditor.Close(shutCtx))
	if writer != nil {
		writer.ClosePool()
	}
	log.Info("audit flushed")
	runErr = errors.Join(runErr, flushTelemetry(shutCtx))
	log.Info("telemetry flushed")
	return runErr
}
