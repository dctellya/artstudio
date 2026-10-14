# Team C tester, phase 1

## 2026-10-14T17:12:33Z prompt received (verbatim summary of key points)
Tester of Team C phase 1, worktree artstudio-wt/c-acc, only test/acceptance/**. Make every SPEC §6 acceptance bullet a real assertion (no notYet); chaos test gated by ACCEPTANCE_CHAOS=1; graceful-stop test gated by ACCEPTANCE_STOP=1; commit "test(c-1): real acceptance checks"; end with TEST BRIEF. (Full prompt text is in the lead's message; not re-copied here.)

## 2026-10-14T17:12:40Z read SPEC §5/§6, skeleton test, compose.yaml (postgres user/db artstudio/artstudio)
## 2026-10-14T17:13:54Z rewrote acceptance_test.go; go vet -tags acceptance OK; ran against skeleton stack with CA_FILE pointing at main checkout: red as expected (limit=51 returned 200, /ready 404, metrics families missing, no vendor_calls, no rate limit). compose-based tests failed in the worktree only because .env is absent there (run from a checkout with .env).

## TEST BRIEF
See final message of the tester; summary:
- TestSearchMissThenHit: fresh random query -> X-Cache miss then hit; body has pagination, meta.request_id.
- TestArtworkWithImageURL: /artworks/27992 200, image_url ends /full/843,/0/default.jpg.
- TestNotFoundProblemAndNegativeCache: random high id 404 problem+json, request_id == X-Request-ID, repeat -> X-Cache negative.
- TestLimitOutOfRange: limit=51 and limit=50&page=21 -> 400 problem+json.
- TestPerIPRateLimit: 120 concurrent /health -> some 429 problem+json with Retry-After; sleeps 3s after.
- TestReadyVendorNeverBlocks: /ready 200, checks vendor, db, cache present.
- TestMetricsOverTLS: all frozen families on https :9090.
- TestAuditRowsInPostgres: psql via compose exec, count(vendor_calls) > 0 within 20s.
- TestHealthcheckSubcommand: compose exec api /artstudio healthcheck.
- TestChaosBreakerOpenThenStale (ACCEPTANCE_CHAOS=1), TestGracefulStopExitsZero (ACCEPTANCE_STOP=1).
- Production requirements: chaos-up must set CACHE_TTL_SEARCH=1s; logs must contain JSON msg "shutdown" or "draining" and msg "audit flushed"; /ready vendor detail key "breaker" = "open"; stub binary /stubserver ctl 503|clear; api service healthcheck status reported by docker inspect.

## 2026-10-14T17:29:53Z follow-up: tests for review findings 2-4
Merged main. Added TestOTLPEndpointHTTPSOnly (internal/config), cmd/artstudio/telemetry_test.go (TestSetupTelemetryOTLPPaths, drives setupTelemetry(ctx, config.Config) from team-c/phase-1; red there: server saw only "/" instead of /v1/logs and /v1/traces), and checks.config required in TestReadyVendorNeverBlocks. telemetry.go is not on this branch so the package test only compiles once the coder branch is merged.
