# Team C — final

Phases: 0 (walking skeleton, PR #1/#2), 1 (wiring, CI, acceptance, PR #11), 2 (image/policy CI, overlay in make up, PR #13). All green.
Measured: first CI run (PR #11) test 1m55s cold, build 1m02s, scan 55s; warm on main test 1m48s, build 1m02s, scan 46s; PR #13 warm test 1m00s, build 59s, scan 29s, policy 21s. Acceptance 12 checks + stop + chaos green; README gate from a clean clone passed at 17:37.
Deviations: D-009..D-011; stub healthcheck off; integration-fix tests written by coders (lead-accepted).
Would improve: SHA-pinned actions (B-001), shared limiter/Redis for replicas, B-032..B-040.
