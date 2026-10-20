# Orchestrator grant: a session you name can message its siblings

- **Spec:** [docs/product-specs/389-orchestrator-grant-and-session-msg.md](../../product-specs/389-orchestrator-grant-and-session-msg.md)
- **Design:** [docs/design-docs/agent-orchestration.md](../../design-docs/agent-orchestration.md)
- **Issue:** —
- **Branch:** `feature/389-orchestrator-grant`
- **PR:** [#524](https://github.com/lucascaro/hive/pull/524)
- **Status:** active

## Summary

Add a persisted per-session `Orchestrator` flag, let the GUI set it,
and let a `ModeSession` connection from a granted session send
`SEND_TO_SESSION` to a sibling in its own project, with the daemon
stamping provenance. `hived msg` learns to work from inside a session.
Written for a fresh session to pick up: research cites `main` as of
2026-09-10; re-verify names against `main` before writing code, and
**start by reading how 338 actually shipped** — this plan assumes the
frames, error codes and `hived msg` it describes.

## Research

- `internal/daemon/daemon.go` — `serveControl` resolves
  `restricted`, `ownSessionID`, `ownProjectID` once at handshake;
  `sessionModeFrames` is the allowlist a `ModeSession` connection may
  use; `controlOps` carries the restriction into `handleControlFrame`.
  This is where the grant check lives.
- `internal/daemon/socket.go:~164` — the events socket that
  `HIVE_SOCKET` names; serves `ModeEvent` and `ModeSession` only.
- `internal/registry/registry.go:83` `Entry` — add the flag next to
  `Agent`; `internal/registry/persist.go` `MetaFile` — persist it.
- `internal/wire/control.go` — `CreateSpec` (:40), `SessionInfo`
  (:152), `UpdateSessionReq` (:417), `Error`/`ErrCode*` block. 338
  adds `SendToSessionReq`, `SentResp`, `FrameSendToSession`.
- `internal/registry/deliver.go` (from 338) — the three delivery
  strategies; provenance prefix is applied before strategy selection.
- `cmd/hived/idea.go` — `ideaEnv`, `ideaDial`, the `ModeSession`
  handshake and `CheckSocketDir` guard; `cmd/hived/msg.go` (from 338)
  — the control-socket client to extend.
- `cmd/hivegui/frontend/src/components/modals/Launcher.tsx`,
  `components/SessionRow.tsx` (context menu), `store/store.ts`,
  `bridge.ts`, `cmd/hivegui/app_calls.go` — the create/update path
  the checkbox and toggle ride.
- `internal/buildinfo` — `DaemonContract` bump (wire changes).

## Corrections to the 2026-09-10 draft (found against main d6620e62)

1. **Error codes.** `not_found` / `invalid` do not exist. 338 uses `no_such_session` (sendErrCode, daemon.go:2240) and `bad_payload`. Only new code: `ErrCodeNotOrchestrator = "not_orchestrator"`. Other-project target ⇒ `no_such_session`; self ⇒ `bad_payload` ("a session cannot message itself"). Spec success-criteria wording is amended to match (spec itself says "or the existing nearest code").
2. **Name resolution.** 338's `hived msg` resolves names CLIENT-side via LIST_SESSIONS (msg.go resolveTarget). A ModeSession conn cannot LIST_SESSIONS (not in sessionModeFrames), so the daemon resolves: id exact, else unique exact name, among `ProjectID == ownProjectID` sessions. Duplicate name ⇒ `bad_payload` listing the same-project ids (matches control-path behaviour).
3. **Provenance.** `Registry.SendToSession(id,text,queue,origin)` already has `origin` (only ACP uses it, defaults `wire.OriginUser`). Reuse it: session callers pass `"session:<id>"`. Registry resolves the sender's Name under r.mu, sanitizes the BODY first (empty ⇒ `ErrMessageInvalid`, so a bare prefix is never delivered), then prepends `Message from Hive session "<name>": ` and re-checks `wire.MaxMessageLen` on the combined string, all before `tryInbox` (so Claude-inbox, Pi-extension, typed, queued and ACP all carry it). The name is cleaned separately (control chars/newlines → space, `"` → `'`, cap 64 runes) because `sanitizeInput` keeps `\n`/`\t` and a name containing a newline could forge a second "Message from…" line. A sender that no longer exists ⇒ refuse with `no_such_session` (never send with a blank name). ACP path: origin `wire.OriginUser` is kept (the prefix in the text already carries provenance; AcpTranscript.tsx:124-125,207-208 would render a raw `From session:<id>` otherwise). The `session:<id>` origin is used only for the log line.
4. **Grant is set only by the user.** `CreateSpec.Orchestrator` from a plugin socket (`ops.principal != ""`) is forced false in the CREATE_SESSION arm; `UpdateSessionReq.Orchestrator` from a plugin is refused (`mode_not_allowed`). ModeSession conns can't reach CREATE/UPDATE at all (allowlist). Design doc: "depth 1 — a spawned session is not an orchestrator unless the user grants it".
5. **Three easy-to-miss persistence/update touchpoints**: `updatesPersistedFields` (commands.go:112) and the `bellOnly` test in `Registry.Update` (registry.go:2003) both enumerate fields — a toggle-only request would be silently dropped / treated as no-persist without them. Persistence copies: `persistEntryLocked` MetaFile (registry.go:2126), load (registry.go:1305, :1330), archive/restore (closed.go:140, :440), `Info()` (registry.go:348), `create.go:625`.

## Approach

### Wire/registry
- `CreateSpec.Orchestrator bool json:"orchestrator,omitempty"`; `UpdateSessionReq.Orchestrator *bool json:"orchestrator,omitempty"`; `SessionInfo.Orchestrator bool json:"orchestrator,omitempty"`; `Entry.Orchestrator`; `MetaFile.Orchestrator`.
- `ErrCodeNotOrchestrator`. `DaemonContract` 31 → 32 (a GUI that sends/reads the flag needs a daemon that persists it).
- `Registry.Update`: apply `req.Orchestrator`; include in bellOnly negation (so it persists). `updatesPersistedFields` includes it.

### Daemon (internal/daemon/daemon.go; helper in new internal/daemon/orchestrate.go)
- `sessionModeFrames[FrameSendToSession] = true`; update the mode_not_allowed message text.
- `FrameSendToSession` arm: if `ops.restricted`: (a) read the caller's entry from the registry on this frame (not cached at handshake): missing/ungranted ⇒ `not_orchestrator`, and take the caller's CURRENT `ProjectID` from that same read (a user can move a session between projects mid-connection; the handshake-cached `ownProjectID` is not used for this verb); (b) `resolveSibling` against that project; force `queue=true` for session callers (spec: always queued); (c) target==caller ⇒ `bad_payload`; (d) from = `"session:"+ops.ownSessionID`. Control conn unchanged (origin `wire.OriginUser`). The plugin refusal stays first.
- Log: `send_to_session caller=<control|session> from=<id> to=<id> delivery=<kind>` — existing line already has caller=control, from=user; add the session variant, and log refusals (outcome = error code) for the session caller too, resolved target id rather than the raw ref.
- CREATE/UPDATE arms: plugin cannot set the flag (item 4).

### hived msg (cmd/hived/msg.go)
- In-session (`ideaEnv()` ok): `ideaDial` (ModeSession), skip LIST_SESSIONS, send `SessionID: ref` as given; print `delivery` kind (no "to name (id)" pre-line since unresolved client-side — or print after SENT using SentResp.SessionID). Out-of-session: unchanged. On `not_orchestrator` print: `this session may not direct other sessions — enable "May direct other sessions" in the launcher or the session's menu`. Remove the "not available inside a Hive session yet" error; update the file header + usage.

### GUI
- `app_calls.go`: `SetSessionOrchestrator(id, want)` → UPDATE_SESSION{Orchestrator}; `CreateSession` threads `orchestrator`; frontend `bridge.ts`, `store` (read `orchestrator ?? ` snake_case at the boundary), `Launcher.tsx` checkbox (default off, not remembered), `SessionRow.tsx` context-menu checked item + glyph next to the idea glyph (new `Icon` name per docs/design-docs/ui), `wails-mock.ts` + `hive-global.d.ts` + `e2e-real/wails-bridge.ts`. `hived-ws-bridge/main.go` and `wire/testclient` kept in lock-step.
- No keybinding ⇒ no bindings/menu/help work. Context-menu item is a command if the codebase routes menu items through `commands.ts` — check `SessionRow` pattern while implementing; if so follow it.

### Docs
- `docs/design-docs/control-plane.md` one sentence; `docs/design-docs/agent-orchestration.md` phase-1 note that error codes reuse 338's; `DESIGN.md` untouched (no structural change); changeset `.changesets/389-orchestrator-grant.md` (minor); `site/features.json` entry `since: "Unreleased"`; README section on `hived msg` if one exists.

## Tests
internal/daemon (new orchestrate_test.go, using the existing session-conn harness in send_test.go / idea tests):
- `TestSessionModeSendEmptyBodyRefused`, `TestSessionModeSendForcesQueue` (client `queue=false` still queues), `TestSessionModeSendFollowsProjectMove` (move sender with UpdateSession{ProjectID}, then old-project target ⇒ `no_such_session`).
- `TestSessionModeSendRequiresGrant` — ungranted ⇒ `not_orchestrator`, nothing delivered.
- `TestSessionModeSendRevokedMidConnection` — open session conn, send OK, `UpdateSession{orchestrator:false}` on a control conn, next send on the SAME conn ⇒ `not_orchestrator`.
- `TestSessionModeSendOtherProjectNotFound`, `TestSessionModeSendSelfRefused`, `TestSessionModeSendResolvesName` (+ duplicate-name refused, id works).
- `TestPluginCannotSetOrchestrator` (create forced false; update refused).
- `TestSendToSessionLogLine` for caller=control and caller=session (capture `log` output).
- update `send_test.go:71` (it asserts SEND_TO_SESSION is NOT allowlisted) to assert it IS allowlisted but refused without the grant.
internal/registry:
- `TestSendProvenancePrefixEveryPath` — table: claude inbox (fake inboxSend), pi extension, typed, queued, ACP: each receives `Message from Hive session "<name>": ` + text.
- `TestSendProvenanceNameCannotForge` — name with `\n`/`"`/control chars is neutralised.
- `TestSendProvenanceCountsAgainstCap`, `TestSendProvenanceEmptyBodyRefused`.
- `TestMetaFileRoundTripsOrchestrator` + restart reload + archive/unarchive round trip; `TestUpdateOrchestratorPersists` (toggle-only request writes to disk).
cmd/hived: `TestMsgInSessionUsesSessionSocket` (e2e style like idea_e2e_test.go: env set ⇒ ModeSession, not_orchestrator message printed verbatim; granted ⇒ prints kind).
Frontend: dom test (also no `originLabel` change needed: origin stays `user`); launcher checkbox ⇒ `create` payload carries orchestrator; row toggle ⇒ bridge call; glyph renders from `orchestrator`. e2e (mock): every-shortcut sweep unaffected. e2e-real: decided at implement time after reading the harness; if it cannot spawn a session with HIVE_SOCKET env, the round-trip is covered by the daemon tests + dom/mock tests and the plan's Progress says so.

## Verification
- `GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod) scripts/test.sh` (go unit dom e2e) exit 0.
- `for os in darwin linux windows; do GOOS=$os staticcheck ./... && GOOS=$os go vet ./...; done` exit 0.
- `scripts/check-daemon-contract.sh origin/main HEAD` exit 0; `scripts/check-test-hooks-stripped.sh`; `npm run typecheck` after `./scripts/ci-bootstrap.sh`; `biome ci .`.
- Mutation checks (each must turn a named test red): drop the per-frame flag read (cache at handshake) ⇒ `RevokedMidConnection` fails; drop the prefix on the typed path ⇒ provenance test fails; drop `Orchestrator` from `updatesPersistedFields` ⇒ `TestUpdateOrchestratorPersists` fails.
- Manual smoke, no human: `wails dev` at localhost:34115 per docs/verifying-the-gui-by-hand.md — Playwright script ticks the launcher checkbox, asserts glyph, toggles menu item.

## Second opinion

Independent reviewer, 2 rounds. Round 1: revise (8/10) — 5 must-fix applied (empty-body, per-frame project, unauthenticated HELLO recorded, origin stays user, forced queue). Round 2: revise (7/10) — TOCTOU applied (single locked registry call); shell-target policy put to the operator. Dispositions:
- TOCTOU (grant/project read vs sender lookup): applied. New `Registry.SendFromSession(callerID, ref, text)` does grant check + current-project read + sibling resolution + sender-name read under ONE `r.mu` hold, then delegates to the shared delivery code. Test: `TestSendFromSessionRevokedBetween…` via registry-level call; sender deleted ⇒ `no_such_session`, nothing delivered.
- Shell/non-agent targets: **operator decision** (see below). Default in this plan: session callers may only target sessions whose agent is a known agent kind or ACP (refuse plain shells with `bad_payload` "target is not an agent session"), because a granted agent otherwise gets arbitrary command execution in a sibling shell and the drain-on-idle queue has the same exposure.
- Prefix includes the short id: `Message from Hive session "name" (abc123): …` (names are not unique/renamable).

## Risks / open
- **ModeSession HELLO is unauthenticated** (daemon.go:1059-1062): the connection is bound to the `session_id` the client claims. A process holding `HIVE_SOCKET` that learns a granted sibling's id could inherit its grant. Accepted residual: same-user threat model (SECURITY.md); ids are not exposed to sessions (events-socket list is narrowed to own id; no sibling listing until spec 390). Fixing it (per-session secret in env) is a separate hardening for the user to decide — it would also need the nonce plumbing SUBMIT_RESULT has. **Surfaced for the operator.**
- Body text can itself contain a forged `Message from Hive session "boss": …` line; provenance is a text prefix, so only the daemon-set *leading* prefix is trustworthy. Inherent; stated in docs.
- No ping-pong guard beyond "not self" and the per-target queue cap (maxQueuedMessages 16 / maxQueuedBytes); the log line is the evidence. Matches spec non-goal.
- Grep every `Entry{`/`MetaFile{` constructor (restore, duplicate, closed.go) so none drops `Orchestrator`.
- Prefix is part of the typed text: for a TUI agent the target sees `Message from Hive session "x": …` as a user prompt line — intended (spec) but a prompt-injection laundering vector remains by design; mitigated by the user-named grant.
- Duplicate names within a project: refused, ids listed. A granted session has no way to learn sibling ids until spec 390; it can still use names. Accepting.
- `DaemonContract` bump costs users their agents on upgrade only if GUI/daemon mismatch; required here.
- Phase gate (≥10 session sends over 2 weeks) is operational, not testable here.

## Decision log

- **2026-09-10** — Name resolution for `hived msg` happens in the
  daemon, project-scoped, rather than giving granted sessions a
  session list. Why: keeps phase 2's read surface a separate decision
  with its own gate.
- **2026-09-10** — Flag persisted in `MetaFile`. Why: a grant is user
  intent, like `Agent`; losing it on daemon restart would silently
  break an orchestration the user set up.
- **2026-10-20** — Granted sessions may target agent/ACP sessions only; plain shells refused (`bad_payload`). Why: otherwise a granted agent gets command execution in a sibling shell (operator decision).
- **2026-10-20** — ModeSession HELLO stays unauthenticated; recorded as accepted residual risk, hardening deferred. Why: same-user threat model; ids not exposed to sessions.

## Progress

- **2026-09-10** — Plan written.
- **2026-10-20** — Plan re-verified against main, revised, second-opinion x2, approved.
- **2026-10-20** — Implemented on `feature/389-orchestrator-grant`. Go: wire fields, `Registry.SendFromSession` (grant + current project + target + sender name under one `r.mu` hold), daemon arm, plugin guards, `hived msg` in-session path, DaemonContract 32. GUI: launcher checkbox, glyph, palette command. Verified: `go test ./...` (daemon, registry, cmd/hived incl. `-tags e2e` msg tests), vitest unit+dom, Playwright e2e (559), typecheck, `biome ci`, staticcheck+vet on darwin/linux/windows, ui-lint, hooks-stripped. e2e-real: 34/35; `glyph-utf8` fails on this Mac (bash 3.2 echo/locale in the harness), unrelated to the diff.

## PR convergence ledger
- **2026-10-20 iter 1** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: a4e1d07 (minor usage note applied after).
