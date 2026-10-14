# C reviewer, phase 1

## 2026-10-14T17:24:50Z prompt received (verbatim)

> You are the REVIEWER of Team C (delivery: wiring, Dockerfile, compose, Makefile, CI), phase 1, per docs/TEAM-PROTOCOL.md. Read-only. Worktree: /Users/dct/code/EllyaOrg/projects/dalimaray/artstudio-wt/c (branch team-c/phase-1). Diff of Team C's own work: `git diff main...HEAD` (C commits: e7b385c, 5d2fd0a, b264620 CI prep; 1372bbc wiring; c40a8f8; 1803020; 57f7168; tester ac4961d merged via 0d6008c). Spec: SPEC.md §2, §3, §5, §7 and docs/decisions/ in the worktree, plus docs/RULES.md. Do not read docs/assignment.md. Budget ~8 minutes. Known, being fixed elsewhere (not findings): X-Cache missing on error responses (Team B fix), audit_writes_total absent until first write (Team D fix).
>
> Check: cmd/artstudio wiring correctness (all components constructed from config; Noop auditor when DATABASE_URL empty; ClosePool after Close; prober started and stopped; both listeners TLS 1.3 via tlsconf with HSTS on the API; no plaintext listener; metrics on its own listener; graceful shutdown order and timeout from config; OTLP exporters use tlsconf.Client with the CA, never insecure; secrets never logged — check config logging and errors; healthcheck subcommand); Dockerfile (multi-stage, CGO_ENABLED=0, distroless static nonroot, cache mounts, OCI source label, no platform pinned, ldflags); compose.yaml (only 127.0.0.1:8443 and :9090 published, never 0.0.0.0; read_only, cap_drop ALL, no-new-privileges; postgres and stub not published; healthchecks; no secrets committed — .env.example placeholders only); Makefile (DOCKER_DEFAULT_PLATFORM= prefix on docker calls; pinned images; ci target = same commands as CI); .github/workflows/ci.yml (3 jobs test/scan/build, no needs, permissions read-only, pinned action versions per §4, no `go install`, caches, postgres service for integration tests, will it pass on the first run on ubuntu-latest: e.g. vacuum docker path, gitleaks dir mode with .gitleaks.toml, govulncheck-action go-version-file, hadolint-action; generated code committed so no generate step needed); dependabot; versions §4; magic values; RULES 11 timing expectations. You may run go vet, golangci-lint, go test, actionlint via `docker run --rm -v "$PWD:/repo" -w /repo rhysd/actionlint:1.7.7` if available, `docker compose config -q`.
> Report findings only, blocking and security first, with file and line; mark each BLOCKING or BACKLOG (blocking = wrong, insecure, breaks acceptance, or would make the first CI run red). Change nothing except docs/agents/logs/c/reviewer-phase-1.md, where you write, every entry timestamped from `date -u +%Y-%m-%dT%H:%M:%SZ` when it happens, the prompt you received (verbatim), what you checked and your findings; commit only that file (`docs(c-1): review`, trailer `Co-Authored-By: Claude Opus <noreply@anthropic.com>`). Never amend, reset, rebase or stash; never push.
> Final answer: findings (BLOCKING first) with file:line, why, concrete fix.

## 2026-10-14T17:28:12Z what was checked

Read cmd/artstudio/{main,telemetry}.go, Dockerfile, compose.yaml, Makefile, .github/workflows/ci.yml, .github/dependabot.yml, .env.example, .gitleaks.toml, .dockerignore, internal/config (LogValue redaction), internal/tlsconf, server.HTTPServer, audit.New, otlptracehttp v1.47.0 `WithEndpointURL` source; SPEC §2-§7, RULES, D-001, D-002.

Ran (output from the tools):
- `go vet ./...`: clean.
- `golangci-lint run` (v2.14.0, same as CI): **3 issues, gosec G204** in test/acceptance/acceptance_test.go:119, :341, :353 (config has `build-tags: [acceptance, integration]`).
- `go test -race ./...`: all ok.
- `govulncheck ./...`: No vulnerabilities found.
- actionlint 1.7.7: rc 0, no findings.
- `POSTGRES_PASSWORD=x docker compose config -q`: ok.
- `docker manifest inspect` vacuum v0.33.0, gitleaks v8.30.1, hadolint v2.15.1, postgres:18.6-alpine, golang:1.27.2-alpine: all exist.
- hadolint v2.15.1 on Dockerfile: rc 0. gitleaks dir (CI command): no leaks. vacuum lint (CI command): rc 0, 0 errors, 30 warnings.
- oapi-codegen regenerated to scratch: identical to committed internal/httpapi/api.gen.go.
- `.env` git-ignored; only `.env.example` tracked, placeholders only; no certs tracked.

