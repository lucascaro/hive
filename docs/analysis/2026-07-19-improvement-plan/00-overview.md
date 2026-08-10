# Hive improvement plan — 2026-07-19

Repo-wide analysis (Go backend, frontend/tests, docs/process) at v2.3.0.
Four phases, ordered by pain. Each phase is independently shippable; stop
whenever the remaining items no longer feel worth it.

## Health snapshot

**Good:** lean go.mod, strong test culture (daemon/socket/pty/VT/registry all
covered, conformance fixtures), clean frontend module split with pure-core
`src/lib/` + matching unit tests, centralized frontend error surfacing
(`reportFailure`), fresh CHANGELOG, essentially zero TODO/FIXME rot (2
deliberate `ponytail:` markers only).

**Top problems, ranked:**

1. **e2e-real suite is red on `main` and gates every PR** (P1, spec 245).
   Linux fails 5/8, macOS 4/8 runs; doc-only commits flip CI red. Root cause
   undiagnosed. → Phase 1.
2. **Three hand-rolled copies of the daemon wire client** (GUI `app.go`,
   `hived-ws-bridge/main.go`, test-only `wire/testclient`) — the two
   production copies have no unit tests. → Phase 3.
3. **Zero JS static analysis** for ~14k LOC of vanilla JS (no ESLint/Prettier/
   Biome, no jsconfig). → Phase 2.
4. **Windows CI runs no frontend tests at all** despite Windows having the most
   platform-specific bug reports (#177, #183). → Phase 2.
5. **CI workflow copy-paste**: build-linux/macos/windows.yml share ~95% of the
   Wails bootstrap ladder; also duplicated in build.sh; Wails CLI pinned
   v2.12.0 in CI but `@latest` in build.sh. → Phase 2.
6. **God-files**: `internal/registry/registry.go` (1123, `Create` alone is 225
   lines), `cmd/hivegui/app.go` (912),
   `cmd/hivegui/frontend/src/app/session-term.ts` (996),
   `cmd/hivegui/frontend/src/style.css` (1488). → Phase 3 (Go), Later (JS/CSS).
7. **Docs drift**: README says v2.0.0-alpha.1 / Go 1.22 / nonexistent `spikes/`;
   CONTRIBUTING still documents the v1 TUI build; AGENTS.md links 6 missing
   docs; 4 never-filled template stubs (FRONTEND, RELIABILITY, QUALITY_SCORE,
   PRODUCT_SENSE); 16 exec-plans stuck in `active/` though shipped. → Phase 4.

## Phases

| Phase | File | Theme | Size |
|-------|------|-------|------|
| 1 | `phase-1-unblock-ci.md` | Fix/quarantine flaky e2e-real | S–M |
| 2 | `phase-2-ci-and-tooling.md` | Dedupe CI, Windows frontend leg, JS lint | M |
| 3 | `phase-3-go-consolidation.md` | Shared wire client, registry split, agent dedupe | M–L |
| 4 | `phase-4-docs-hygiene.md` | Kill doc drift, plan-lifecycle sweep | S |

## Decided in (was YAGNI, now committed)

- **TypeScript migration** — full migration of the ~14k-LOC vanilla JS.
  Decided 2026-07-25, overriding the original "cheap `@ts-check` 80% is
  enough" call. This is M–L, not a tooling add; scoped in Phase 2c as its own
  workstream and gated behind Biome landing first. See the effort note there
  before starting.

## Deliberately NOT proposed (YAGNI)

- Frontend framework migration — vanilla + modules is working fine.
- Replacing the shared `state` singleton — documented as intentional; only
  revisit if it starts causing real bugs.
- Splitting `style.css` / `session-term.ts` — cosmetic until they cause merge
  pain; noted in phase 3 as optional.
- Dropping `google/uuid` — 3 call sites, stable dep, not worth the churn.
