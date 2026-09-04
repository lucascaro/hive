# Feature: Idea inbox: capture ideas mid-session, start a session from one later

- **GitHub Issue:** —
- **Stage:** PLAN
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P1
- **Branch:** —
- **PR:** —

## Description

⌘I (or `hived idea add` from inside any session) files an idea / bug /
feedback note against a project; the sidebar shows a per-project inbox;
any open idea can become a new session with the idea as its opening
prompt. Spec: `docs/product-specs/337-idea-inbox.md`.

## Research

See `docs/exec-plans/active/337-idea-inbox.md` → Research.

### Relevant Code
- `internal/registry/persist.go`, `projects.go` — persisted shapes + event hub to mirror
- `internal/wire/control.go` — `CreateSpec`, project frames to mirror
- `internal/registry/create.go` — argv assembly for the opening prompt
- `cmd/hivegui/frontend/src/components/ProjectCard.tsx`, `components/modals/`, `app/keyboard.ts`

### Constraints / Dependencies
- Phase 3 typed-prompt path needs spec 336 phase 1 (`idle` state).
- `DaemonContract` bump.

## Plan

Three phases, one PR each — see exec plan.

### Risks
- ⌘I focus return must be exact; a stolen focus after capture is the
  whole feature failing. Validate in a real browser (Playwright mock,
  `CI=1`).
- Typed prompt on heuristic agents may land before the TUI is ready
  even at `idle`; keep a 300 ms grace after the idle transition.

## Implementation Notes

<Filled during IMPLEMENT stage.>

## PR convergence ledger

<Append-only.>

## QA verdict

<Append-only.>
