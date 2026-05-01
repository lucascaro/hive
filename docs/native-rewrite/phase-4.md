# Phase 4 — Projects + grid

**Status:** In progress
**Branch:** `silent-light`
**Inputs:** Phase 1–3 working at `6020f8d`

## Why projects first

Sessions today are flat. In old (tmux-era) Hive, a "project" groups
sessions sharing a working directory + identity (think: monorepo
front-end and back-end live as separate sessions but feel like one
unit). The user wants this back, and wants the grid view scoped to a
project — so projects need to land before grid.

After Phase 4:

> 1. Two projects exist (`hive` rooted at `~/projects/hive`, `dotfiles`
>    rooted at `~/.dotfiles`). Each project has 2-3 sessions.
> 2. Sidebar shows them as a tree: project headers with sessions
>    nested underneath.
> 3. A grid view tiles all sessions of one project — or all sessions
>    everywhere — and ⌘← / ⌘→ moves the grid focus to the next
>    project.
> 4. Quit GUI, relaunch — projects, sessions, names, colors, cwds
>    all preserved.

## Out of scope

- Drag-to-move sessions between projects (Phase 5; metadata is in place
  but UX adds complexity)
- Splits inside a single grid cell (Phase 5)
- Project-level templates / hooks (Phase 5)
- Daemon-restart resume of scrollback (still Phase 1.7)

## Data model

```
Project {
  id        string    // uuid
  name      string    // display
  color     string    // sidebar accent
  cwd       string    // default working dir for new sessions
  order     int
  created   time.Time
}

Session: existing fields + project_id string  // empty = unassigned (legacy)
```

A bootstrapped "default" project exists if none on disk; sessions
without a `project_id` are assigned to it.

## Wire protocol delta (still v1; frames added monotonically)

| Code | Name             | Mode    | Direction | Payload                                       |
|------|------------------|---------|-----------|-----------------------------------------------|
| 0x0d | LIST_PROJECTS    | control | C → S     | `{}`                                          |
| 0x0e | PROJECTS         | control | S → C     | `{ "projects": [ProjectInfo, ...] }`          |
| 0x0f | CREATE_PROJECT   | control | C → S     | `{ "name", "color", "cwd" }`                  |
| 0x10 | KILL_PROJECT     | control | C → S     | `{ "project_id", "kill_sessions": bool }`     |
| 0x11 | UPDATE_PROJECT   | control | C → S     | `{ "project_id", name?, color?, cwd?, order? }`|
| 0x12 | PROJECT_EVENT    | control | S → C     | `{ "kind": added\|removed\|updated, "project": ProjectInfo }` |

Existing types extended:

- `CreateSpec.ProjectID` — when set, daemon assigns the new session
  to that project and uses the project's `cwd` if `spec.Cwd` is empty.
- `SessionInfo.ProjectID` — present on every session sent to clients.
- `UpdateSessionReq.ProjectID` — reassign session to a different
  project (Phase 5; defined now to avoid a wire bump later).

`KILL_PROJECT.kill_sessions=false` reassigns sessions to the default
project; `true` kills them.

## Persistence

```
StateDir/
  index.json                  # global ordering: { project_order: [...] }
  projects/
    <pid>/
      project.json            # { id, name, color, cwd, order, created }
  sessions/
    <sid>/
      session.json            # adds project_id field
      scrollback.log          # (still Phase 1.7)
```

The session-side `index.json` from Phase 2 stays for compatibility
loading; new persistence writes session order *within a project* via
each session's `order` field, and project order via the top-level
`index.json`. Migration on startup: any session without `project_id`
gets assigned to the default project.

## Daemon dispatch

Control connection now handles 6 more frame types. Project events
broadcast on the same listener channel as session events. Bootstrap
order on `daemon.New`:

1. Open registry (loads projects + sessions).
2. If no projects exist, create a "default" project rooted at the
   daemon's `--cwd`.
3. Migrate sessions without `project_id` → assigned to default.
4. Revive sessions with no live PTY (existing behavior).

## GUI

### Sidebar tree

```
+------------------------------+
|  Hive                    [+] |   ← global new (creates session in
|------------------------------|     active project)
|  ▼ ■ hive                ⊕  |   ← project header, ⊕ = new session here
|       • amber-falcon claude |
|       • still-meadow shell  |
|  ▶ ■ dotfiles            ⊕  |
|------------------------------|
|  hints …                     |
+------------------------------+
```

- Project header: collapse chevron + color block + name + per-project
  new button.
- Active session highlighted as before; active project has a slight
  background tint so the grid focus is visible.
- Right-click / context menu (or +Shift gesture) on project: rename,
  recolor, change cwd, archive.
- New-project button at the top (next to brand) opens a small inline
  editor: name + color picker + cwd field (cwd defaults to GUI launch
  dir).

### Grid view

Toggle via ⌘G. Two scopes:

- **Per-project grid** (default): tiles every alive session of the
  active project. Each tile shows the session's name, color stripe,
  and a live xterm. Click a tile to make it the focused session
  (⌘1..⌘9 still work).
- **All sessions grid**: ⇧⌘G. Same layout, but tiles every session
  across every project, grouped visually by project color band.

⌘← / ⌘→ in grid mode shifts focus to the previous/next project. In
the regular single-pane mode, the existing behavior (switch session)
is preserved — grid mode owns those keys only when active.

Tile sizing: simple `auto-fill` CSS grid with a min-tile size of
360x220. The tile is a thin frame; the active tile is highlighted
with a bright border in the project's color.

## Milestones

| #   | Goal | Done when |
|-----|------|-----------|
| 4.1 | Wire delta (project frames + ProjectID) | round-trip tests for every new frame |
| 4.2 | `internal/registry` project type + persistence | unit tests for create/kill (with and without session reassignment), update, persistence-across-Open |
| 4.3 | Daemon dispatches project frames; bootstrap default project | E2E test creates project, creates session in it, kills project, sessions move to default |
| 4.4 | GUI sidebar tree (read-only first) | manual smoke; sessions render under their project |
| 4.5 | GUI sidebar tree CRUD | new project, rename, recolor, change cwd; new session "+" per project; manual smoke |
| 4.6 | GUI grid view per-project | ⌘G toggles; tiles render; click switches focus |
| 4.7 | GUI grid view all-sessions; ⌘←/→ project nav | ⇧⌘G toggles between scopes; arrow nav works |
| 4.8 | **Acceptance: 2 projects survive restart** | manual on macOS |

## Risks

| Risk | Mitigation |
|------|------------|
| Many xterm instances tile-rendered = layout pain (fonts, fit) | Each tile gets its own FitAddon; refit on grid layout change; only attach connections for visible tiles |
| Existing v1 hivegui clients stop working when daemon adds project frames | Frames are added monotonically — old clients ignore them. SessionInfo gets a new field; older clients ignore unknown JSON fields. |
| Bootstrapping a default project changes file layout for users from Phase 3 | One-time migration on Open: create default, assign orphans. Idempotent. |

## What this unblocks

- Phase 5 (workflows, agent teams): a workflow runs in the context of
  a project, gets the project's cwd by default, can address its
  sessions by role.
- Phase 6 (release): sidebar UX is largely settled; remaining work is
  packaging and signing.
