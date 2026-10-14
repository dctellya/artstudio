package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/dctellya/artstudio/internal/config"
	"github.com/dctellya/artstudio/internal/tlsconf"
)

const (
	serviceNameKey = "service.name"
	tracesPath     = "/v1/traces"
	logsPath       = "/v1/logs"
)

// setupTelemetry wires traces and logs over OTLP/HTTPS when an endpoint is configured.
// It returns the extra slog handler (nil when off) and a flush function.
func setupTelemetry(ctx context.Context, cfg config.Config) (slog.Handler, func(context.Context) error, error) {
	if cfg.OTLPEndpoint == "" {
		return nil, func(context.Context) error { return nil }, nil
	}
	tlsCfg, err := tlsconf.Client(cfg.OTLPCertificate)
	if err != nil {
		return nil, nil, fmt.Errorf("otlp tls: %w", err)
	}
	base := strings.TrimRight(cfg.OTLPEndpoint, "/")
	res := resource.NewSchemaless(attribute.String(serviceNameKey, cfg.OTelServiceName))

	traceExp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(base+tracesPath), otlptracehttp.WithTLSClientConfig(tlsCfg))
	if err != nil {
		return nil, nil, fmt.Errorf("otlp trace exporter: %w", err)
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(traceExp), sdktrace.WithResource(res))

	logExp, err := otlploghttp.New(ctx, otlploghttp.WithEndpointURL(base+logsPath), otlploghttp.WithTLSClientConfig(tlsCfg))
	if err != nil {
		return nil, nil, errors.Join(fmt.Errorf("otlp log exporter: %w", err), tp.Shutdown(ctx))
	}
	lp := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)), sdklog.WithResource(res))

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	flush := func(ctx context.Context) error {
		return errors.Join(tp.Shutdown(ctx), lp.Shutdown(ctx))
	}
	return otelslog.NewHandler(cfg.OTelServiceName, otelslog.WithLoggerProvider(lp)), flush, nil
}
