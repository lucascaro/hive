# Phase 4 — Docs and process hygiene

All small, mechanical, low-risk. One or two PRs total. `hs-doc-garden` is the
repo's own tool for exactly this — consider running it instead of doing this
by hand.

## 4a. Fix stale claims

- `README.md`: stale in several places, so grep-sweep `v2.0.0` rather than
  fixing one line — "Latest release: v2.0.0-alpha.1" (actual: **v2.3.0**) AND
  the build example `./build.sh --version v2.0.0-alpha.2`; layout lists
  nonexistent `spikes/`; path `hivegui/frontend/` should be
  `cmd/hivegui/frontend/`; "Go 1.22+" vs go.mod's 1.25.0.
- `CONTRIBUTING.md`: build instructions (`go build -o hive .`, install to
  `/usr/local/bin`) describe the **v1 TUI**, contradicting README's v2 Wails
  flow. Rewrite the build section; keep the (current) test-layer table.
- `AGENTS.md`: doc-map links 6 files that don't exist (`ARCHITECTURE.md`,
  `docs/hooks.md`, `docs/keybindings.md`, `docs/agent-teams.md`,
  `docs/features.md`, `docs/design-decisions.md`). De-reference them, or
  point `ARCHITECTURE.md` mentions at `DESIGN.md` which actually covers it.

## 4b. Delete the unfilled template stubs

`FRONTEND.md`, `RELIABILITY.md`, `QUALITY_SCORE.md`, `PRODUCT_SENSE.md` are
hivesmith scaffolds containing only `<placeholder>` tables. **Decided
2026-07-25: delete all four** (recoverable from the template if ever wanted);
an empty template masquerading as a doc is worse than no doc.

## 4c. Plan-lifecycle sweep

- `docs/exec-plans/active/` holds 16 plans; many shipped in v2.3.0 and still
  say `Status: active` (e.g. 218, 240-jump-to-next, 240-custom-agents, 217,
  213, 210). Move shipped ones to `completed/` per PLANS.md.
- Dedupe `docs/product-specs/144-vt-snapshot-rgb-24-bit-colors*.md` (two
  files for issue #144).
- `features/BACKLOG.md`: Completed/Rejected tables are empty despite ~30
  shipped features. **Decided 2026-07-25: drop those tables** and let
  product-specs/CHANGELOG be the single record; don't maintain two ledgers.
- `docs/generated/` and `docs/references/` contain only a README each —
  delete or leave; zero cost either way.

## 4d. Process tweak to prevent recurrence

Add "move exec-plan to completed/" to the release checklist in
`scripts/release.sh`'s pre-flight (or the PR template), so the sweep happens
at merge time instead of piling up.
