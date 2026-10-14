# Team B reviewer, phase 1

## 2026-10-14T17:03:33Z prompt received (verbatim)

You are the REVIEWER of Team B (contract + server + hardening), phase 1, per docs/TEAM-PROTOCOL.md. Read-only. Worktree: /Users/dct/code/EllyaOrg/projects/dalimaray/artstudio-wt/b (branch team-b/phase-1); diff `git diff main..HEAD` (ff6da6b tests, 62e70e9 code, 8ce5968 test fixes). Spec: /private/tmp/claude-501/-Users-dct-code-EllyaOrg-projects-dalimaray-artstudio/d212cccb-88e6-4b68-849f-d75301780e03/scratchpad/briefs/team-b.md plus docs/RULES.md. Do not read docs/assignment.md. Budget ~8 minutes.

Check against the spec and RULES: the brief's numbers (timeouts 5/10/15/60s, per-IP 10 r/s burst 20, shed 200, limit/page ranges and limit×page ≤ 1000 → 400 documented in the OpenAPI description), error mapping (404/429/502/503+Retry-After/504; vendor 403 → 502), problem+json shape and never leaking vendor payload or internal error text, X-Request-ID in/out (length bound), contract-first (handlers generated from api/openapi.yaml, request validation active, /openapi.yaml and /docs served from embedded files, no CDN in index.html), metrics names artstudio_http_*, access log fields, versions (§4; swagger-ui-dist 5.33.2), no secrets, no magic values, no test edits by the coder (62e70e9 must not touch *_test.go), spec-vs-code drift, security (per-IP limiter memory bound / eviction, RemoteAddr only, panic recovery, header injection, path traversal in /docs/{file}, unbounded bodies). Coder deviations to judge: middleware order (request id → observe → recovery → shed → limiter → otelhttp → validator → router), added GET /docs/{file}, servers url "/", Retry-After defaults from config. You may run go test, go vet, golangci-lint, vacuum in the worktree.
Report findings only, blocking and security first, with file and line; mark each BLOCKING or BACKLOG (blocking = wrong, insecure or breaks the spec's acceptance; style is BACKLOG). Change nothing except docs/agents/logs/b/reviewer-phase-1.md, where you write, every entry timestamped from `date -u +%Y-%m-%dT%H:%M:%SZ` when it happens, the prompt you received (verbatim), what you checked and your findings; commit only that file (`docs(b-1): review`, trailer `Co-Authored-By: Claude Opus <noreply@anthropic.com>`). Never amend, reset, rebase or stash; never push.
Final answer: the findings list (BLOCKING first), each with file:line, why, and the concrete fix.

## 2026-10-14T17:03:33Z checked: commit scope
`git show --stat 62e70e9`: no *_test.go touched. OK.

## 2026-10-14T17:05:30Z checked
- Read brief (team-b.md) and docs/RULES.md; read internal/server/{server,middleware,limiter,handlers,problem,metrics}.go, swaggerui/index.html, api/openapi.yaml, api/spec.go, api/oapi-codegen.yaml.
- `go vet` clean; `go test -race -count=1 ./internal/server/... ./api/...` ok (3.695s); `golangci-lint run` 0 issues; `vacuum lint -d api/openapi.yaml` 0 errors, 30 warnings, 6 informs, score 90/100.
- Regenerated internal/httpapi with oapi-codegen from api/openapi.yaml: no diff (no drift).
- swagger-ui-bundle.js contains "5.33.2"; index.html has no absolute URL (no CDN).
- Numbers: timeouts from cfg (server.go:115-118), limiter cfg.ClientRate/ClientBurst, shed cfg.MaxInFlight, buckets match SPEC, metric names artstudio_http_*; limit/page ranges and limit x page <= 1000 in OpenAPI description (openapi.yaml:29-33, 56) and enforced handlers.go:52.
- Error map handlers.go:137-154 matches SPEC; details are fixed text. Request id: printable ASCII, <=128, CR/LF rejected.
- kin-openapi v0.150.0 SchemaError.Error() appends "Schema:" JSON and "Value:" unless details are disabled (openapi3/schema_error.go:113); nothing in server.go disables it.

## 2026-10-14T17:05:30Z findings
BLOCKING
1. server.go:79 and server.go:73 - validator and router 400 detail is err.Error(): kin-openapi appends the schema dump and the raw value; router binding errors carry Go strconv text. Brief: no internal error text in problems. Fix: build openapi3filter.Options, call WithCustomSchemaErrorFunc(func(e *openapi3.SchemaError) string { return e.Reason }), pass as middleware.Options.Options; router ErrorHandlerFunc writes fixed text ("invalid request parameters"); add test asserting detail has no "Schema:"/"Value:".

BACKLOG
2. server.go:89-96 order: limiter and shed run outside otelhttp, so 429/503-shed access-log lines have empty trace_id/span_id (SPEC: every line). Accept the order (cheap rejection under load) and record in DECISIONS, or start a span earlier.
3. middleware.go:206 route label is set inside the router, so validator 400s and 404s are counted as route="unrouted". Fine for cardinality; dashboards cannot attribute 400s per route. Document or match the route in observe via the OpenAPI router.
4. limiter.go:41-70 no hard cap on buckets: entries live ~2x burst/rate (4s) so memory is bounded by distinct IPs per 4s, but IPv6 clients can rotate /128s. Fix: key IPv6 by /64 and/or cap entries (config CLIENT_LIMITER_MAX_ENTRIES), sweep is O(n) under the lock.
5. handlers.go:57 query of only whitespace passes minLength 1 and normalises to "" -> vendor search with empty q. Fix: 400 when normalise(query)=="".
6. handlers.go:151 default -> 500: a raw context.DeadlineExceeded from Lookup would be 500 not 504. Fix: also map context.DeadlineExceeded to 504.
7. handlers.go:94-95 /ready runs checkers sequentially on the request context with no per-check timeout; a hung db check holds the request until WriteTimeout. Fix: per-check timeout from config, run concurrently.
8. openapi.yaml:222-225 X-Request-ID header schema lacks maxLength 128 / pattern that the code enforces (drift).
9. server.go:110-119 MaxHeaderBytes left at Go default (1 MiB); all routes are GET with no body read, so bodies are not a risk. Optionally set MaxHeaderBytes from config.
10. Deviations judged acceptable: GET /docs/{file} (enum-restricted, embed FS cannot escape: no traversal), servers url "/", Retry-After defaults from cfg (BreakerOpenTimeout / UpstreamLimiterMaxWait) - record each in DECISIONS (RULES 8/9).
11. vacuum: 23 oas3-missing-example, 7 camel-case-properties (snake_case is intended), 6 description-duplication. Style.

No secrets, no magic values found; 62e70e9 touches no *_test.go.
