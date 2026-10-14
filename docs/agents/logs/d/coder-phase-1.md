# D coder, phase 1

## 2026-10-14T16:58:14Z prompt received (verbatim summary of the lead message)
CODER of Team D phase 1: make the tester's tests (internal/audit, internal/tlsconf, internal/artic/stubserver) pass with minimum production code per spec excerpt and docs/RULES.md; no test edits; never amend/reset/rebase/stash, never push; no new modules; numbers from config.Config; build audit Writer/pgx Sink/migration/pool metrics/Checker/span/Noop, tlsconf Server/Client/HSTS, stubserver + cmd/stubserver (fixtures captured from the real API, ctl subcommand, server timeouts); run vet, golangci-lint, unit and integration tests with throwaway postgres 18.6-alpine; commit as feat(d-1) with trailer; log here.

## 2026-10-14T16:59:30Z fixtures
Captured from api.artic.edu with the AIC-User-Agent header: search.json (q=monet limit=12 page=1), artwork-27992.json, artwork-111628.json, probe.json (/artworks?limit=1&fields=id). Real 404 body for an unknown id copied into the stub.

## 2026-10-14T17:00:00Z code
Wrote internal/audit/{audit,sink,poolmetrics}.go + migrations/001_vendor_calls.sql, internal/tlsconf/tlsconf.go, internal/artic/stubserver/stubserver.go, cmd/stubserver/main.go.
First integration run: TestBatchInsertLandsRows failed because the test calls w.Check after w.Close; Close therefore no longer closes the pool, new method Writer.ClosePool() does (deviation below).
Lint: three gosec taint findings in production code got //nolint with reasons; one G402 remains in tlsconf_test.go:91 (test deliberately dials with MaxVersion TLS1.2).

## 2026-10-14T17:00:33Z final runs
go vet ./... exit 0. golangci-lint: 1 issue (gosec G402, tlsconf_test.go:91, a test file). Unit: 3 packages ok, 21 tests pass. Integration (postgres:18.6-alpine, stopped after): 10 pass in internal/audit. Output saved by the lead's request in the final message.

## Deviations
1. Writer.Close does not close the pgx pool; Writer.ClosePool() does (test calls Check after Close). C's wiring must call ClosePool after Close.
2. golangci-lint G402 in a test file I may not edit: needs an exclusion for _test.go in .golangci.yml (owned by C) or a tester fix.
3. Stub "slow" default 2500ms (spec: just over the 2s attempt timeout; test requires 2s..4s), a constant in the stub rather than config.