Confirmed OK: Noop auditor on empty DATABASE_URL; Close then ClosePool (D-002); prober started and stopped and awaited; both listeners ListenAndServeTLS with tlsconf.Server, HSTS on API handler only; metrics on own listener; shutdown order servers → prober → audit Close → ClosePool → telemetry flush, timeout from SHUTDOWN_TIMEOUT; config logged via redacting LogValue; healthcheck subcommand trusts CA, SAN localhost present; Dockerfile multi-stage, CGO_ENABLED=0, distroless nonroot, cache mounts, OCI source label, no platform, ldflags; compose publishes only 127.0.0.1:8443/9090, postgres and stub unpublished, hardening on api and stub; Makefile docker calls prefixed; CI 3 jobs, no needs, contents: read, §4 action versions, pg service, no go install.

## 2026-10-14T17:28:12Z findings

### BLOCKING

1. **CI test job red on first run: golangci-lint gosec G204** at test/acceptance/acceptance_test.go:119, :341, :353 (`exec.Command("docker", ...)` with variable args). `.golangci.yml:4-6` lints the `acceptance` tag, so `golangci-lint-action` (ci.yml:40) fails and `make lint` / `make ci` fail. Fix: add `//nolint:gosec // test-only docker CLI, fixed binary` on the three lines (or a gosec G204 exclusion for `test/acceptance/` in .golangci.yml).
2. **OTLP exporters post to `/`, not `/v1/traces` and `/v1/logs`** (cmd/artstudio/telemetry.go:37 and :43). In otlptracehttp v1.47.0 `WithEndpointURL("https://alloy:4318")` sets URLPath to "/" when the URL has no path, so the compose value from §5 makes Alloy 404 every export; traces and logs share one URL. Off in phase 1 (endpoint empty), but it breaks phase 2 and the obs live round trip. Fix: use `WithEndpoint(u.Host)` + default paths, or `WithEndpointURL(base + "/v1/traces")` and `base + "/v1/logs"`.
3. **OTLP may export in plaintext**: same option sets `Insecure = scheme != "https"`, and internal/config/config.go:137 does not validate the scheme, so `OTEL_EXPORTER_OTLP_ENDPOINT=http://...` silently exports without TLS (brief: TLS on every edge, OTLP https). Fix: in config validate, reject a non-empty OTLP endpoint whose scheme is not https.
4. **/ready lacks `config` and (with audit off) `db` checks** vs §5 contract `checks:{config,db,cache,vendor}` (cmd/artstudio/main.go:167-170). Fix: add a static `config` checker (OK, config validated at start) and, when DATABASE_URL is empty, a `db` checker returning OK with `{"enabled": false}`.

### BACKLOG

5. compose.yaml:4 api has no `stop_grace_period`; Docker's 10s default equals SHUTDOWN_TIMEOUT 10s (main.go:202), and audit Close + telemetry flush share that same deadline, so a slow drain gets SIGKILLed before the audit flush. Fix: `stop_grace_period: 15s` and give audit/telemetry their own budget after the HTTP drain.
6. compose.yaml:15 `env_file: .env` injects POSTGRES_PASSWORD and GRAFANA_ADMIN_PASSWORD into the api container, which only needs CONTACT_EMAIL (+ DATABASE_URL). Fix: pass `CONTACT_EMAIL` via `environment:` and drop env_file.
7. compose.yaml:17,46 `${POSTGRES_PASSWORD}` without `:?`: an empty value yields a broken DATABASE_URL and a postgres that refuses to start with an unclear error. Fix: `${POSTGRES_PASSWORD:?set in .env}`.
8. Makefile:71 `ci` differs from ci.yml: no `-cover` (Makefile:26 vs ci.yml:43), no integration step (ci.yml:46-47), hadolint-action uses its own bundled hadolint, not v2.15.1. §7 says same commands. Fix: `test` with `-cover`, add `test-integration` to `ci`, scope integration to `./internal/...` in both.
9. ci.yml:47 `-tags integration ./internal/...` reruns every race unit test a second time (RULES 11 time). Fix: `-run Integration` or list only packages with integration files.
10. ci.yml:45 vacuum image pulled uncached each run (RULES 11 caches). Acceptable; note in timing report.
11. ci.yml:67-72 build job passes no VERSION/COMMIT/BUILD_DATE build args, so CI image reports `dev`. Fix: `build-args` from `github.sha`.
12. Dockerfile:10,14 builds `./cmd/...`, so the production image ships `/stubserver`. Deliberate for compose stub, but a separate stage/target is cleaner for P2 publish.
13. Dockerfile:1 no `--platform=$BUILDPLATFORM` + `TARGETOS/TARGETARCH`; P2 multi-arch will build under emulation (slow). Fix in P2.
14. ci.yml:3-5 `push` + `pull_request` without branch filter runs everything twice per PR (different refs, concurrency does not dedupe). Fix: `push: branches: [main]` + tags.
15. main.go:37-40 `defaultListen`/`defaultCAFile` duplicate config defaults; `healthcheckTO` 3s is unconfigured. Minor magic values; healthcheck could reuse config defaults without requiring CONTACT_EMAIL.
16. test/acceptance/acceptance_test.go:119 calls `docker` without `DOCKER_DEFAULT_PLATFORM=`; harmless for stop/start/logs/inspect, inconsistent with §2.
