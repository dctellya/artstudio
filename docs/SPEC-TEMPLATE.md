# Spec template

The structure every SPEC.md follows. Reduced from the full template for small projects.

```
# <service> — Spec for the implementing agent team
Owner: David Carbajosa (architect, reviewer). Implementers: agent team per docs/TEAM-PROTOCOL.md. Status: v<X.Y> (<date>). Living document: §9 is the human feedback channel, §10 the as-built history.

## 1. Purpose            what it is, for whom, what it must prove, what is out of scope (explicitly)
## 2. Environment and commands   machine, runners, allowed commands, merge workflow
## 3. Phases             Walking skeleton first (on main before any team starts: module, interfaces, config, lint, build, compose, one real request end to end, acceptance suite). Then one phase per team for the MVP: the tests it turns green, its acceptance commands, its gate. Stretch phases marked as such; what is out of the box named as out
## 4. Versions           table: component, version, source, verified date
## 5. Architecture       diagram, packages and their interfaces, config keys with defaults, the HTTP contract, metrics names, data (tables), observability, runtime limits
## 6. Tests              per suite: what it covers, where it runs (local / CI), the command
## 7. CI                 jobs, what each gates, expected duration, required checks
## 8. Deliverables       README contents, docs/decisions/ (one file per decision, DECISIONS.md generated index), BACKLOG.md, reports/, docs/transcript.md, docs/agents/logs/
## 9. Changelog and human feedback   | Id | Date | Feedback | Applies | Status |   (written by the lead on main, never by a team branch)
## 10. Evolution log     one row per merged phase: what shipped, tests green, deviations   (written by the lead on main at merge, from the team's report)
```

Each decision in §5 carries a one-line reason. Numbers come from the brief unless the spec says why they changed.

