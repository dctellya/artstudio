# Team C — phase 1 report

Scope: walking skeleton (phase 0), CI prep, wiring of cmd/, Dockerfile, compose, Makefile, acceptance suite. Time: 16:46 → 17:33 UTC.

## Tests written (red)
Skeleton acceptance suite (9d0afea, coder-written as the skeleton step defines). ac4961d (tester): every §6 acceptance bullet real; chaos and stop behind ACCEPTANCE_CHAOS / ACCEPTANCE_STOP. f353999: OTLP https-only, OTLP paths, ready config check (red: nil error; server saw "/").

## Code added
e7b385c/b264620 CI workflow, dependabot, Makefile targets, vacuum pinned; 1372bbc wiring (config, telemetry, registry, audit/Noop, cache, client, prober, lookup, server, TLS listeners, graceful shutdown); c40a8f8 drain log; 1803020 build date; 57f7168 chaos-up; 45cd701 G204 exclusion for acceptance docker calls; 69bd6a8 OTLP paths + https-only, ready config/db, stop_grace_period, CI triggers and integration-only step.

## Integration findings at wiring (fixed in the owning team, PRs)
#7 A: metric families exported from start; #9 B: X-Cache on error responses; #10 D: audit_writes_total from start.

## Review
73e1800 (opus): 4 BLOCKING (lint G204 would fail first CI; OTLP path "/"; OTLP http allowed; /ready missing config/db) — fixed once. 12 BACKLOG → B-032..B-040 (3 cheap ones folded into the fix).

## Gate (lead, 2026-10-14T17:33:13Z)
```
go test -race -count=1 ./...  → 9 packages ok (cmd/artstudio, artic, stubserver, audit, cache, config, lookup, server, tlsconf)
integration (-run pattern, postgres:18.6-alpine) → TestVendorFault…, TestMigrateIdempotent, TestBatchInsertLandsRows, TestPoolMetricsRegistered, TestBadRowDoesNotLoseBatch PASS
make test-acceptance → ok 8.005s (12 PASS, chaos/stop skipped by flag)
make test-acceptance-stop → TestGracefulStopExitsZero PASS 9.70s (coder run)
make chaos-up && make test-acceptance-chaos → TestChaosBreakerOpenThenStale PASS 10.76s (coder run)
make ci → 0 (0 lint issues, vacuum 0 errors, no vulnerabilities, no leaks, hadolint clean, image built)
```

## Deviations
Decisions D-009 (OTLP), D-010 (chaos profile), D-011 (CI). Stub healthcheck disabled. Integration-fix regression tests written by coders (lead-accepted under the box).

## Left red
None.
