# Idea inbox: capture ideas mid-session, start a session from one later

- **Spec:** [docs/product-specs/337-idea-inbox.md](../../product-specs/337-idea-inbox.md)
- **Design:** [docs/design-docs/control-plane.md](../../design-docs/control-plane.md)
- **Issue:** —
- **Branch:** `cedar-light` (phase 1)
- **PR:** [#352](https://github.com/lucascaro/hive/pull/352) (phase 1)
- **Status:** active

## Summary

Add an `Idea` record owned by the registry, six control frames to
manage it, a `hived idea` CLI that files one from inside a session, a
⌘I quick-capture sheet and a per-project inbox modal in the GUI, and
an `initial_prompt` on `CreateSpec` so an idea can become a session in
one click. Written so a smaller agent can implement each phase alone.

## Research

Every citation below was re-verified against the tree during review;
line numbers are as of `origin/main` at ad68e32.

- `internal/registry/persist.go:11-53` — `MetaFile`, `IndexFile`,
  `ProjectMetaFile`, `ProjectIndexFile`: the persisted shapes. **The
  on-disk layout is a directory per record**, not a flat file:
  `sessions/<id>/session.json` + `sessions/index.json`
  (`registry.go:788`), `projects/<id>/project.json` +
  `projects/index.json` (`projects.go:361`). Ideas deliberately
  diverge — flat `ideas/<id>.json`, no index file — because they carry
  no user-visible order (newest-first is derived from `Created`) and so
  need no order authority. Use the same atomic writer: `writeAtomic` at
  `internal/registry/persist.go:55` (temp + rename at `:63`).
- `internal/registry/paths.go:52-63` — `SessionsDir` / `ProjectsDir`.
  `IdeasDir(stateDir)` belongs here beside them, **not** in
  `persist.go`.
- `internal/registry/events.go:9-18` — `Listener`, `ProjectListener`,
  `Subscribe`. This is where the listener types and `Subscribe` live;
  `projects.go:457` is `SubscribeProjects` and `:480` is
  `broadcastProject` (building `wire.ProjectEvent` at `:483`). Ideas
  need the same trio (`IdeaListener` in `events.go`, `SubscribeIdeas` +
  `broadcastIdea` in `ideas.go`); copy the project one, do not
  generalise the three hubs into one (out of scope).
- `internal/registry/closed.go:31-32` — the bounded-retention pattern
  (`maxTombstones = 20`, `maxTombstoneAge = 7 * 24h`) if ideas ever need
  pruning; they do not now.
- `internal/wire/control.go:488-545` — `ProjectInfo` (`:488`),
  `ListProjectsReq` (`:498`), `ProjectsResp` (`:502`),
  `CreateProjectReq` (`:507`), `KillProjectReq` (`:516`),
  `UpdateProjectReq` (`:522`), event kinds (`:532`), `ProjectEvent`
  (`:539`). Mirror for ideas.
- `internal/wire/control.go:29` — `type CreateSpec struct`; add
  `InitialPrompt`.
- `internal/wire/control.go:565-590` — `ControlError` and the
  well-known error codes (`ErrCodeWorktreeDirty` at `:580`). Note
  `ControlError` carries `SessionID` but **no `ProjectID`**; the
  open-ideas refusal below needs one added.
- `internal/wire/frame.go:128-136` — the highest frame id currently
  defined is `FrameAgentEvent = 0x22`. Next free is `0x23`. Ideas take
  `0x23`–`0x28` (six ids). `FrameType.String()` at `frame.go:139` has a
  case per frame — a missing case makes the daemon's `unexpected
  control frame` log (`daemon.go:984`) print a bare number.
- `internal/wire/client.go:120-137` — **`controlEvents`**, the shared
  frame → client-event dispatch table. `cmd/hivegui/app_control.go:433`
  and `cmd/hived-ws-bridge/main.go:503` fan out *solely* through
  `ControlEventName`; a frame with no entry here is silently dropped and
  the GUI never hears it. Request frames must stay absent —
  `internal/wire/wire_test.go:404-424` pins exactly that shape.
- `internal/wire/testclient/client.go:107` — the daemon test helper the
  e2e suites drive (`cmd/hived/e2e_test.go:442`,
  `custom_agent_e2e_test.go:43`). The planned e2e needs idea methods
  here.
- `internal/registry/create.go:404-431` — `resolveAgentCmd`; argv is
  assembled with `SessionIDFlag` at `:420` and `SpawnArgs` at
  `:425-429`; a positional prompt appends at `:430`. `:405`
  (`if len(cmd) > 0 || spec.Agent == ""`) already short-circuits the
  raw-`Cmd`-from-client case before any append, so no extra guard is
  needed there.
- **`SpawnArgs` is appended in two places** — `create.go:426` and
  `registry.go:631` (used by `Restart` at `:962` and `Revive` at
  `:1079`). The opening prompt must be appended in `create.go` **only**;
  putting it in `appendSpawnArgs` would re-send it on every restart.
- `internal/agent/agent.go:29-66` — `Def`. The `Def` **literals** live
  in the `defsByID` map at `agent.go:91+` (Claude `:99-109`, Pi
  `:152+`); `claude.go` / `pi.go` hold only the adapter funcs
  (`claudeResumeArgs:72`, `claudeSpawnArgs:121`, `piSpawnArgs:72`).
- `internal/agentstate/machine.go` — spec 336's state machine.
  `wire.StateIdle` is the **empty string** (`control.go:214`) and
  `attachSessionHooks` (`registry.go:340-357`) installs a fresh machine
  that *starts* idle. **`Machine` has no `Subscribe`** — its API is
  `New/Snapshot/Output/Bell/Exit/ClearWaiting/Tick/Apply`. Transitions
  are announced by the registry: `announceStateLocked`
  (`registry.go:474`) after `Apply` (`registry.go:424`), under `r.mu`.
- `internal/session/session.go:431` — `(*session.Session).Write`, the
  method the attach path uses for C→S `FrameData`
  (`daemon.go:1070-1072`). This is what types a prompt into the PTY.
- `internal/registry/registry.go:605-617` — **`hiveEnv`** builds the
  `HIVE_SESSION_ID` / `HIVE_SOCKET` pair. `create.go:115` and
  `registry.go:965` (revive/restart) both call it. Its doc comment
  already names spec 337 as the reason it exists.
- `cmd/hived/main.go:28-33` — subcommand dispatch added by 336 is a
  single `if os.Args[1] == "hook"`, not a `switch`. Adding `idea` means
  a second `if` or a small dispatch refactor.
- `internal/wire/client.go:20` — `wire.Client`; entry point
  `wire.Handshake(conn, hello)` at `:37`. Reuse from the CLI.
- `internal/buildinfo/contract.go` — `DaemonContract = 4`. Bumping means
  editing the const **and** prepending a numbered `N — why` History
  entry in the doc comment above it; `scripts/check-daemon-contract.sh`
  gates it in CI (`changesets.yml:97`).
- GUI: every modal in this repo is a **pair** — `app/modals/<x>.ts` owns
  state, `components/modals/<X>.tsx` renders. The launcher is
  `app/modals/launcher.ts` + `components/modals/Launcher.tsx`.
  **There is no close-confirm modal file**: it is an `openChoiceDialog()`
  call at `app/events.ts:797`, rendered by
  `components/modals/ChoiceDialog.tsx` from `ChoiceSpec`
  (`app/modals/choice-dialog.ts:30`, fields
  `title/detail/bullets/note/choices` only). **There is no main-area
  panel precedent**: `GridView` (`components/App.tsx:48`, rendered
  `:120`) is the only main-area component; the worktree browser is a
  modal (`components/modals/Worktrees.tsx`).
- Focus restore is **not** a shared helper on the modal path — it is
  re-implemented inline at `ChoiceDialog.tsx:64`, `inline-rename.ts:77`,
  `app/modals/launcher.ts:122`, `app/modals/settings.ts:74`. The
  exported functions to call are `focusActiveTerm()` /
  `refocusActiveTerm()` at `app/focus.ts:320` / `:324`.
- `app/inline-rename.ts` exists (opener captured at `:77`).
- `cmd/hivegui/app_calls.go:226` — `CreateProject` binding, writing
  `wire.FrameCreateProject` at `:231`: the template for a new binding.
  Event forwarding is generic (`app_control.go:434`
  `wruntime.EventsEmit`), so a new S→C frame needs only a
  `controlEvents` entry, not a new Go function.
- `cmd/hivegui/app_calls.go:109` — `App.CreateSession` is **11
  positional parameters**. Adding `initialPrompt` makes 12.
- `cmd/hived-ws-bridge/main.go:329` decodes `CreateSpec` as raw JSON, so
  it needs no change. `cmd/hivebar/client.go:127` ignores unknown frames
  by design — no change there either.
- `cmd/hivegui/frontend/src/lib/shortcuts.ts:8-16` — the module header
  enumerates the **five-file drift surface** for any binding change:
  (1) `app/keyboard.ts` + `lib/keymap.ts`, (2) `shortcutGroups()` AND
  `paletteShortcuts()` in `shortcuts.ts`, (3) the palette command table
  in `main.tsx`, (4) `cmd/hivegui/menu_darwin.go` (⌘ chords only — ⌘I
  and ⇧⌘I both qualify), (5) the Keybinds table in `README.md`.
  `AGENTS.md:177-192` repeats this.
- `DESIGN.md:48` enumerates what the registry owns (`sessions/`,
  `projects/`, `closed/`); `DESIGN.md:87` is a closed enumeration of
  every writer under `StateDir` ("The GUI owns three files… hived owns
  one more… All four"). Ideas touch `:48`; `:87`'s counts stay correct
  only because ideas are registry-owned.
- `.github/workflows/changesets.yml:113-129` — every PR must add a
  `.changesets/*.md` entry or carry the `no-changeset` label.
  `scripts/check-changeset.sh` is the local mirror.

## Approach

### Data (`internal/registry/ideas.go`, `paths.go`)

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

- `IdeasDir(stateDir)` in `paths.go` ⇒ `StateDir()/ideas/`, one flat
  file per idea, `id` = UUID. See the Research note on why this diverges
  from the per-record-directory layout.
- `Text` is **rejected**, not truncated, above 4 KiB
  (`ErrCodeIdeaTooLong`); `Kind`/`Status` validated against constant
  slices in `wire`. Unknown `ProjectID` ⇒ error, no write.
- `ExternalRef` stays on `IdeaFile` only. It is **not** on
  `wire.IdeaInfo` — nothing renders it, and adding an `omitempty` JSON
  field to the wire type later is a zero-cost change.
- Registry API: `AddIdea(IdeaSpec) (wire.IdeaInfo, error)`,
  `UpdateIdea(id, patch)`, `RemoveIdea(id)`, `ListIdeas(projectID)`
  (empty ⇒ all), `SubscribeIdeas()`. Loaded at boot next to projects; a
  malformed file is logged and skipped, never deleted.
- `Kill`/close of a session whose `ID` matches an idea's `SessionID`
  does **not** change the idea (the GUI asks the user; it sends
  `UPDATE_IDEA{status:done}` explicitly).

### Project deletion cascade (`internal/registry/projects.go`)

`KillProject` currently reassigns or kills sessions and then
`os.RemoveAll`s the project directory; nothing touches ideas, so as
written they would survive as unreachable orphans (no card exists to
badge them). **Ideas are deleted with their project**, with a confirm
step when any are still open:

- `KillProjectReq` gains `DeleteIdeas bool json:"delete_ideas,omitempty"`.
- If the project has ≥1 idea with `status != done` and `DeleteIdeas` is
  false, the daemon refuses with
  `ErrCodeProjectHasIdeas = "project_has_ideas"` and does nothing.
- `ControlError` gains `ProjectID string json:"project_id,omitempty"` so
  the GUI knows which project to retry — it currently carries only
  `SessionID`.
- **Refactor the two guards into one path.** `worktree_dirty` (session
  level) and `project_has_ideas` (project level) are the same shape:
  the daemon refuses destructive work that would silently lose
  something, the GUI confirms, the client retries with a force flag.
  Extract that into a single close-guard helper on the daemon side and
  a single `openChoiceDialog` + retry branch in `app/events.ts`, keyed
  on the error code, rather than a second copy of the dirty-worktree
  branch. `ControlError.ProjectID` is what lets one branch serve both.
- The confirm names the open-idea count and retries with
  `delete_ideas: true`.
- Idea files are removed before `os.RemoveAll` of the project dir, each
  emitting `IDEA_EVENT(removed)`.

### Wire

- `IdeaInfo` — same fields as `IdeaFile` **minus `ExternalRef`**,
  `created`/`updated` as RFC 3339 strings.
- Frames (`internal/wire/frame.go`, next free id is `0x23`):
  `FrameListIdeas 0x23` (C→S), `FrameIdeas 0x24` (S→C),
  `FrameAddIdea 0x25` (C→S), `FrameUpdateIdea 0x26` (C→S),
  `FrameRemoveIdea 0x27` (C→S), `FrameIdeaEvent 0x28` (S→C).
  **Four C→S, two S→C.** Add a `String()` case for each at
  `frame.go:139`.
- `internal/wire/client.go` `controlEvents`: add
  `FrameIdeas: "idea:list"` and `FrameIdeaEvent: "idea:event"`. The four
  request frames stay absent (`wire_test.go:404-424` asserts it).
- `IdeaEvent{Kind: added|updated|removed, Idea IdeaInfo}`.
- `AddIdeaReq{SessionID, ProjectID, Kind, Text}` — `SessionID` is the
  filing session (becomes `source_session_id`); `ProjectID` is optional
  and, when empty, **resolved by the daemon from the live registry entry
  for `SessionID`**. Neither set ⇒ error.
- `UpdateIdeaReq{ID, Text *string, Status *string}` (pointer = "not
  provided"), matching `UpdateProjectReq`'s shape. No `Kind` (nothing
  asks to re-kind) and no `SessionID` (see below).
- `RemoveIdeaReq{ID}`, `ListIdeasReq{ProjectID}`.
- `CreateSpec.InitialPrompt string json:"initial_prompt,omitempty"`.
  **No `CreateSpec.IdeaID`** — the GUI sends
  `UPDATE_IDEA{status:started, session_id}` after the create returns.
  There is no registry transaction to join (session and idea are two
  independent temp+rename writes), so a daemon-side link would shrink
  the crash window without closing it, at the cost of a second create
  path.
- Error codes: `ErrCodeIdeaTooLong`, `ErrCodeProjectHasIdeas`.
- `DaemonContract`: 4 → 5, with a `5 — …` History entry in the doc
  comment above the const (the gate expects both).

### Daemon

`handleControlFrame` arms for the four C→S idea frames, dispatched
inline (no git; no goroutine needed). Idea events fan out on every
control connection like project events. `LIST_IDEAS` replies with
`FrameIdeas`.

**Version skew is a silent degrade, and that is accepted:** an old GUI
against a new daemon simply never sends idea frames; a new GUI against
an old daemon gets `unexpected control frame` logs (`daemon.go:984`) and
a `LIST_IDEAS` that never resolves, showing an empty inbox with no
error. `app_control.go:157` already surfaces the contract mismatch as
the `daemon:stale` banner, which is the user-facing signal.

### Initial prompt delivery (`internal/registry/create.go`)

- `def.PositionalPrompt` (a `bool` on `agent.Def`, true for Claude and
  Pi) ⇒ append the prompt as a bare positional at `resolveAgentCmd:430`.
  Quote nothing — it is argv, not a shell string. A `func` field would
  be scaffolding: both known implementations are `[]string{prompt}`;
  widen to a func when an agent needs a flag rather than a positional.
- **Verify before implementing phase 3.** That
  `claude --session-id <uuid> "text"` starts an *interactive* session
  seeded with the text (rather than non-interactive print mode), and the
  same for `pi --session-id <id> "text"`, is asserted by the spec but
  proven by nothing in this tree. Run both by hand first; if either
  drops to one-shot mode, that agent falls back to the typed path below
  and `PositionalPrompt` may end up with zero users.
- Otherwise ⇒ store `pendingPrompt` on the `Entry` and write
  `prompt + "\r"` via `(*session.Session).Write` when the entry next
  reaches idle. Three constraints the naive version gets wrong:
  1. **Not "the first time it reaches idle."** `wire.StateIdle` is the
     empty string and `attachSessionHooks` (`registry.go:340-357`)
     starts every machine idle, so that predicate matches at t=0 —
     before the TUI has drawn an input box. Fire on the first idle edge
     *after* a `working` transition.
  2. **No listener exists.** `agentstate.Machine` has no `Subscribe`;
     the hook goes at the `announceStateLocked` (`registry.go:474`) /
     `Apply` (`registry.go:424`) call sites — which run under `r.mu`. Do
     the PTY write **outside** the lock (capture the pending prompt
     under `r.mu`, clear it, then write) or the registry mutex is held
     across a PTY write.
  3. **`pendingPrompt` is in-memory only** (`Entry`'s unpersisted
     fields, `registry.go:73-120`; `Cmd` is not in `MetaFile` at all). A
     daemon restart between create and first idle loses it, and `Revive`
     (`registry.go:948-963`) rebuilds argv without it. Therefore the
     idea flips to `started` **only after** the prompt is delivered
     (argv path: immediately; typed path: on delivery), not at create
     time. If the session exits first, drop the prompt, log, and leave
     the idea `open`.
  - `ponytail:` comment naming the ceiling (multi-line prompts on TUIs
    that treat `\r` as submit only sometimes; Codex/Gemini both accept a
    single line).

### `hived idea` (`cmd/hived/idea.go`)

```
hived idea add [-k idea|bug|feedback] <text…>
hived idea list [--all]
```

`add` reads `HIVE_SESSION_ID` and `HIVE_SOCKET`; **no
`HIVE_PROJECT_ID`** — the daemon resolves the project from the live
session entry, so an idea filed after a session is reassigned lands in
the right project. Missing socket or session id ⇒ `hived idea: not
running inside a Hive session` on stderr, exit 2. A session whose
project cannot be resolved falls back to the default project (spec:92)
rather than erroring. Joins remaining args with spaces. Prints the new
idea's id. Uses `wire.Client` in control mode, one request, exit.

`list` exists only so phase 1 is usable before the GUI lands; it prints
the current session's project, or every project with `--all`. Pruned as
speculative: `--json` (no consumer), `-p` (the session resolves the
project), `hived idea done` (the GUI action the spec asks for covers
it). Add each when something actually needs it.

### GUI

- **Store:** `ideas: Map<id, IdeaInfo>`, selectors `openIdeasByProject`.
  Bridge handles `idea:event` and `idea:list`; boot fetches
  `LIST_IDEAS`. Wire payloads are snake_case on the JS side.
- **⌘I sheet** (`app/modals/quick-idea.ts` +
  `components/modals/QuickIdea.tsx` — modals are a state/render pair in
  this repo): text input (multiline, Enter submits, ⇧Enter newline —
  reuse the ⌘Enter/Enter convention from spec 217), kind segmented
  control (default idea), project select defaulting to the focused
  session's project, else the **default project** (spec:92). On submit:
  `ADD_IDEA` with `session_id` = focused session; close; call
  `refocusActiveTerm()` from `app/focus.ts:324` (there is no shared
  helper on the modal path — the other modals each re-implement it).
- **Inbox badge** on `ProjectCard.tsx`: open count; hidden at zero.
  ⇧⌘I opens the inbox for the focused project; **with no focused session
  it is a no-op** — the badge click covers that case, and "first
  project" would open an unrelated inbox on a stray keystroke.
- **Inbox modal** (`app/modals/idea-inbox.ts` +
  `components/modals/IdeaInbox.tsx`): the same modal shell as
  Worktrees/Launcher. `GridView` is the only main-area component in the
  frontend and there is no view-swap concept to reuse, so a modal is
  both the repo pattern and the smaller diff. List of open ideas, newest
  first, with kind chip, text, relative age, source session name (if
  still open) and, for `started`, a link that focuses the session. Row
  actions: Start session, Edit (inline, reuse `inline-rename.ts`), Done,
  Delete (confirm). No open/all filter — one predicate; add the toggle
  when someone asks to review completed ideas.
- **Start session:** opens the launcher modal with project locked and a
  read-only "Opening prompt" preview (`"<Kind>: <text>"`); the modal's
  create call passes `initial_prompt`. On success the GUI sends
  `UPDATE_IDEA{status:started, session_id}`. Worktree checkbox honoured;
  the branch field keeps the launcher's existing default (no slugging —
  unicode/punctuation/collision edge cases for behaviour the spec never
  asked for).
- **Session row:** small idea glyph when a `started` idea's `session_id`
  matches (store keeps a reverse map from `IdeaInfo.session_id`; cheaper
  than a new `SessionInfo` wire field).
- **Close session: no idea prompt.** Closing a session leaves its idea
  `started`; "mark done" is an inbox row action only. Nothing is lost
  on session close — the idea outlives the session by design. The
  guard belongs at project delete, where ideas actually are destroyed.
  (The spec was amended to match; it previously asked for a
  "mark idea done" checkbox on close.) Note the close-confirm sheet is
  **not a component** anyway — it is `openChoiceDialog()` at
  `app/events.ts:797`, fires only on `worktree_dirty`, and `ChoiceSpec`
  has no checkbox field.
- **Keybinding drift surface is five files** (`shortcuts.ts:8-16`,
  `AGENTS.md:177-192`), all listed below.

### Files to change

Go:

- `internal/wire/frame.go` — six frame ids `0x23`–`0x28` + `String()`
  cases.
- `internal/wire/control.go` — `IdeaInfo`, req/resp types, `IdeaEvent`,
  `CreateSpec.InitialPrompt`, `KillProjectReq.DeleteIdeas`,
  `ControlError.ProjectID`, two error codes.
- `internal/wire/client.go` — `controlEvents` entries for `FrameIdeas`
  and `FrameIdeaEvent`.
- `internal/wire/testclient/client.go` — idea methods for the e2e
  suites.
- `internal/buildinfo/contract.go` — bump to 5 **+ History entry**.
- `internal/registry/paths.go` — `IdeasDir`.
- `internal/registry/persist.go` — `IdeaFile`, load/save via
  `writeAtomic`.
- `internal/registry/events.go` — `IdeaListener`.
- `internal/registry/registry.go` — boot load, `Entry.pendingPrompt`,
  idle-edge hook at the `announceStateLocked`/`Apply` sites (write
  outside `r.mu`).
- `internal/registry/create.go` — positional prompt append in
  `resolveAgentCmd` **only** (not `appendSpawnArgs`).
- `internal/registry/projects.go` — idea cascade + open-ideas refusal in
  `KillProject`.
- `internal/daemon/daemon.go` — frame arms + fan-out.
- `internal/agent/agent.go` — `Def.PositionalPrompt bool` + `defsByID`
  entries for Claude and Pi (the literals live here, not in
  `claude.go`/`pi.go`).
- `cmd/hived/main.go` — `idea` subcommand dispatch.
- `cmd/hivegui/app_calls.go` — idea bindings; **`CreateSession` converts
  from 11 positional parameters to a single options struct** and gains
  `InitialPrompt`. Wails regenerates the TS bindings from the Go
  signature, so `cmd/hivegui/frontend/wailsjs/` changes too (generated —
  run `./scripts/ci-bootstrap.sh`, do not hand-edit).
- `cmd/hivegui/menu_darwin.go` — ⌘I and ⇧⌘I menu items.

Frontend (`cmd/hivegui/frontend/`):

- `src/bridge.ts`, `src/app/state.ts`, `src/store/store.ts`
- `src/app/keyboard.ts`, `src/lib/keymap.ts`, `src/lib/shortcuts.ts`
  (both `shortcutGroups()` and `paletteShortcuts()`), `src/main.tsx`
  (palette command table)
- `src/app/events.ts` — `project_has_ideas` confirm branch
- `src/components/ProjectCard.tsx`, `Sidebar.tsx`, `SessionRow.tsx`
- `src/components/modals/Launcher.tsx` (`:289`) +
  `src/app/modals/launcher.ts` — opening-prompt preview,
  `initial_prompt` on create, options-struct call
- `test/e2e/wails-mock.ts` (`:300`, `:954-966`),
  `test/e2e-real/wails-bridge.ts` (`:147-162`),
  `test/dom/launcher.test.tsx` (`:93`) — rewritten for the options
  struct

Docs:

- `DESIGN.md:48` — registry owns `ideas/` (`:87`'s "all four" count is
  unaffected because ideas are registry-owned; say so).
- `docs/product-specs/keyboard-keymap-tables.md` — ⌘I, ⇧⌘I.
- `README.md` — Keybinds table.
- `.changesets/337-idea-inbox-registry.md`,
  `.changesets/337-idea-inbox-gui.md`,
  `.changesets/337-idea-inbox-initial-prompt.md` — one per phase/PR; CI
  hard-fails a PR with none.

### New files

- `internal/registry/ideas.go`, `internal/registry/ideas_test.go`
- `cmd/hived/idea.go`, `cmd/hived/idea_test.go`
- `cmd/hivegui/frontend/src/app/modals/quick-idea.ts`,
  `src/components/modals/QuickIdea.tsx`
- `cmd/hivegui/frontend/src/app/modals/idea-inbox.ts`,
  `src/components/modals/IdeaInbox.tsx`
- `cmd/hivegui/frontend/test/dom/quick-idea.test.tsx`,
  `test/dom/idea-inbox.test.tsx`
- `cmd/hivegui/frontend/test/e2e/idea-inbox.spec.ts` (mock)

### Tests

Registry (`ideas_test.go`):

- `TestAddIdeaPersistsAtomically`, `TestIdeasSurviveReload`,
  `TestMalformedIdeaSkipped`
- `TestAddIdeaRejectsOversizeText` (>4 KiB ⇒ `idea_too_long`, no file
  written), `TestAddIdeaRejectsUnknownKind`,
  `TestUpdateIdeaRejectsUnknownStatus`
- `TestAddIdeaUnknownProject`, `TestUpdateIdeaUnknownID`,
  `TestRemoveIdeaUnknownID`
- `TestListIdeasEmptyProjectReturnsAll`
- `TestAddIdeaResolvesProjectFromSession` (and that it follows a session
  reassigned via `UpdateSession`)
- `TestKillProjectRefusesWithOpenIdeas`,
  `TestKillProjectDeletesIdeasWithForce`
- `TestPendingPromptTypedOnIdleAfterWorking` (asserts it does **not**
  fire on the t=0 idle), `TestPendingPromptDroppedOnExit`,
  `TestIdeaNotStartedUntilPromptDelivered`

Wire:

- Round-trips for every new type; `UpdateIdeaReq` pointer semantics.
- `TestIdeaFrameStrings` (no bare-number `String()`).
- `TestIdeaControlEventNames` — `FrameIdeas`/`FrameIdeaEvent` map, the
  four request frames do not (extends `wire_test.go:404-424`).

Daemon:

- `TestIdeaEventFanOut`, `TestListIdeasReply`,
  `TestCreateSessionWithInitialPrompt`.
- `TestCloseGuardRefusesAndForces` — table-driven over both codes
  (`worktree_dirty`, `project_has_ideas`), asserting the shared guard
  refuses without the force flag and proceeds with it. This is what
  stops the refactor from regressing the existing dirty-worktree
  behaviour (`daemon_test.go:390`, `:492`).

Agent / create:

- `TestClaudePositionalPrompt`, `TestPiPositionalPrompt`,
  `TestRestartDoesNotResendPrompt` (guards the
  `create.go`-not-`appendSpawnArgs` placement).

`cmd/hived`:

- `TestIdeaAddOutsideHiveExits2`, `TestIdeaListOutput`
- e2e `TestIdeaAddFromSession` — spawn a shell session, run
  `hived idea add` inside it via PTY, assert `IDEA_EVENT(added)` with
  the right `source_session_id` and daemon-resolved `project_id`.

Frontend:

- Store reducers; `QuickIdea` submit/cancel/focus-return; `IdeaInbox`
  list/edit/done/delete.
- `launcher.test.tsx` covers the options-struct call shape (guards the
  refactor); the existing dirty-worktree confirm test must still pass
  against the shared branch.
- Playwright mock e2e per spec success criteria: capture → count →
  start → prompt visible in the fake PTY. Run with `CI=1`.

## Verification

Run from the repo root of the worktree. Fresh worktree first:

```sh
./scripts/ci-bootstrap.sh                # pinned Wails CLI + generated bindings
(cd cmd/hivegui/frontend && npm install --no-audit --no-fund)
(cd cmd/hivegui/frontend && ./node_modules/.bin/playwright install chromium)
```

`ci-bootstrap.sh` installs the Wails CLI and regenerates the wailsjs
bindings `npm run typecheck` needs, but it does **not** install node
modules — every `./node_modules/.bin/*` command below is unrunnable
until the `npm install` above. (`scripts/test.sh:30-33` lazy-installs
them itself; the standalone commands do not.)

Then, per phase:

```sh
GOTOOLCHAIN=$(sed -n 's/^go //p' go.mod) go build ./... && go vet ./...
go test ./...
for os in darwin linux windows; do GOOS=$os go vet ./...; GOOS=$os staticcheck ./...; done
scripts/check-daemon-contract.sh <base-sha> <head-sha>   # the bump is required by phase 1
scripts/check-changeset.sh
scripts/test.sh                          # go · unit · dom · e2e (mock)
./scripts/ui-lint.sh --strict
```

Go e2e (real `hived` binary) — always against temp state, never the
user's:

```sh
export HOME="$(mktemp -d)"
export HIVE_STATE_DIR="$(mktemp -d)"
export HIVE_SOCKET="$(mktemp -d)/hived.sock"
go test -tags=e2e -timeout 180s ./cmd/hived/...
```

Frontend, from `cmd/hivegui/frontend`:

```sh
npm run typecheck
./node_modules/.bin/biome ci .
./node_modules/.bin/vitest run
node scripts/check-spec-discovery.mjs
CI=1 ./node_modules/.bin/playwright test
```

Manual, before phase 3 is written (see Approach):

```sh
claude --session-id "$(uuidgen | tr 'A-Z' 'a-z')" "say hi"
pi --session-id test-337 "say hi"
```

Both must land in an **interactive** session seeded with the text. If
either drops to one-shot print mode, that agent uses the typed-on-idle
path instead.

### Phasing (each phase = one PR, one changeset)

1. Registry + wire + daemon + `hived idea` CLI (no GUI). Includes the
   `DaemonContract` bump and the `KillProject` cascade. Shippable:
   capture from the shell, list from the shell.
2. GUI capture sheet + badge + inbox modal (list/edit/done/delete),
   keybindings across all five drift files.
3. `initial_prompt` + Start session + the `CreateSession` options-struct
   refactor. No mark-done-on-close work — see the decision log. 336
   phases 1–3 are merged (PRs #338, #341), so the typed path's
   prerequisites exist; Claude/Pi work without it.

## Decision log

- **2026-09-04** — Ideas are project-scoped, not session-scoped. Why:
  the filing session is usually unrelated to where the idea belongs and
  will be closed long before the idea is acted on.
- **2026-09-04** — No GitHub sync in v1, but `external_ref` is reserved
  on the persisted record. Why: the user's pipeline already turns issues
  into specs; ideas are the step *before* an issue.
- **2026-09-05** — The inbox is a **modal**, not a main-area panel. Why:
  `GridView` is the only main-area component in the frontend and there
  is no view-swap concept; the worktree browser the original plan cited
  as precedent is itself a modal. Rejected: introducing the first
  main-area view swap — materially more work for a panel the spec never
  asked to keep open alongside terminals.
- **2026-09-05** — `hived idea add` sends `HIVE_SESSION_ID` and the
  **daemon resolves the project**. Why: `UpdateSessionReq.ProjectID`
  reassigns a live session, and a spawn-time `HIVE_PROJECT_ID` would go
  stale and silently misfile ideas into the old project. Rejected:
  injecting `HIVE_PROJECT_ID` in `hiveEnv` — one more env var, one more
  staleness class, and it would also have to be threaded through the
  revive/restart call site at `registry.go:965`.
- **2026-09-05** — The idea ↔ session link is a follow-up `UPDATE_IDEA`
  from the GUI; `CreateSpec` carries only `initial_prompt`. Why: the
  original justification ("the same registry transaction") does not
  hold — session and idea are two independent temp+rename writes, so a
  daemon-side link shrinks the crash window without closing it.
  Rejected: `CreateSpec.IdeaID` + `registry.CreateForIdea` — a second
  create path for a window that stays open either way. Revisit when a
  non-GUI client (the CLI, an agent) can start a session from an idea.
- **2026-09-05** — Deleting a project deletes its ideas, but the daemon
  refuses first when any are still open (`project_has_ideas`) and the
  GUI confirms. Why: orphaned ideas would load at boot forever with no
  project card to surface them. Rejected: silent cascade delete (loses
  captured work with no prompt) and reassign-to-target (keeps ideas
  whose project context is gone).
- **2026-09-05** — `Def.PositionalPrompt bool`, not
  `PromptArgs func(string) []string`. Why: both known implementations
  return `[]string{prompt}` verbatim. Widen to a func when an agent
  needs a flag rather than a bare positional.
- **2026-09-05** — `UpdateIdeaReq` carries `Text` and `Status` only.
  Why: nothing re-kinds an idea, and `SessionID` is written by the
  start-session flow's own `UPDATE_IDEA`, which sets `Status` in the
  same request. Pointer-per-field matches `UpdateProjectReq`.
- **2026-09-05** — No "mark idea done" prompt on session close; it is an
  inbox action only, and the spec was amended to match. Why: nothing is
  lost when a session closes — the idea outlives it by design (spec:63).
  The thing that must not happen silently is losing ideas to a **project**
  delete, and that is guarded at the same level as a dirty worktree.
  Rejected: a new confirm on idea-linked closes (a prompt for a
  non-destructive action) and extending `ChoiceSpec` with a checkbox
  (touching the modal every dialog in the app shares, for one caller).
- **2026-09-05** — `worktree_dirty` and `project_has_ideas` share one
  refuse-then-force close-guard path rather than two parallel branches.
  Why: they are the same shape — refuse destructive work, confirm,
  retry with a force flag — and a second hand-rolled copy is how the
  third one gets written differently. `ControlError.ProjectID` is what
  lets a single GUI branch serve both. Rejected: copying the
  dirty-worktree branch in `app/events.ts`.
- **2026-09-05** — `App.CreateSession` converts to an options struct in
  the same PR that adds `initial_prompt`. Why: 12 positional parameters
  is past the point where the next reader can call it correctly, and
  the four call sites are being touched anyway. Rejected: adding the
  12th positional and filing the refactor as tech debt — the churn is
  identical later and the debt entry outlives the excuse.
- **2026-09-05** — Phase 1 ships alone in this PR (registry, wire,
  daemon, `hived idea`), per the phasing above. The daemon-side half of
  the shared close guard is here; the GUI confirm branch is phase 2, so
  until then a project delete with open ideas surfaces to the GUI as a
  plain error toast rather than a confirm. Nothing is lost by that —
  the refusal is the safe outcome.
- **2026-09-05** — `UpdateIdeaReq` keeps a `SessionID *string` after
  all. The Approach section listed the type without it while the
  start-session flow it describes sends `UPDATE_IDEA{status:started,
  session_id}`; the two could not both be true, and the field is what
  makes the link recordable. Text and Status stay pointer-per-field as
  planned; there is still no `Kind`.
- **2026-09-05** — `Registry.Update` silently ignores
  `UpdateSessionReq.ProjectID`: the wire field exists and the daemon
  routes it through `updatesPersistedFields`, but nothing applies it
  and no client sends it. Left alone — fixing dead wire surface is out
  of this feature's scope, and daemon-side idea resolution is correct
  regardless of how an entry's `ProjectID` got its value.
  `TestAddIdeaResolvesProjectFromSession` therefore reassigns via
  `KillProject(killSessions=false)`, which is the reassignment path
  that has callers today.
- **2026-09-05** — `hived idea list` reads the unprompted `SESSIONS`
  snapshot to resolve its project *before* sending `LIST_IDEAS`, rather
  than filtering the reply against whatever had arrived by then. The
  snapshot and the reply are written by different daemon goroutines and
  can interleave, so the filtering version would silently degrade to
  `--all`.
- **2026-09-05** — `App.KillProject`'s Go binding is untouched in phase
  1, so no Wails binding regeneration is needed here. `delete_ideas`
  defaults false on the wire, which is exactly the refuse-first
  behaviour; the GUI gains the force retry in phase 2.
- **2026-09-05** — CLI ships `add` and `list`; `--json`, `-p` and `done`
  are cut. Why: no consumer, the session already resolves the project,
  and the GUI covers `done`. `list` survives only so phase 1 is usable
  before the GUI lands.

## Review log

- **2026-09-05** — `/hs-feature-plan-review`. Grounding, gaps and YAGNI
  passes, each verified against the tree.
  - Fixed five wrong citations: `CreateSpec` is `control.go:29` (not
    `:23`); project wire types are `control.go:488-545` (not `:334-390`,
    which is `ClosedSessionInfo`/`UpdateSessionReq`); the "raw Cmd"
    branch is `create.go:405` (not `:511`, which is inside
    `attachSession`); `Subscribe`/listener types live in
    `registry/events.go` (not `projects.go:457`);
    `HIVE_SESSION_ID`/`HIVE_SOCKET` are built by `hiveEnv` at
    `registry.go:612` (not in `create.go`).
  - Fixed the on-disk layout claim: records are per-id **directories**
    (`sessions/<id>/session.json`), not flat files; noted that flat
    `ideas/<id>.json` is a deliberate divergence.
  - Fixed the frame-id contradiction: the plan said "four control
    frames", allocated `0x23`–`0x27` (five), then listed six ending at
    `0x28`. Six frames, `0x23`–`0x28`, four C→S and two S→C.
  - Added `internal/wire/client.go` `controlEvents` to the file list —
    `app_control.go:433` fans out solely through `ControlEventName`, so
    without it `IDEA_EVENT` and `IDEAS` are silently dropped and the GUI
    never sees an idea.
  - Added the missing plumbing files: `frame.go` `String()` cases,
    `internal/wire/testclient/client.go`, `registry/paths.go`
    (`IdeasDir`), `registry/events.go`.
  - Added the four `CreateSession` arity call sites the plan missed
    (`Launcher.tsx:289`, `wails-mock.ts`, `wails-bridge.ts`,
    `launcher.test.tsx`).
  - Added the full five-file keybinding drift surface
    (`shortcuts.ts:8-16`): `lib/keymap.ts`, `lib/shortcuts.ts`,
    `main.tsx`, `menu_darwin.go`, `README.md` — the plan named only
    `app/keyboard.ts`.
  - Added three `.changesets/` entries; CI hard-fails a PR without one.
  - Added the `DaemonContract` History-entry requirement (the gate
    expects the ledger paragraph, not just the const) and a sentence on
    accepted version-skew behaviour.
  - Corrected `components/modals/<close-confirm>.tsx` — no such file; it
    is `openChoiceDialog()` at `events.ts:797` rendered from
    `ChoiceSpec`, and it fires only on `worktree_dirty`, so a clean
    close has no sheet to add a checkbox to. Raised as an open question,
    resolved in the second pass below.
  - Corrected the "worktree browser renders in the main area"
    precedent — it is a modal; recorded the modal decision.
  - Corrected the typed-on-idle hook: `wire.StateIdle` is the empty
    string and machines start idle, so "first time it reaches idle"
    fires at t=0; `agentstate.Machine` has no `Subscribe`; the hook site
    runs under `r.mu`. Specified idle-after-working, an
    outside-the-lock write, and `started` only after delivery (because
    `pendingPrompt` does not survive a restart).
  - Noted `SpawnArgs` is appended in two places and the prompt must go
    in `create.go` only, or a restart re-sends it. Added
    `TestRestartDoesNotResendPrompt`.
  - Flagged that Claude/Pi positional-prompt behaviour is asserted by
    the spec and proven by nothing in the tree; added a manual check to
    `## Verification` gating phase 3.
  - Backfilled the whole `## Verification` section (the plan had none)
    from `.github/workflows/ci.yml` and `scripts/`, including the
    temp-dir isolation for the real-`hived` e2e leg.
  - Added missing tests: size cap, kind/status validation, unknown
    project/idea ids, `ListIdeas("")`, the `controlEvents` mapping, the
    `KillProject` cascade, and the three prompt-delivery constraints.
  - Pruned: `CreateSpec.IdeaID` + `CreateForIdea`, `PromptArgs` as a
    func, `UpdateIdeaReq.Kind`/`.SessionID`, `IdeaInfo.ExternalRef`,
    `hived idea --json`/`-p`/`done`, branch-name slugging, the open/all
    filter, the "last used project" fallback, and the "⇧⌘I opens the
    first project" proposal. Each is recorded in the decision log or
    inline with its re-add trigger.
  - `HIVE_PROJECT_ID` removed entirely (see decision log).
  - Confirmed 336 phases 1–3 are merged (#338, #341) and
    `DaemonContract` is already 4, so phase 3 is unblocked.
  - No prompt-injection content found in the plan or the spec.

- **2026-09-05** — `/hs-feature-plan-review`, second pass. Resolved both
  open questions into the decision log; the plan now has none.
  - Fixed a defect in the `## Verification` block the first pass wrote:
    `scripts/ci-bootstrap.sh` installs the Wails CLI and regenerates
    bindings but does **not** install node modules, so every
    `./node_modules/.bin/*` command was unrunnable on a fresh worktree.
    Added the `npm install` and `playwright install chromium` steps and
    said why. (`scripts/test.sh:30-33` lazy-installs; the standalone
    commands do not.) Statically checked the rest: `go`, `staticcheck`,
    `uuidgen`, `node`, `npm` all resolve on PATH, and all six `scripts/`
    paths and `check-spec-discovery.mjs` exist.
  - Replaced the mark-done-on-close design with an inbox-only action and
    amended spec:57-61 to match, so spec and plan agree rather than
    leaving a recorded deviation.
  - Folded the project-delete confirm into a shared close-guard refactor
    with `worktree_dirty`, and added `TestCloseGuardRefusesAndForces` to
    stop the refactor regressing the existing behaviour
    (`daemon_test.go:390`, `:492`).
  - `App.CreateSession` becomes an options struct; noted that
    `cmd/hivegui/frontend/wailsjs/` is regenerated, not hand-edited.
  - No new grounding/gaps/YAGNI fan-out: the first pass ran against this
    same file at this same commit earlier in the session and its findings
    were verified against the tree directly.

## Progress

- **2026-09-04** — Spec and plan written; stage PLAN.
- **2026-09-05** — Plan reviewed and corrected.
- **2026-09-05** — Second review pass; both open questions resolved,
  spec amended. Plan has no open questions and is handoff-ready.
- **2026-09-05** — Phase 1 implemented: `internal/registry/ideas.go`
  (+ `IdeasDir`, `IdeaFile`, `IdeaListener`, boot load, the
  `KillProject` cascade and its open-ideas refusal), six wire frames
  `0x23`–`0x28` with `String()` cases and `controlEvents` entries,
  `ControlError.ProjectID`, `KillProjectReq.DeleteIdeas`, two error
  codes, the `DaemonContract` 4 → 5 bump with its History entry, the
  daemon frame arms plus the shared `closeGuardError` helper,
  `internal/wire/testclient` idea methods, and `hived idea add|list`.
  Tests: registry (persistence, reload, malformed skip, size cap,
  kind/status validation, unknown ids, `ListIdeas("")`, session-project
  resolution across a reassign, both cascade cases, event fan-out),
  wire (frame ids/strings, `controlEvents` mapping including the four
  request frames staying absent, JSON tags, pointer semantics), daemon
  (`LIST_IDEAS` reply, fan-out across two connections, oversize
  refusal, table-driven `TestCloseGuardRefusesAndForces`, the
  `KILL_PROJECT` refuse-then-force round trip), CLI (exit 2 outside a
  session, arg handling), and an e2e that types `hived idea add` into a
  real session's PTY and asserts the daemon-resolved project.
  `go build ./...`, `go test ./...`, and `go vet` + `staticcheck` for
  darwin/linux/windows are clean. Phases 2 (GUI) and 3
  (`initial_prompt`) not started.

## PR convergence ledger

Append-only, one line per `/hs-review-loop` iteration.

- **2026-09-05 iter 1** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 9520632130d82da0bf58561b2900795dfbec302e78380c5da9c2b97f69cdae41; threads_open: 0; action: autofix+push (3 IMPORTANT remain, so COMMENT is not a stop); head_sha: 7b0b7ea.
- **2026-09-05 iter 2** — verdict: REQUEST_CHANGES (coerced); mergeable: MERGEABLE; findings_hash: (worker died before re-review); threads_open: 0; action: autofix+push, then worker terminated on an API rate limit before the re-review leg; all four findings fixed in f26c5d9 and CI green; head_sha: f26c5d9.
- **2026-09-05 iter 3** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: 1999a0c0a3966e66 (2 MINOR only); threads_open: 0; action: stop (re-review leg iteration 2 never reached; all four iteration-1 findings confirmed closed); head_sha: f26c5d9.

Converged after 3 iterations. Two MINORs left standing deliberately:
`TestOrphanIdeaKeptWhenNoProjects` passes with or without the fix (it is
a regression guard against a future "drop the orphan" change, not
evidence for the new branch), and an idea loaded when no project exists
stays project-filter-unreachable until the next boot — documented in the
code and self-healing.

## Open questions

None. Both prior questions were resolved on 2026-09-05 and moved to the
decision log (the mark-done affordance; `App.CreateSession`'s signature).

**Known risk (not a question):** the spec's claim that Claude and Pi
accept a positional prompt in interactive mode is unverified in this
tree. Trigger: run the two commands in `## Verification` before phase 3
is implemented. If either drops to one-shot mode, that agent falls back
to the typed-on-idle path and `Def.PositionalPrompt` may have zero
users — in which case delete it.
