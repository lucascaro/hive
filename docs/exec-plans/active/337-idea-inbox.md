# Idea inbox: capture ideas mid-session, start a session from one later

- **Spec:** [docs/product-specs/337-idea-inbox.md](../../product-specs/337-idea-inbox.md)
- **Design:** [docs/design-docs/control-plane.md](../../design-docs/control-plane.md)
- **Issue:** —
- **Branch:** `feature/337-idea-inbox`
- **PR:** —
- **Status:** active

## Summary

Add an `Idea` record owned by the registry, four control frames to
manage it, a `hived idea` CLI that files one from inside a session, a
⌘I quick-capture sheet and a per-project inbox panel in the GUI, and
an `initial_prompt` on `CreateSpec` so an idea can become a session in
one click. Written so a smaller agent can implement each phase alone.

## Research

- `internal/registry/persist.go:12-52` — `MetaFile`, `IndexFile`,
  `ProjectMetaFile`: the persisted shapes. Sessions live in
  `sessions/<id>.json`, projects in `projects/<id>.json`, tombstones in
  `closed/`. Ideas follow the identical pattern in `ideas/<id>.json`;
  copy the atomic write helper used there (temp + rename).
- `internal/registry/projects.go` — project CRUD + `SubscribeProjects`
  (`:457`) and `ProjectEvent` fan-out. Ideas need the same trio
  (`Subscribe`, event kinds, broadcast); copy the project one, do not
  generalise the three hubs into one (out of scope).
- `internal/registry/closed.go` — the bounded-retention pattern
  (last 20 / 7 days) if ideas ever need pruning; they do not now.
- `internal/wire/control.go:334-390` — `ProjectInfo`, `ListProjectsReq`,
  `ProjectsResp`, `CreateProjectReq`, `UpdateProjectReq`, `ProjectEvent`
  and kinds. Mirror for ideas. `frame.go` ids run to `0x22` after spec
  336; ideas take `0x23`–`0x27`.
- `internal/wire/control.go:23` — `CreateSpec`; add `InitialPrompt`.
- `internal/registry/create.go:420` — argv assembly; the positional
  prompt is appended after `SessionIDFlag`/`SpawnArgs`. `create.go:511`
  is the "raw Cmd from client" branch — do not append there.
- `internal/agent/agent.go` — `Def`; add `PromptArgs func(prompt string) []string`
  (Claude: `[prompt]`; Pi: `[prompt]`; others nil ⇒ typed path).
- Spec 336's `agentstate` — the typed path waits for `State == idle`
  then writes `prompt + "\r"` to the PTY through `session.Session`'s
  existing input writer (find the method the attach path uses for
  `FrameData` C→S).
- `cmd/hived/main.go` — subcommand dispatch added by 336 (`hook`);
  `idea` is the second. Both are "client mode": dial socket, HELLO
  `control`, one request, print, exit.
- `internal/wire/client.go:20` — `wire.Client`, the control-mode client
  helper the GUI uses; reuse from the CLI.
- Env: `HIVE_SESSION_ID`, `HIVE_SOCKET` injected by 336 into every
  session. **This spec adds `HIVE_PROJECT_ID`** next to them in
  `create.go` (336 deferred it because nothing there reads it).
- GUI: `cmd/hivegui/frontend/src/app/keyboard.ts` (keymap tables —
  see `docs/product-specs/keyboard-keymap-tables.md`), `app/modals/`
  and `components/modals/` (the launcher and close-confirm sheets are
  the pattern for ⌘I), `components/ProjectCard.tsx` (where the count
  badge goes), `components/Sidebar.tsx`, `store/store.ts`,
  `app/state.ts`. `cmd/hivegui/app_control.go` / `app_calls.go` are the
  Go-side Wails bindings that relay control frames; each new frame needs
  a binding + event forward (`app_calls.go` shows `CreateProject`).
- `hivegui/frontend/src/bridge.ts` — the Wails ⇄ store bridge; new
  events (`IDEA_EVENT`) are registered here.

## Approach

### Data (`internal/registry/ideas.go`, `persist.go`)

```go
type IdeaFile struct {
    ID              string    `json:"id"`
    ProjectID       string    `json:"project_id"`
    Kind            string    `json:"kind"`               // idea | bug | feedback
    Text            string    `json:"text"`
    Status          string    `json:"status"`             // open | started | done
    Created         time.Time `json:"created"`
    Updated         time.Time `json:"updated"`
    SourceSessionID string    `json:"source_session_id,omitempty"`
    SessionID       string    `json:"session_id,omitempty"` // session started from it
    ExternalRef     string    `json:"external_ref,omitempty"` // reserved (GitHub issue URL later)
}
```

