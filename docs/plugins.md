# Writing a Hive plugin

A Hive plugin is a program that Hive runs for you in the background. It
sees what every session is doing — which agent started working, which
one is waiting on you, which one exited — and can act: rename a session,
type into it, start or stop one, file an idea. Plugins are how features
that are too specific for Hive itself get built: a webhook when an agent
needs you, a bridge to a chat app, a rule that restarts a crashed agent.

This page is the contract. The reference plugin,
[`plugins/webhook/`](../plugins/webhook/), is written against it and
nothing else.

> **Plugin API 0.1 — experimental.** Until the plugin API reaches 1.0 it
> is best-effort: any Hive release may change it, and a plugin is only
> loaded by a Hive that implements exactly the `api_version` it names.
> From 1.0 on, compatibility follows semantic versioning.

## Trust

**A plugin runs as you, with everything you can do.** There is no
sandbox. It can read and write your files, reach the network, start
programs, and — through Hive — type into any session, including ones
running agents with access to your code and credentials. Install
plugins you would be comfortable running yourself.

Because of that, Hive installs every plugin **disabled**. Nothing a
plugin contains runs until you enable it, and the Hive app asks you
first, showing the plugin's name, source and the command it will run.
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
  "api_version": "0.1",
  "description": "POSTs a JSON message to a URL whenever a session starts waiting on you.",
  "main": { "command": ["node", "main.mjs"] }
}
```

| Field | Required | Meaning |
|-------|----------|---------|
| `id` | yes | Lowercase letters, digits and hyphens, up to 63 characters. Unique among installed plugins. |
| `name` | yes | Shown to the user. |
| `version` | no | Your plugin's own version. |
| `api_version` | yes | The plugin API you target. Must be `0.1` for this Hive. |
| `description` | no | One sentence, shown to the user. |
| `main.command` | yes | The program and arguments Hive runs, in the plugin directory. A first element starting with `./` is relative to the plugin; anything else is looked up on Hive's `PATH`. |
| `ui` | — | Reserved for GUI entry points. A manifest that declares one is refused by this version of Hive. |

Any language works. The reference plugin uses Node (18 or newer) and
the SDK below.

## Installing

> The Hive app's **Settings → Plugins** tab — install, enable, disable,
> remove — arrives in the next phase of this feature (spec 460). Until
> then plugins are managed over the wire protocol with the
> `INSTALL_PLUGIN` / `SET_PLUGIN_ENABLED` / `REMOVE_PLUGIN` requests
> below, which is what that tab will send.

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

## Lifecycle

| Status | Meaning |
|--------|---------|
| `stopped` | Installed and disabled (or enabled but not started yet). Never running. |
| `running` | The process is alive. |
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
| `REMOVE_PLUGIN` | `RemovePluginReq` | `PLUGIN_EVENT` (removed) |

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
  connected plugin does not count, whatever it calls itself.
- **It has a rate budget.** Cheap reads cost 1, changes that are
  broadcast to every client cost 10, and anything that starts or
  destroys something (creating, restarting or killing a session,
  worktree changes, installing a plugin) costs 100, against 1000 per
  second with a burst of 2000. Past it, Hive stops reading the plugin's
  requests until the budget refills — nothing is dropped, requests just
  slow down.

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

## Testing a plugin

Run it against an isolated Hive, never your real one:
`scripts/dev-iso.sh` starts a throwaway daemon with its own socket and
state directory. Install your plugin there by directory, enable it, and
watch `plugin.log` in its data directory.
