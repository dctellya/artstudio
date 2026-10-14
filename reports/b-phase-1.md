# Team B — phase 1 report

Scope: api/ (contract), internal/httpapi (generated), internal/server. Time: 16:55 → 17:11 UTC (16 min).

## Tests written (red)
ff6da6b: contract (400s incl. limit=50&page=21, boundaries, response validation vs schema) and server unit (error map 11 cases, no vendor body, request id, per-IP 429, shed, panic, timeouts, health, ready, version/docs/openapi, meta headers, access log). Red: `undefined: server.Server/Deps/VersionInfo/New`. 8ce5968: unsatisfiable RetryAfter case fixed, G101 annotation. ea2d96b: 400 detail leaks nothing (red: "should not contain Schema:").

## Code added
62e70e9: openapi.yaml, oapi-codegen config + generated code, server, middleware chain, problem+json, limiter, metrics, embedded Swagger UI 5.33.2. 1a8e444: sanitised 400 details.

## Review
bf0b900 (opus): 1 BLOCKING (400 details leaked schema/raw value) — fixed once. 10 BACKLOG → B-009..B-016.

## Gate (lead, 2026-10-14T17:11:25Z)
```
ok  internal/server  3.910s
go vet clean; golangci-lint 0 issues; vacuum: Passed with 0 errors, 30 warnings and 6 informs
after merging main: all 8 packages ok
```

## Deviations
Middleware order, Retry-After defaults (D-008); servers "/", /docs/{file} (D-007).

## Left red
None.
