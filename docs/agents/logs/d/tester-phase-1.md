# D tester, phase 1

## 2026-10-14T16:55:35Z prompt received (verbatim, abbreviated of nothing material)

You are the TESTER of Team D (audit log, TLS config, vendor stub), phase 1, per docs/TEAM-PROTOCOL.md. Worktree: artstudio-wt/d (branch team-d/phase-1). Spec: briefs/team-d.md plus internal/ports/*.go and internal/config/config.go. Write exactly the SPEC §6 tests for rows audit, tlsconf, stub. Postgres tests under build tag integration reading TEST_DATABASE_URL (skip if unset); the rest unit. Signatures given: audit.New/Migrate/NewWithSink/Sink/Noop, tlsconf.Server/Client/HSTS, stubserver.New/WithSlow. Never amend/reset/rebase/stash, never push, no production code. Commit as test(d-1): with trailer. Run the go test -race command, paste red output, end with TEST BRIEF. Log here.

## 2026-10-14T16:56:53Z actions
Read brief, ports, config. Wrote internal/audit/audit_test.go (unit), internal/audit/audit_integration_test.go (tag integration), internal/tlsconf/tlsconf_test.go, internal/artic/stubserver/stubserver_test.go.
Red: go test -race fails with "no non-test Go files" / build failed for all three packages (external _test packages, so no undefined: lines appear).
Sanity: with throwaway stubs (deleted, never committed) go vet passes for all three plus -tags integration, and tlsconf tests pass against a 20-line reference impl.

## 2026-10-14T16:57:23Z TEST BRIEF
Unit command: go test -race ./internal/audit/... ./internal/tlsconf/... ./internal/artic/stubserver/...
Integration: TEST_DATABASE_URL=postgres://... go test -race -tags integration ./internal/audit/...
audit unit: TestRecordNeverBlocks (10000 Records with stalled sink return <2s), TestQueueFullIncrementsDropped (queue 5, batch 1, 50 records: artstudio_audit_dropped_total >= 40), TestFlushBySize, TestFlushByInterval, TestCloseFlushes (5 rows reach sink after Close), TestWriterMetricsAndChecker (Name "db", queue_depth/writes_total/write_duration registered after a flush), TestNoopAuditor.
audit integration: TestMigrateIdempotent, TestBatchInsertLandsRows (25 rows via CopyFrom, w.Check OK), TestPoolMetricsRegistered (artstudio_db_pool_{total,idle,acquired,max}_conns, _acquire_total, _acquire_duration_seconds_total).
tlsconf: TestServerMinTLS13, TestServerBadFiles, TestClientTrustsOnlyGivenCA (RootCAs has exactly 1 subject, no InsecureSkipVerify, other-CA server rejected with UnknownAuthorityError), TestClientBadCAFile, TestHSTSHeader.
stub: TestServesSearchFixture, TestServesArtworkFixtureAndUnknown404, TestProbeEndpoint, TestCtl429SendsRetryAfter, TestCtl503, TestCtlSlowConfigurableDelay, TestCtlSlowDefaultJustOverTwoSeconds, TestCtlTimeoutHangsUntilClientGivesUp, TestCtlClearRestoresService.
Required API: see message to lead.

## 2026-10-14T17:00:57Z lint fix
Lead request: added //nolint:gosec on the deliberate TLS 1.2 dial (tlsconf_test.go:91). golangci-lint on ./internal/tlsconf/...: see commit; tlsconf tests run.
