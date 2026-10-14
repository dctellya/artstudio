# Team C — phase 2 report

Scope: CI `image` (multi-arch buildx, trivy CRITICAL gate, syft SBOM artifact, cosign keyless, ghcr push, anonymous pull check) and `policy` (checkov dockerfile + github_actions, `make obs-validate`) jobs; cross-compiling Dockerfile; `make up/down` with the obs overlay. Time: 17:41 → 17:47 UTC.

## Code
874aae7 jobs + Dockerfile + Makefile; adde957 review fixes (tool pins trivy v0.76.0 / syft v1.54.1 / cosign v3.1.3, trivy cache via the action, concurrency cancels only PRs, anon pull continue-on-error, policy via make obs-validate). Added pin: docker/setup-qemu-action v4.4.0 (GitHub releases, 2026-10-14).

## Review
cf11c39 (opus): 1 BLOCKING (GHCR package starts private, anonymous pull fails) — continue-on-error + human sets the package public once; 4 BACKLOG folded into the fix. Checkov 3.3.29 has no compose framework: compose not scanned (noted in ci.yml).

## Gate
PR #13: test 1m00s, build 59s, scan 29s, policy 21s (warm), actionlint 0, multi-arch buildx locally ok. First `image` run on main: see README Verify table and final report.

## Tests
No new tests (CI configuration); checked by actionlint and the runs.
