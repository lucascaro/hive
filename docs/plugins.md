# Writing a Hive plugin

A Hive plugin extends Hive in one of two ways, or both:

- **In the background.** A program Hive runs for you. It sees what every
  session is doing — which agent started working, which one is waiting
  on you, which one exited — and can act: rename a session, type into
  it, start or stop one, file an idea. A webhook when an agent needs
  you, a bridge to a chat app, a rule that restarts a crashed agent.
- **In the app.** A JavaScript module the Hive app loads, which adds
  to the app itself: a view for a session, commands in the command
  palette, a badge on sidebar rows, and a section in Settings. See
  **[App surfaces](#app-surfaces)**.

Plugins are how features that are too specific for Hive itself get
built.

This page is the contract. The two reference plugins are written
against it and nothing else: [`plugins/webhook/`](../plugins/webhook/)
runs in the background, and
[`plugins/session-notes/`](../plugins/session-notes/) lives in the
app.

> **Plugin API 0.2 — experimental.** Until the plugin API reaches 1.0 it
> is best-effort: any Hive release may change it, and a plugin is only
> loaded by a Hive that implements exactly the `api_version` it names.
> From 1.0 on, compatibility follows semantic versioning. What changed in
> each version is under **[API versions](#api-versions)**.

## Trust

**A plugin runs as you, with everything you can do.** There is no
sandbox. It can read and write your files, reach the network, start
programs, and — through Hive — type into any session, including ones
running agents with access to your code and credentials. Install
plugins you would be comfortable running yourself.

An app plugin is no different: its module runs inside the Hive app,
with the app's full access — every session's screen, every action the
app can take — and it can change what the app shows you.

Because of that, Hive installs every plugin **disabled**. Nothing a
plugin contains runs until you enable it, and the Hive app asks you
first, showing the plugin's name, source and the command it will run
(or the module it will load into the app).
That prompt is the app's; the protocol itself does not ask anyone. Any
wire client — including an enabled plugin, which has full trust — can
install, enable and remove plugins directly, just as it could run any
program you can.

## Layout

A plugin is a directory with a manifest, `hive-plugin.json`, at its
root:

```json
{
  "id": "webhook",
  "name": "Webhook",
  "version": "0.1.0",
  "api_version": "0.2",
  "description": "POSTs a JSON message to a URL whenever a session starts waiting on you.",
  "main": { "command": ["node", "main.mjs"] }
}
```

| Field | Required | Meaning |
|-------|----------|---------|
| `id` | yes | Lowercase letters, digits and hyphens, up to 63 characters. Unique among installed plugins. |
| `name` | yes | Shown to the user, including in the install prompt. `name`, `version`, `description` and the `main.command` arguments may not contain control characters, invisible formatting characters (bidi overrides and isolates, zero-width characters) or line separators. |
| `version` | no | Your plugin's own version. |
| `api_version` | yes | The plugin API you target. Must be `0.2` for this Hive. |
| `description` | no | One sentence, shown to the user. |
| `main.command` | one of `main`, `ui` | The program and arguments Hive runs, in the plugin directory. A first element starting with `./` is relative to the plugin; anything else is looked up on Hive's `PATH`. |
| `ui.entry` | one of `main`, `ui` | The ES module the app loads (`.js` or `.mjs`), relative to the plugin directory. See [App surfaces](#app-surfaces). |
| `ui.style` | no | A stylesheet (`.css`) the app links while the plugin is loaded. |

A plugin needs `main`, `ui`, or both. `ui` paths must stay inside the
plugin directory: no `..`, no leading `/`, no URL.

Any language works. The reference plugin uses Node (18 or newer) and
the SDK below.

## Installing

Users manage plugins in the Hive app under **Settings → Plugins**:
paste a folder path or git URL and press Install, and Hive shows what the
plugin is, where it came from and the exact command it will run, with a
full-privileges warning. Accepting enables it; cancelling removes it
again. Turning a plugin on from the tab's toggle shows the same trust
prompt; turning it off acts at once, and removing it asks you to confirm
first. None of these needs a
daemon restart. It sends the same `INSTALL_PLUGIN` /
`SET_PLUGIN_ENABLED` / `REMOVE_PLUGIN` requests described below, so any
other wire client can do the same.

A plugin is installed from either:

- **a local directory** — an absolute path (or `~/…`). Hive copies it,
  skipping `.git` and symlinks, so later edits to the original do not
  reach the installed copy. Reinstall to pick them up.
- **a git URL** — `https://`, `ssh://`, `git://`, `file://`, or
  `user@host:path`. Hive makes a shallow clone and **pins the commit it
  cloned**; it never pulls. Reinstall to update. Git and ssh never
  prompt: a repository that needs a password or an unknown host key
  fails the install instead.

Installing an id that is already installed is refused; remove it first.

### Bundled plugins

Some plugins ship inside Hive. Today that is **Plan review**
([plugins/plan-review](../plugins/plan-review)), which shows an agent's
plan in the app before it runs. `hived` writes a bundled plugin into its
state dir on every start, so it always matches the Hive you run, and
lists it with `"source": "builtin"` and `"builtin": true`. It starts
disabled like any other plugin. It can be enabled and disabled (the app
does not ask for trust first: it is part of Hive), but not removed, and
installing another plugin under its id is refused. A bundled plugin is
written against this document only; it uses nothing a third-party
plugin cannot.

## Lifecycle

| Status | Meaning |
|--------|---------|
| `stopped` | Installed and disabled (or enabled but not started yet). Never running. |
| `running` | The process is alive. A plugin with only a `ui` has no process: it is `running` whenever it is enabled. |
| `crashed` | The process exited; Hive restarts it after a backoff that doubles from 1s up to 30s. |
| `failed` | It exited more than 5 times within 60s. Hive stops restarting it until you enable it again. |
| `refused` | It cannot run as installed: its `api_version` is not this Hive's, or its command is not on `PATH`. `status_detail` says which. |

Any exit while enabled counts as a crash — a plugin is expected to run
until Hive stops it. On disable, remove or daemon shutdown Hive sends
the plugin's process group `SIGTERM`, waits 3 seconds, then kills the
whole group (on Windows, `taskkill /T`). Do not daemonise or detach
child processes; they would escape that.

**Exit when your connection closes.** Hive starts a fresh copy whenever
it needs one, on a fresh socket. A plugin that reconnects on its own
would, after a daemon restart, run twice. The SDK does this for you.

## Environment

| Variable | Value |
|----------|-------|
| `HIVE_SOCKET` | The plugin's own socket for this run. Connect here. |
| `HIVE_PLUGIN_ID` | The plugin's id. |
| `HIVE_PLUGIN_API` | The plugin API version Hive implements. |
| `HIVE_PLUGIN_DIR` | The installed copy. Treat it as read-only; it is replaced on reinstall. |
| `HIVE_PLUGIN_DATA_DIR` | Your data directory. Put configuration and state here. It survives removing and reinstalling the plugin. |

Everything else in the environment is inherited from the daemon —
including `PATH` and anything secret you exported in the shell that
started Hive — except Hive's own `HIVE_SESSION_ID` and `HIVE_PLUGIN_*`
variables.

The working directory is `HIVE_PLUGIN_DIR`. Everything the plugin
writes to stdout and stderr goes to `plugin.log` in its data directory
(capped at 1 MiB, with one rotated `plugin.log.1`).

Hive's data directory is `plugin-data/<id>/` under its state directory
(`~/Library/Application Support/Hive` on macOS, `$XDG_STATE_HOME/hive`
or `~/.local/state/hive` on Linux, `%LOCALAPPDATA%\Hive` on Windows).

## The protocol

A plugin is an ordinary Hive client: it speaks the same wire protocol
as the Hive app, on the socket in `HIVE_SOCKET`. Every connection to
that socket — including ones opened by programs your plugin starts — is
treated as your plugin.

Each frame is a 1-byte type, a 4-byte big-endian payload length, and
the payload (at most 1 MiB): JSON for everything except `DATA`, which
carries raw terminal bytes. Field names are `snake_case`.

A connection opens with `HELLO`:

```json
{ "version": 1, "client": "plugin/webhook", "mode": "control" }
```

and the daemon answers `WELCOME` (or `ERROR`). Three modes are served
on a plugin socket:

- **`control`** — the management connection. Right after `WELCOME` the
  daemon sends a `PROJECTS` and a `SESSIONS` snapshot, then every
  broadcast below as it happens. Requests have no ids: a reply is the
  next frame of its type, and a failure is an `ERROR`.
- **`attach`** (`"session_id": "…"`) — one session's terminal. The
  daemon replays the scrollback, then streams output as `DATA`; send
  `DATA` to type and `RESIZE` (`{ "cols", "rows" }`) to resize.
- **`create`** (`"create": { … }`) — create a session and attach to it
  in one step.

### Everything a plugin can do

A plugin can send every control request the Hive app can. The request
and reply payloads are the Go structs of the same name in
[`internal/wire/`](../internal/wire/control.go).

| Request | Payload | Answered by |
|---------|---------|-------------|
| `LIST_SESSIONS` | `ListSessionsReq` | `SESSIONS` |
| `CREATE_SESSION` | `CreateSpec` | `SESSION_EVENT` (added) |
| `KILL_SESSION` | `KillSessionReq` | `SESSION_EVENT` (removed) |
| `UPDATE_SESSION` | `UpdateSessionReq` | `SESSION_EVENT` |
| `RESTART_SESSION` | `RestartSessionReq` | `SESSION_EVENT` |
| `RESTORE_SESSION` | `RestoreSessionReq` | `SESSION_RESTORED`, `CLOSED` |
| `LIST_CLOSED` | `ListClosedReq` | `CLOSED` |
| `LIST_PROJECTS` | `ListProjectsReq` | `PROJECTS` |
| `CREATE_PROJECT` | `CreateProjectReq` | `PROJECT_EVENT` (added) |
| `KILL_PROJECT` | `KillProjectReq` | `PROJECT_EVENT` (removed) |
| `UPDATE_PROJECT` | `UpdateProjectReq` | `PROJECT_EVENT` |
| `LIST_WORKTREES` | `ListWorktreesReq` | `WORKTREES` |
| `REMOVE_WORKTREE` | `RemoveWorktreeReq` | `WORKTREES` |
| `CREATE_WORKTREE` | `CreateWorktreeReq` | `WORKTREES` |
| `RENAME_WORKTREE` | `RenameWorktreeReq` | `WORKTREES` |
| `DELETE_BRANCH` | `DeleteBranchReq` | `WORKTREES` |
| `SET_WORKTREE_LABEL` | `SetWorktreeLabelReq` | `PROJECT_EVENT` |
| `LIST_IDEAS` | `ListIdeasReq` | `IDEAS` |
| `ADD_IDEA` | `AddIdeaReq` | `IDEA_EVENT` (added) |
| `UPDATE_IDEA` | `UpdateIdeaReq` | `IDEA_EVENT` |
| `REMOVE_IDEA` | `RemoveIdeaReq` | `IDEA_EVENT` (removed) |
| `RESOLVE_PROMPT` | `ResolvePromptReq` | `SESSION_EVENT` |
| `RESOLVE_WORKTREE_CHOICE` | `ResolveWorktreeChoiceReq` | `SESSION_EVENT` |
| `GET_ACTIVITY` | `GetActivityReq` | `ACTIVITY` |
| `SEARCH_TRANSCRIPT` | `SearchTranscriptReq` | `TRANSCRIPT_MATCHES` |
| `GET_TRANSCRIPT_LINES` | `GetTranscriptLinesReq` | `TRANSCRIPT_LINES` |
| `GET_PLAN_REVIEW` | `GetPlanReviewReq` | `PLAN_REVIEW` |
| `RESOLVE_PLAN_REVIEW` | `ResolvePlanReviewReq` | `SESSION_EVENT` |
| `CLIENT_COMMAND` | `ClientCommand` | `CLIENT_BROADCAST` |
| `SHUTDOWN` | empty | the daemon exits |
| `LIST_PLUGINS` | empty | `PLUGINS` |
| `INSTALL_PLUGIN` | `InstallPluginReq` | `PLUGIN_EVENT` (added) |
| `SET_PLUGIN_ENABLED` | `SetPluginEnabledReq` | `PLUGIN_EVENT` |
| `SET_PLUGIN_CONFIG` | `SetPluginConfigReq` | `PLUGIN_EVENT` (updated) |
| `SET_CLIENT_UI` | `SetClientUIReq` | nothing. The Hive app's own announcement of which plugin UIs it runs; ignored from a plugin |
| `REMOVE_PLUGIN` | `RemovePluginReq` | `PLUGIN_EVENT` (removed) |
| `GET_ACP_TRANSCRIPT` | `GetAcpTranscriptReq` | `ACP_TRANSCRIPT` (snapshot) |
| `PROMPT_ACP` | `PromptAcpReq` | `ACP_TRANSCRIPT`, `SESSION_EVENT` (state). The turn's origin is recorded as `plugin:<id>` |
| `ANSWER_PERMISSION` | `AnswerPermissionReq` | `ACP_TRANSCRIPT` |

And everything the daemon sends on a control connection:

| Frame | When |
|-------|------|
| `WELCOME` | Handshake accepted. |
| `ERROR` | A request failed (`code`, `message`). |
| `SESSIONS` | Snapshot after `WELCOME`; answer to `LIST_SESSIONS`. |
| `SESSION_EVENT` | Broadcast. `kind` is `added`, `removed`, `updated`, `title`, `attention` or `state`; `session` is the whole `SessionInfo`. |
| `PROJECTS` | Snapshot after `WELCOME`; answer to `LIST_PROJECTS`. |
| `PROJECT_EVENT` | Broadcast: a project was added, removed or updated. |
| `WORKTREES` | A project's worktree inventory. |
| `CLOSED` | The sessions that can be reopened. |
| `SESSION_RESTORED` | A reopen succeeded. |
| `CLIENT_BROADCAST` | Broadcast: a relayed `CLIENT_COMMAND`. |
| `IDEAS` | Answer to `LIST_IDEAS`. |
| `IDEA_EVENT` | Broadcast: an idea was added, removed or updated. |
| `ACTIVITY` | Broadcast: what an agent is doing inside its turn (tools, plan). |
| `TRANSCRIPT_MATCHES` | Answer to `SEARCH_TRANSCRIPT`. |
| `TRANSCRIPT_LINES` | Answer to `GET_TRANSCRIPT_LINES`. |
| `PLAN_REVIEW` | Answer to `GET_PLAN_REVIEW`. |
| `PLUGINS` | Answer to `LIST_PLUGINS`. |
| `PLUGIN_EVENT` | Broadcast: a plugin was installed, removed, or changed status. |
| `ACP_TRANSCRIPT` | Answer to `GET_ACP_TRANSCRIPT` (`reset`), and broadcast as an ACP session's transcript changes. |

The two things to watch most plugins need are `SESSION_EVENT`'s `state`
(`working`, `waiting_input`, `waiting_permission`, `exited`, `error`,
or empty for idle) and `needs_attention` (the terminal rang its bell).

## Limits

A plugin is automation, and two things follow from that. Both are
guard rails against a *buggy* plugin — a runaway loop, a stuck reader —
not a security boundary: a plugin runs as you, so a hostile one could
simply connect to Hive's main socket as the app does. See **Trust**.

- **It never counts as someone who can answer a question.** When a
  worktree fails to set up or an agent asks for its plan to be
  reviewed, Hive only waits if a person could see the question. A
  connected plugin does not count, whatever it calls itself. For a plan
  review, only an app window running the bundled plan-review plugin's
  UI counts: the app announces that with `SET_CLIENT_UI`, which Hive
  ignores from a plugin.
- **It has a rate budget.** Cheap reads cost 1, changes that are
  broadcast to every client cost 10, and anything that starts or
  destroys something (creating, restarting or killing a session,
  worktree changes, installing a plugin) costs 100, against 1000 per
  second with a burst of 2000. Past it, Hive stops reading the plugin's
  requests until the budget refills — nothing is dropped, requests just
  slow down. Requests that are broadcast also wait, for up to two
  seconds, while any connected app window is behind on the updates
  already sent to it, so a plugin cannot flood a window off its
  connection. Plugins take turns to broadcast, so a plugin whose own
  requests are slow to finish can briefly delay other plugins'
  broadcasts; it never delays the app's windows or your sessions.

A connection that stops reading what Hive sends it is disconnected,
the same as any client.

## The SDK

[`plugins/sdk/hive-plugin.mjs`](../plugins/sdk/hive-plugin.mjs) is a
single dependency-free file for Node 18+. **Copy it into your plugin**
next to your entry point — it is deliberately not a package, so an
install from a git URL needs no build or `npm install`.

```js
import { connect } from './hive-plugin.mjs';

const hive = await connect(); // HIVE_SOCKET, HELLO, WELCOME

hive.on('SESSION_EVENT', (ev) => {
  if (ev.session.state === 'waiting_input') {
    console.log(`${ev.session.name} is waiting`);
  }
});

hive.send('ADD_IDEA', { project_id: '…', text: 'from a plugin' });

const term = await hive.attach(sessionId); // type into a session
term.write('echo hello\n');
term.on('data', (buf) => process.stdout.write(buf));
```

`connect()` emits every frame by name with its decoded payload, and
`send(name, payload)` sends any request in the table above. Under Hive
it exits the process when the connection closes, as required above.
Typed shapes for the main payloads are in the file's JSDoc.

## App surfaces

A plugin with a `ui` entry adds to the Hive app. Its module is loaded
into the app when the plugin is enabled and unloaded when it is
disabled or removed; a reinstall loads the new copy. The module's
default export is an `activate` function. Hive calls it once, with a
`hive` object, and it returns what the plugin contributes:

```js
export default function activate(hive) {
  const h = hive.React.createElement;
  return {
    commands: [{ id: 'hello', title: 'Say hello', keys: { key: 'L', shift: true }, run: () => {} }],
    badge: (session) => (session.name === 'main' ? { text: 'Main' } : null),
    sessionView: {
      modal: { title: 'Hello', component: ({ session, close }) => h('p', null, `Hi ${session.name}`) },
      panel: { title: 'Hello', component: ({ session }) => h('p', null, session.name) },
      banner: (session) => null,
    },
    settings: () => h('p', null, 'Nothing to set'),
    deactivate() {},
  };
}
```

Every field is optional. `activate` may be `async`.

### The `hive` object

| Member | What it is |
|--------|------------|
| `apiVersion` | `"0.2"`. |
| `pluginId` | Your plugin's id. |
| `React` | The app's React (19). Build components with it — `hive.React.createElement`, hooks — and never bundle your own copy: two Reacts on one page break hooks. |
| `components` | App components you may render: `Button` (`{ label, kind?: 'primary', onClick, id? }`), `Kbd` (a key hint), `Markdown` (`{ source }`: renders markdown safely — no raw HTML). |
| `useSessions()` / `getSessions()` | Every session, as the app sees it (the wire `SessionInfo`). The `use` form is a hook that re-renders on change. |
| `subscribeSessions(callback)` | Calls `callback(sessions)` on every change to the session list, for code outside a component. Returns an unsubscribe function; it also ends when the plugin unloads. |
| `useActiveSessionId()` / `getActiveSessionId()` | The focused session's id, or `null`. |
| `on(event, callback)` | Subscribes to an app event and returns an unsubscribe function. Events are the app's own names for daemon broadcasts — `session:event`, `project:event`, `idea:event`, `plugin:event` and the rest — with the payload already parsed. Subscriptions end when the plugin unloads. |
| `actions.switchTo(sessionId)` | Focuses a session. |
| `actions.getPlanReview(sessionId, reviewId)` | Asks for the plan text of a session's pending plan review (its `pending_plan_review.review_id`). The answer arrives as a `planreview:plan` event, or a `control:error` event with code `plan_review_stale` when that review is already over. |
| `actions.resolvePlanReview({ session_id, review_id, decision, comments?, feedback? })` | Answers a pending plan review: `decision` is `approve` or `deny`, `comments` is `[{ quote, text }]`. Resolves once sent. The daemon parks a review only while some window runs the bundled plan-review plugin's UI; see **Limits**. |
| `actions.externalPlanReviewers()` | Resolves to the other tools set up to review Claude's plans: `[{ kind: 'settings' \| 'plugin', id, active }]`. |
| `settings.get()` / `settings.use()` / `settings.set(object)` | Your plugin's settings, a JSON object of up to 64 KiB. `set` replaces the whole object; it is shown at once and saved in the background. See below. |
| `openSessionView(sessionId, props?)` | Opens your modal for a session. Resolves `'closed'` when your component calls `close()`, or `'dismissed'` when the user pressed Escape, the view was replaced, or the plugin unloaded. |
| `closeSessionView()` | Closes your modal if it is open. |
| `togglePanel()` | Opens or closes your panel beside the terminal. It shares the activity inspector's column: opening one closes the other. |

For anything else — creating sessions, sending input, filing ideas —
give your plugin a `main` process too and use the wire protocol.

### What a plugin contributes

- **`sessionView.modal`** — `{ title, component, hints? }`. A dialog for
  one session, opened with `openSessionView`. `title` is a string, or
  `(session, props) => string` for one that names the session.
  `component` gets
  `{ session, props, close }`. Hive draws the dialog frame, the title
  and the Escape handling; `hints` lists key hints shown in its footer
  (default `[esc] close`). Put your own buttons in the body.
- **`sessionView.panel`** — `{ title, component }`. A panel beside the
  terminal for the focused session, toggled with `togglePanel`.
  `component` gets `{ session }`.
- **`sessionView.banner`** — `(session) => { text, action? } | null`.
  A one-line bar above the status bar while that session is focused;
  `action` is `{ label, run }`. One plugin banner shows at a time.
- **`commands`** — `[{ id, title, keys?, run }]`. Listed in the command
  palette. `keys` is `{ key, shift? }`: the platform modifier (⌘ on
  macOS, Ctrl elsewhere), optionally Shift, and one letter or digit.
  Hive shows the chord in the palette and in the ⌘/ shortcuts overlay.
  **Hive's own shortcuts always win:** a chord Hive already uses is
  refused (the command stays, without a key), and if two plugins want
  the same chord the plugin whose id sorts first keeps it.
- **`badge`** — `(session) => { text, title?, tone? } | null`. A short
  marker on the session's sidebar row. `tone` is `'neutral'`, `'info'`
  or `'warn'`. Keep `text` to a word; it is cut at 12 characters.
- **`settings`** — a component rendered under your plugin's row in
  **Settings → Plugins**. Enter inside it is yours: Settings does not
  treat it as save-and-close.
- **`deactivate()`** — called when the plugin unloads. Clear timers
  here.

`badge` and `banner` are recomputed when sessions or your settings
change. Keep them cheap: they run while the sidebar renders.

### Settings

A plugin's settings live in `ui-config.json` in its data directory, so
they survive removing and reinstalling the plugin. They are sent to
every app window, which is how two windows stay in step: a change in
one reaches the other. Only UI plugins have them — Hive never reads,
sends or overwrites a plugin's own `config.json`, which is where
background plugins keep things like tokens. `settings.set` shows the
new value at once; if Hive cannot save it, the value goes back to the
saved one. Writes are coalesced: calling `set` in a loop saves the
latest value, not every one.

### Styling

Your stylesheet is loaded into the app page, so it is global. Prefix
every class with your plugin id (`.session-notes-…`) and use Hive's
theme tokens — `var(--fg)`, `var(--fg-muted)`, `var(--surface)`,
`var(--surface-raised)`, `var(--border)`, `var(--space-1)` to
`var(--space-6)`, `var(--text-xs)` to `var(--text-lg)`,
`var(--radius-sm)`, `var(--state-info)`, `var(--state-attention)`,
`var(--state-error)` — so your plugin follows the theme the user
picked. Do not hard-code colours or pixel font sizes.

### When a plugin breaks

The app contains a plugin that misbehaves. If its module does not load
within 10 seconds, `activate` does not finish within 5, a component it
rendered throws or stays suspended for 5 seconds, or one of its
callbacks throws, Hive unloads it from the app and shows **Stopped in
the app** with the reason in its row in **Settings → Plugins**, next to
a **Disable** button. The rest of the app carries on. It stays stopped
until it is disabled and enabled again, or reinstalled. Your module
runs on the app's main thread, so a synchronous endless loop freezes
the app like any other page script would — don't block.

### Testing an app plugin

Install it by directory into an isolated Hive (`scripts/dev-iso.sh`)
and open the app against it. Hive serves your files from the installed
copy, so edit the original, then reinstall to pick the change up.

## Testing a plugin

Run it against an isolated Hive, never your real one:
`scripts/dev-iso.sh` starts a throwaway daemon with its own socket and
state directory. Install your plugin there by directory, enable it, and
watch `plugin.log` in its data directory.

## API versions

This page is the single source of truth for the plugin API: the
manifest, the environment, the wire protocol as a plugin sees it, the
SDK and the `hive` object. When the API changes, this page changes in
the same pull request, and so does this section.

**How the version moves.** Until 1.0, a manifest's `api_version` must
equal the version Hive implements exactly, so changing the version refuses every
installed plugin until its author updates it. The version therefore
changes only when a plugin written for the previous one could break:
a frame, field, `hive` member or manifest field removed or renamed, or
one whose meaning changed. Additions — a new frame, a new field, a new
`hive` member — leave it alone before 1.0, so check for what you use rather than
assuming a version implies it. From 1.0 on, an addition raises the
minor version and a break raises the major one, and Hive loads any
plugin whose major version matches.

Each entry names the Hive release that first shipped it.

### 0.2

Hive: Unreleased.

- **App plugins.** A manifest may declare `ui` (`entry`, optional
  `style`) instead of, or as well as, `main`. The app loads the module
  and calls its default export with the `hive` object — see
  [App surfaces](#app-surfaces).
- New requests: `SET_PLUGIN_CONFIG` (a UI plugin's settings) and
  `SET_CLIENT_UI` (the app's own; ignored from a plugin).
- `PluginInfo` gains `ui`, `config` and `builtin`; `source` may be
  `builtin` (see [Bundled plugins](#bundled-plugins)).
- The `hive` object's `subscribeSessions`, `actions.getPlanReview`,
  `actions.resolvePlanReview` and `actions.externalPlanReviewers`
  arrived within 0.2, after its first surfaces.
- **Breaking:** a manifest must say `"api_version": "0.2"`. A `0.1`
  plugin is shown as `refused` until it does. Nothing else a `0.1`
  plugin could use changed.

### 0.1

Never in a Hive release; it existed only between pull requests.

- Background plugins: `main.command`, the plugin socket and
  environment, every control request and event, and the Node SDK.
