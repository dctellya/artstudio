// Package server is the HTTP layer: the generated router behind the middleware
// chain, the error mapping to problem+json, and the operational endpoints.
package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	middleware "github.com/oapi-codegen/nethttp-middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/dctellya/artstudio/api"
	"github.com/dctellya/artstudio/internal/config"
	"github.com/dctellya/artstudio/internal/httpapi"
	"github.com/dctellya/artstudio/internal/ports"
)

// VersionInfo is the build information served by GET /version (set by ldflags).
type VersionInfo struct {
	Version, Commit, GoVersion, BuildDate string
}

// Deps are the collaborators of the server.
type Deps struct {
	Lookup   ports.Lookup
	Checkers []ports.Checker
	Registry *prometheus.Registry
	Logger   *slog.Logger
	Version  VersionInfo
}

// Server holds the HTTP handlers and listeners' settings.
type Server struct {
	cfg      config.Config
	lookup   ports.Lookup
	checkers []ports.Checker
	registry *prometheus.Registry
	log      *slog.Logger
	version  VersionInfo
	metrics  *httpMetrics
	handler  http.Handler
}

// New builds the server and registers its metrics on d.Registry.
func New(cfg config.Config, d Deps) (*Server, error) {
	if d.Lookup == nil || d.Registry == nil || d.Logger == nil {
		return nil, errors.New("server: Lookup, Registry and Logger are required")
	}
	metrics, err := newHTTPMetrics(d.Registry)
	if err != nil {
		return nil, fmt.Errorf("server: register metrics: %w", err)
	}
	doc, err := openapi3.NewLoader().LoadFromData(api.Spec)
	if err != nil {
		return nil, fmt.Errorf("server: load embedded spec: %w", err)
	}
	s := &Server{
		cfg: cfg, lookup: d.Lookup, checkers: d.Checkers, registry: d.Registry,
		log: d.Logger, version: d.Version, metrics: metrics,
	}

	mux := http.NewServeMux()
	router := httpapi.HandlerWithOptions(handlers{s}, httpapi.StdHTTPServerOptions{
		BaseRouter:  mux,
		Middlewares: []httpapi.MiddlewareFunc{matchedRoute},
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			writeProblem(w, r, http.StatusBadRequest, err.Error())
		},
	})
	filterOpts := openapi3filter.Options{}
	filterOpts.WithCustomSchemaErrorFunc(func(e *openapi3.SchemaError) string { return e.Reason })
	validate := middleware.OapiRequestValidatorWithOptions(doc, &middleware.Options{
		Options:              filterOpts,
		DoNotValidateServers: true,
		ErrorHandlerWithOpts: func(_ context.Context, err error, w http.ResponseWriter, r *http.Request, o middleware.ErrorHandlerOpts) {
			detail := http.StatusText(o.StatusCode)
			if o.StatusCode == http.StatusBadRequest {
				detail = validationDetail(err)
			}
			writeProblem(w, r, o.StatusCode, detail)
		},
	})

	// Outermost first. Recovery sits inside the request id and the observer so a
	// panic response carries the id and is logged and counted like any other.
	h := router
	h = validate(h)
	h = traced(h)
	h = s.limitClients(h)
	h = s.shed(h)
	h = s.recoverPanic(h)
	h = s.observe(h)
	h = requestID(h)
	s.handler = h
	return s, nil
}

// Handler is the public API handler.
func (s *Server) Handler() http.Handler { return s.handler }

// MetricsHandler serves /metrics for the metrics listener.
func (s *Server) MetricsHandler() http.Handler {
	return promhttp.HandlerFor(s.registry, promhttp.HandlerOpts{Registry: s.registry})
}

// HTTPServer returns an http.Server with the configured timeouts.
func (s *Server) HTTPServer(addr string, h http.Handler, tlsCfg *tls.Config) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		TLSConfig:         tlsCfg,
		ReadHeaderTimeout: s.cfg.HTTPReadHeaderTimeout,
		ReadTimeout:       s.cfg.HTTPReadTimeout,
		WriteTimeout:      s.cfg.HTTPWriteTimeout,
		IdleTimeout:       s.cfg.HTTPIdleTimeout,
	}
}

const invalidParamsDetail = "invalid request parameters"

// validationDetail describes a request validation failure as `parameter "<name>" <reason>`
// from the schema reason only: raw values, schema dumps and parser text are never echoed.
func validationDetail(err error) string {
	var re *openapi3filter.RequestError
	if !errors.As(err, &re) || re.Parameter == nil {
		return invalidParamsDetail
	}
	reason := "is invalid"
	var se *openapi3.SchemaError
	if errors.As(re.Err, &se) && se.Reason != "" {
		reason = se.Reason
	}
	return fmt.Sprintf("parameter %q %s", re.Parameter.Name, reason)
}
