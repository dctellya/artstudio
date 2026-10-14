# Rules

The rules every system I build inherits. Reduced from the full set for small projects.

1. **Tests before code.** No production code without a failing test that asks for it. Tests are the contract between the spec and the build.
2. **A walking skeleton first.** The thinnest end-to-end slice (one real request through the real stack, health, build, compose) is on the default branch before any team deepens its area, so every merge is checked by real acceptance tests and whatever the clock stops on runs.
3. **Versions verified, latest stable.** Every dependency and image is pinned to a version checked against the official source on the day it is added; the spec's version table says when.
4. **No secrets, no magic values.** Secrets only from the environment, never in the repo or the logs; every number (timeout, TTL, threshold) comes from configuration with a stated default and a reason in the spec.
5. **Everything from a clean clone.** `make test` and `docker compose up --build` work on a machine that has only Go and Docker; the README's commands are the ones CI runs.
6. **CI is a gate, not infrastructure.** It builds once, runs the gates in parallel, scans, signs; it never deploys. Under five minutes.
7. **Counts come off the tool.** Test counts, coverage, timings and scan results are pasted from the run that produced them, never estimated.
8. **The spec is as-built.** When the code and the spec disagree at the end of a phase, the spec is updated in the same pull request, with the reason in `DECISIONS.md`.
9. **Deviations are the human's to approve.** The team proposes, with reason and cost, and waits.
10. **Honest time.** The README states the real time spent; what was left out is listed, not hidden.
11. **CI under two minutes warm.** Wall-clock equals the slowest job: independent light jobs in parallel (test, scan, build) with no `needs`; caches on everything (Go modules and build, linter, buildx, scanner databases); prebuilt actions, never installs at run time; no runtime a gate does not need (vacuum, not spectral; checkov only in the heavy stage); the image built incrementally with cache mounts and without push in the light stage; unit and contract tests start no container; integration behind a tag. The heavy stage (publish, image scan, SBOM, signature, config policy) runs after the light gates. Measured per run and reported.
12. **Git forward only.** Never amend, reset, rebase or stash; a mistake is fixed by a new commit. Nothing is pushed to the default branch except by merging a pull request.
13. **One file per decision.** `docs/decisions/D-NNN-slug.md` (what, why, alternatives, consequences); the index is generated. Two branches never edit the same shared file; the living spec sections are written by the lead on the default branch at merge time.
14. **Review fixes what blocks.** Blocking and security findings are fixed in the phase, once; everything else goes to the backlog with its reason. A review never starts a second tester-coder round for style.
15. **Nothing reaches an agent mid-task.** Decisions are settled before an agent starts; what comes up later waits for its next phase or goes to the backlog.
16. **Deliverables grow with the work.** README, time log, transcript and reports start with the skeleton and are extended at every merge, never written at the end.
