# Team C coder, phase 1 part 1

2026-10-14T16:57:03Z PROMPT (verbatim summary of lead message): CODER of Team C phase 1 part 1; deliver (1) .github/workflows/ci.yml per SPEC §7 MVP, (2) dependabot.yml, (3) Makefile targets lint-api/ci/obs-validate/test-obs/chaos-up/chaos/heal/generate, (4) compose.yaml stub on :8080 + OTEL env on api, Dockerfile builds all cmd, (5) validate locally; rules: no amend/reset/rebase/stash, never push, no secrets/magic values, do not touch other teams' dirs; commit feat(c-1) with Co-Authored-By trailer; log here.
2026-10-14T16:57:03Z Created .github/workflows/ci.yml (jobs test, scan, build; pinned by tag as listed, SHA pinning to backlog; vacuum via docker image dshanley/vacuum:latest; postgres:18.6-alpine service; gitleaks dir mode with .gitleaks.toml).
2026-10-14T16:57:03Z Created .github/dependabot.yml (gomod, github-actions, docker weekly).
2026-10-14T16:57:03Z Makefile: added lint-api, generate, VACUUM var, real obs-validate/test-obs, COMPOSE_FILES adds compose.obs.yaml when present for chaos-up/chaos/heal, gitleaks passes --config, ci includes lint-api; removed not_yet helper.
2026-10-14T16:57:03Z compose.yaml: api gets OTEL_EXPORTER_OTLP_ENDPOINT (default empty) and OTEL_EXPORTER_OTLP_CERTIFICATE=/certs/ca.pem. Stub already ran /stubserver on backend; listen address :8080 is Team D's default (no env invented).
2026-10-14T16:57:03Z Validation: yaml parse ok; actionlint (docker rhysd/actionlint) clean on ci.yml; make lint test vuln gitleaks hadolint image rc=0 (0 lint issues, no vulns, no leaks); make lint-api fails: api/openapi.yaml missing (expected, Team B).
Deviations: Dockerfile unchanged (already builds ./cmd/... to /). obs-validate/test-obs depend on Team E files.
2026-10-14T17:02:23Z Lead fix: pinned dshanley/vacuum:latest -> v0.33.0 in Makefile and ci.yml; grep for :latest/@main/@master found no other unpinned refs.
2026-10-14T17:02:26Z First sed failed (BSD sed -i); commit 5d2fd0a held only the log. Reapplied the pin with sed -i ''.
