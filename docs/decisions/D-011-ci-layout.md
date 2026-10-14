# D-011 CI: three light jobs, tag-pinned actions

- **What:** `test` (vet, golangci-lint, race tests with coverage, vacuum, integration tests against a postgres service), `scan` (gitleaks dir, govulncheck, hadolint), `build` (buildx, no push, gha cache); no `needs`; read-only token; runs on push to main, tags and pull requests; generated code is committed so CI has no generate step. Actions pinned by release tag verified on 2026-10-14.
- **Why:** RULES 6 and 11: wall clock = slowest job; no installs at run time.
- **Alternatives:** SHA pinning (B-001); a single job.
- **Consequences:** phase 2 adds `image` and `policy` after the light jobs.
