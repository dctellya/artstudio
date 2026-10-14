# Team B coder, phase 1

## 2026-10-14T16:57:49Z prompt received (summarised; the full message is in the lead's transcript)
CODER of Team B phase 1, worktree artstudio-wt/b, branch team-b/phase-1. Make the tester's tests (internal/server/*_test.go) pass with minimum production code: api/openapi.yaml (3.0.3, vacuum clean), api/oapi-codegen.yaml + generated internal/httpapi, internal/server (middleware chain, problem+json, health/ready/version/openapi/docs, metrics, access log), embedded swagger-ui-dist 5.33.2. No test edits, no new modules, never amend/reset/rebase/stash, never push. Commit feat(b-1) with Co-Authored-By trailer. Run vet, golangci-lint, go test -race, vacuum. Deadline green 17:25Z.

## 2026-10-14T16:59:16Z swagger-ui
Fetched swagger-ui-dist-5.33.2.tgz from registry.npmjs.org; kept swagger-ui.css, swagger-ui-bundle.js, swagger-ui-standalone-preset.js in internal/server/swaggerui/; wrote index.html (assets under /docs/, spec at /openapi.yaml, no CDN).

## 2026-10-14T17:01:20Z build
Wrote api/openapi.yaml, api/oapi-codegen.yaml, api/spec.go (embed), internal/httpapi/{doc.go,api.gen.go} (go tool oapi-codegen), internal/server/{server,middleware,handlers,problem,limiter,metrics}.go. First oapi-codegen run wrote outside the worktree (output path relative to cwd); removed the stray ../internal and fixed the config path. vacuum: 0 errors, 30 warnings (examples, snake_case required by the contract).
Test result: all pass except TestErrorMapping/retry-after_on_rate_limited (2 subtests): error is struct{ error }{retryErr{3s}}, whose method set is only Error(); RetryAfter() is unreachable without reflection. Not changed; reported to the lead.

## Deviations
- Middleware order: request id -> observe (metrics + access log) -> panic recovery -> shed -> limiter -> otelhttp -> validator -> router, so a panic response carries the request id and is logged.
- /docs/{file} added to the contract (assets must be routable through the request validator).
- spec servers: url "/" (an absolute server URL made the tests' legacy router reject test requests).
- Retry-After default when the error has none: BreakerOpenTimeout for breaker-open, UpstreamLimiterMaxWait otherwise, 1s for shedding.
- golangci-lint reports G101 in the tester's server_test.go:68 (const secret); not touched.
