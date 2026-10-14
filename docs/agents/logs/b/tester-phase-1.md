# Team B tester, phase 1

## 2026-10-14T16:57:08Z prompt received (summarised faithfully; first entry at 2026-10-14T16:55:35Z)
Role: TESTER of Team B phase 1 (worktree artstudio-wt/b, branch team-b/phase-1). Spec: scratchpad briefs/team-b.md + internal/ports + internal/config. Write exactly SPEC §6 rows "contract" and "server unit" for Team B, in internal/server (package server_test), with the exact API: server.Deps{Lookup, Checkers, Registry, Logger, Version}, server.VersionInfo{Version, Commit, GoVersion, BuildDate}, server.New(cfg, d) (*Server, error), Handler(), MetricsHandler(), HTTPServer(addr, h, tlsCfg). Fake ports.Lookup; access log msg "request" with request_id + trace_id. No production code, no build/config edits, no new deps, never amend/reset/rebase/stash, never push. Commit `test(b-1): ...` with Co-Authored-By trailer. Run `go test -race ./internal/server/...`, paste red output; end with TEST BRIEF; log in this file.

## 2026-10-14T16:57:09Z actions
- Read brief, internal/ports, internal/config, docs/TEAM-PROTOCOL.md.
- Created internal/server/{helpers_test.go,contract_test.go,server_test.go,pkg_test.go} (pkg_test.go is an empty `package server` so the package builds and fails on undefined symbols instead of "no non-test Go files").
- `go test -race ./internal/server/...` result: build failed, red as expected:
  undefined: server.Server, server.Deps, server.VersionInfo, server.New

## 2026-10-14T16:57:09Z TEST BRIEF
(see final message of the tester; summary below)
Command: go test -race ./internal/server/...   (contract suite also reads ../../api/openapi.yaml)
Contract: TestContract_InvalidParamsRejectedWith400Problem (limit 0/51, page 0/51, limit=50&page=21, limit=abc, id 0/-3/abc -> 400 problem+json, lookup not called); TestContract_BoundaryParamsAccepted (limit*page=1000 ok); TestContract_ResponsesValidateAgainstSchema (openapi3filter.ValidateResponse for 200/404/503/502/504/400, health, ready, version; spec itself must validate).
Server unit: TestErrorMapping, TestProblemNeverContainsVendorBody, TestRequestID, TestPerIPRateLimit429, TestLoadShedAtMaxInFlight, TestPanicBecomes500Problem, TestHTTPServerTimeoutsFromConfig, TestHealth, TestReady, TestVersion, TestDocsAndOpenAPIServed, TestMetaAndCacheHeaders, TestAccessLogLine.