- Directory `StateDir()/ideas/`, one file per idea, `id` = UUID.
  `Text` capped at 4 KiB; `Kind`/`Status` validated against constant
  slices in `wire`.
- Registry API: `AddIdea(IdeaSpec) (wire.IdeaInfo, error)`,
  `UpdateIdea(id, patch)`, `RemoveIdea(id)`, `ListIdeas(projectID)`
  (empty ⇒ all), `SubscribeIdeas()`. Loaded at boot next to projects;
  a malformed file is logged and skipped, never deleted.
- `Kill`/close of a session whose `ID` matches an idea's `SessionID`
  does **not** change the idea (the GUI asks the user; it sends
  `UPDATE_IDEA{status:done}` explicitly).

### Wire

- `IdeaInfo` (same fields, `created`/`updated` RFC 3339 strings).
- `FrameListIdeas 0x23`, `FrameIdeas 0x24`, `FrameAddIdea 0x25`,
  `FrameUpdateIdea 0x26`, `FrameRemoveIdea 0x27`, `FrameIdeaEvent 0x28`.
  `IdeaEvent{Kind: added|updated|removed, Idea IdeaInfo}`.
- `AddIdeaReq{ProjectID, Kind, Text, SourceSessionID}`,
  `UpdateIdeaReq{ID, Text *string, Kind *string, Status *string, SessionID *string}`
  (pointer = "not provided"), `RemoveIdeaReq{ID}`, `ListIdeasReq{ProjectID}`.
- `CreateSpec.InitialPrompt string json:"initial_prompt,omitempty"` and
  `CreateSpec.IdeaID string json:"idea_id,omitempty"` — when set, the
  daemon links the idea (`status: started`, `session_id`) in the same
  registry transaction as the create, so a crash cannot leave a started
  session with an unlinked idea.
- `DaemonContract++` (new frames, new `CreateSpec` semantics).

### Daemon

`handleControlFrame` arms for the five C→S frames, dispatched inline
(no git; no goroutine needed). Idea events fan out on every control
connection like project events. `CREATE_SESSION` with `IdeaID` calls
`registry.CreateForIdea`.

### Initial prompt delivery (`internal/registry/create.go`)

- `def.PromptArgs != nil` ⇒ append `def.PromptArgs(prompt)` to argv
  (Claude, Pi). Quote nothing — it is argv, not a shell string.
- Otherwise ⇒ store `pendingPrompt` on the `Entry`; a registry listener
  on the 336 state machine writes `prompt + "\r"` to the PTY the first
  time the entry reaches `idle`, then clears it. If the session exits
  first, drop it and log. `ponytail:` comment naming the ceiling
  (multi-line prompts on TUIs that treat `\r` as submit only sometimes;
  Codex/Gemini both accept a single line).

### `hived idea` (`cmd/hived/idea.go`)

```
hived idea add [-k idea|bug|feedback] <text…>
hived idea list [-p <project-id>] [--json]
hived idea done <id>
```

`add` reads `HIVE_PROJECT_ID`, `HIVE_SESSION_ID`, `HIVE_SOCKET`; missing
socket/project ⇒ `hived idea: not running inside a Hive session` on
stderr, exit 2. `-p` overrides the project. Joins remaining args with
spaces. Prints the new idea's id. Uses `wire.Client` in control mode,
one request, exit.

### GUI

- **Store:** `ideas: Map<id, IdeaInfo>`, selectors `openIdeasByProject`.
  Bridge handles `IDEA_EVENT`; boot fetches `LIST_IDEAS`.
- **⌘I sheet** (`components/modals/QuickIdea.tsx`): text input
  (multiline, Enter submits, ⇧Enter newline — reuse the ⌘Enter/Enter
  convention from spec 217), kind segmented control (default idea),
  project select defaulting to the focused session's project, else the
  last used. On submit: `ADD_IDEA` with `source_session_id` = focused
  session; close; refocus previous terminal (use the existing focus
  restore helper from the close-confirm modal).
- **Inbox badge** on `ProjectCard.tsx`: open count; hidden at zero.
  ⇧⌘I toggles the panel for the focused project.
- **Inbox panel** (`components/IdeaInbox.tsx`): renders in the main area
  in place of the terminal grid (like the worktree browser does), list
  of ideas with kind chip, text, relative age, source session name (if
  still open) and, for `started`, a link that focuses the session.
  Row actions: Start session, Edit (inline, reuse `inline-rename.ts`),
  Done, Delete (confirm). Filter: open / all.
- **Start session:** opens the launcher modal with project locked and a
  read-only "Opening prompt" preview (`"<Kind>: <text>"`); the modal's
  create call passes `initial_prompt` + `idea_id`. Worktree checkbox
  honoured; branch name suggestion = slug of the first six words.
