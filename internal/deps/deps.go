// Package deps pins every SPEC §4 Go module in go.mod through blank imports.
// Removed by C at wiring, once the real packages import these modules.
package deps

import (
	_ "github.com/getkin/kin-openapi/openapi3filter"
	_ "github.com/hashicorp/golang-lru/v2"
	_ "github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/oapi-codegen/nethttp-middleware"
	_ "github.com/oapi-codegen/runtime"
	_ "github.com/prometheus/client_golang/prometheus/promhttp"
	_ "github.com/sony/gobreaker/v2"
	_ "github.com/stretchr/testify/require"
	_ "go.opentelemetry.io/contrib/bridges/otelslog"
	_ "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	_ "go.opentelemetry.io/otel"
	_ "go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	_ "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	_ "go.opentelemetry.io/otel/sdk/log"
	_ "go.opentelemetry.io/otel/sdk/trace"
	_ "golang.org/x/net/http2"
	_ "golang.org/x/sync/singleflight"
	_ "golang.org/x/time/rate"
)