- **Session row:** small idea glyph when `idea_id` linked (store keeps a
  reverse map from `IdeaInfo.session_id`).
- **Close session:** if a linked idea is `started`, the close-confirm
  sheet gains a checkbox "Mark idea done" (unchecked by default).
- Keymap: add ⌘I and ⇧⌘I to the keymap tables doc.

### Files to change

- `internal/wire/control.go`, `frame.go` — ideas frames/types, `CreateSpec` fields.
- `internal/buildinfo/contract.go` — bump.
- `internal/registry/persist.go` — `IdeaFile`, ideas dir, load/save.
- `internal/registry/registry.go` — boot load, `Entry.pendingPrompt`,
  idle listener.
- `internal/registry/create.go` — `PromptArgs`, `CreateForIdea`, `HIVE_PROJECT_ID` env.
- `internal/daemon/daemon.go` — frame arms + fan-out.
- `internal/agent/agent.go`, `claude.go`, `pi.go` — `PromptArgs`.
- `cmd/hived/main.go` — `idea` subcommand.
- `cmd/hivegui/app_calls.go`, `app_control.go` — bindings + event forward.
- Frontend: `bridge.ts`, `app/state.ts`, `store/store.ts`,
  `app/keyboard.ts`, `components/ProjectCard.tsx`, `Sidebar.tsx`,
  `SessionRow.tsx`, `components/modals/<launcher>.tsx`,
  `components/modals/<close-confirm>.tsx`.
- `DESIGN.md` — registry owns `ideas/`; `docs/product-specs/keyboard-keymap-tables.md`.

### New files

- `internal/registry/ideas.go`, `ideas_test.go`
- `cmd/hived/idea.go`, `idea_test.go`
- `cmd/hivegui/frontend/src/components/IdeaInbox.tsx`,
  `components/modals/QuickIdea.tsx`, tests for both
- `cmd/hivegui/frontend/e2e/idea-inbox.spec.ts` (mock)

### Tests

- Registry: `TestAddIdeaPersistsAtomically`, `TestIdeasSurviveReload`,
  `TestMalformedIdeaSkipped`, `TestCreateForIdeaLinksAtomically`,
  `TestPendingPromptTypedOnIdle`, `TestPendingPromptDroppedOnExit`.
- Wire: round-trips; `UpdateIdeaReq` pointer semantics.
- Daemon: `TestIdeaEventFanOut`, `TestCreateSessionWithIdeaID`.
- `cmd/hived`: `TestIdeaAddOutsideHiveExits2`, e2e `TestIdeaAddFromSession`
  (spawn a shell session, run `hived idea add` inside it via PTY, assert
  `IDEA_EVENT(added)` with the right `source_session_id`).
- Agent: `TestClaudePromptArgs`, `TestPiPromptArgs`.
- Frontend: store reducers; `QuickIdea` submit/cancel/focus-return;
  Playwright mock e2e per spec success criteria. Run with `CI=1`.

### Phasing

1. Registry + wire + daemon + `hived idea` CLI (no GUI). Shippable:
   capture from the shell, list from the shell.
2. GUI capture sheet + badge + inbox panel (list/edit/done/delete).
3. `initial_prompt` + Start session + close-confirm checkbox. Needs
   336 phase 1 for the typed path; Claude/Pi work without it.

## Decision log

- **2026-09-04** — Ideas are project-scoped, not session-scoped. Why:
  the filing session is usually unrelated to where the idea belongs
  and will be closed long before the idea is acted on.
- **2026-09-04** — Idea ↔ session link is set by the daemon inside the
  create, not by a follow-up `UPDATE_IDEA` from the GUI. Why: no window
  for a crash to leave a started session unlinked.
- **2026-09-04** — Opening prompt via argv for Claude/Pi, typed-on-idle
  for the rest. Why: argv is exact and survives TUI quirks; typing is
  the only agnostic fallback and 336 makes "idle" knowable.
- **2026-09-04** — No GitHub sync in v1, but `external_ref` is
  reserved. Why: the user's pipeline already turns issues into specs;
  ideas are the step *before* an issue.

## Progress

- **2026-09-04** — Spec and plan written; stage PLAN.

## Open questions

1. Where does the inbox panel live for a project with no focused
   session? Proposal: clicking the badge always works; ⇧⌘I with no
   focused session opens the inbox of the first project. Confirm with
   the operator during phase 2.
2. Should `hived idea add` be reachable as a Claude skill/command so an
   agent files ideas by convention? Out of scope here; note for the
   orchestration layer.
