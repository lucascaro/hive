# herdr and iTerm2: agent status and PTY sizing

Sources: see per-claim links. Fetched 2026-10-15.
herdr: `herdrdev/herdr` @ `b431a06` (cloned and read). iTerm2: 3.7.x docs + `gitlab.com/gnachman/iterm2` @ `069b6e0`.
Refresh when either project ships a major version; both moved fast in 2026.

## Summary

- **Project picked:** herdr = `github.com/herdrdev/herdr` (Rust, Apache-2.0 per its LICENSE, site herdr.dev). Search results also show `ogulcancelik/herdr` as an author-account path; the `herdrdev` repo is the one linked from herdr.dev. Not an ambiguous name otherwise.
- **Status, herdr:** two sources per pane. Screen-pattern manifests (default for Claude Code, Codex) and agent-pushed state over a socket API (`pane.report_agent`) for Pi, OpenCode, Kimi, etc. Pushed state wins when present.
- **Status, iTerm2 3.7:** a new `OSC 21337` Session Status protocol plus `it2`. A bundled hook binary (`cc-status`) maps Claude Code/Codex hooks to working/waiting/idle/done/error. Esc-interrupt is caught via Claude's `OSC 9;4` progress sequences, because Claude runs no hook on interrupt.
- **Resize, herdr:** server owns PTYs; with several clients, "last client to focus/select/interact with a tab controls its size". Not smallest-wins.
- **Resize, iTerm2 -CC:** tmux's rule, "never larger than the smallest attached client"; iTerm2 sends `refresh-client -C @win:WxH` and enables tmux pause mode (`pause-after`) for slow-consumer backpressure.
- **Hive today:** last attach frame to arrive wins resize, no policy; state tiers (hook/extension/heuristic/laya) exist; Hive does not read `OSC 9;4`.
- **Best candidates:** (1) use Claude's `OSC 9;4` turn edge to end stuck `working` after Esc; (2) explicit resize-owner policy; (3) a generic "report state" frame/CLI for third-party agents.

## herdr

### Status reporting

- **Detection pipeline.** The detect module reads each pane's live bottom-of-buffer text periodically and matches it against per-agent patterns: [`src/detect/mod.rs:1-4`](https://github.com/herdrdev/herdr/blob/b431a063e2cfdbfc3ca41bcd0dcbc329f92d217f/src/detect/mod.rs#L1-L4). States are `Idle | Working | Blocked | Unknown` ([L11-L20](https://github.com/herdrdev/herdr/blob/b431a063e2cfdbfc3ca41bcd0dcbc329f92d217f/src/detect/mod.rs#L11-L20)).
- **Agent identity** comes from the pane's foreground process (`identify_agent_in_job`, same file); a `HERDR_AGENT=<agent>` env hint covers wrappers ([agents.mdx:70](https://github.com/herdrdev/herdr/blob/b431a063e2cfdbfc3ca41bcd0dcbc329f92d217f/docs/next/website/src/content/docs/agents.mdx#L70)).
- **Screen rules are data ("manifests")**, bundled and remotely updatable without a binary release; local overrides win ([agents.mdx:84-98](https://github.com/herdrdev/herdr/blob/b431a063e2cfdbfc3ca41bcd0dcbc329f92d217f/docs/next/website/src/content/docs/agents.mdx#L84-L98)). `herdr agent explain` shows which rule fired.
- **Blocked is deliberately strict:** only when the live screen matches a known approval/question UI; no match for a known agent falls back to `idle` ([agents.mdx:76](https://github.com/herdrdev/herdr/blob/b431a063e2cfdbfc3ca41bcd0dcbc329f92d217f/docs/next/website/src/content/docs/agents.mdx#L76)).
- **Working is PTY-activity driven**; screen "visible_working" is diagnostic ([`detect/mod.rs:36-39`](https://github.com/herdrdev/herdr/blob/b431a063e2cfdbfc3ca41bcd0dcbc329f92d217f/src/detect/mod.rs#L36-L39)).
- **Working to idle is debounced:** needs 3 confirmations or a time cap before flipping, unless the screen shows live idle chrome or a blocker ([`pane/agent_detection.rs:7-8,39-75`](https://github.com/herdrdev/herdr/blob/b431a063e2cfdbfc3ca41bcd0dcbc329f92d217f/src/pane/agent_detection.rs#L39-L75)).
- **OSC is used lightly:** OSC title/progress strings are fed to manifest rules (`detect_with_osc`, `detect/manifest.rs:12-22`), and Devin is documented as "Screen + OSC". OSC 0/2 titles can show in the sidebar ([agents.mdx:155](https://github.com/herdrdev/herdr/blob/b431a063e2cfdbfc3ca41bcd0dcbc329f92d217f/docs/next/website/src/content/docs/agents.mdx#L155)).
- **Pushed state (hooks/plugins).** Integrations call `pane.report_agent` with `source`, `agent`, `state` (`working|idle|blocked`), optional `message`; session identity via `pane.report_agent_session` ([socket-api.mdx:658-692](https://github.com/herdrdev/herdr/blob/b431a063e2cfdbfc3ca41bcd0dcbc329f92d217f/docs/next/website/src/content/docs/socket-api.mdx#L658-L692)). Same via CLI: `herdr pane report-agent <pane> --source custom:x --agent y --state working` ([integrations](https://herdr.dev/docs/integrations/)).
- **Ordering:** `seq` per `source` drops stale or out-of-order reports ([socket-api.mdx:771](https://github.com/herdrdev/herdr/blob/b431a063e2cfdbfc3ca41bcd0dcbc329f92d217f/docs/next/website/src/content/docs/socket-api.mdx#L771)). `pane.clear_agent_authority` / `pane.release_agent` exist ([L106](https://github.com/herdrdev/herdr/blob/b431a063e2cfdbfc3ca41bcd0dcbc329f92d217f/docs/next/website/src/content/docs/socket-api.mdx#L106)).
- **Precedence:** "When an integration also reports state, Herdr uses those reports instead of reading the screen" ([agents.mdx:64](https://github.com/herdrdev/herdr/blob/b431a063e2cfdbfc3ca41bcd0dcbc329f92d217f/docs/next/website/src/content/docs/agents.mdx#L64)); screen detection is skipped only under a "full lifecycle hook authority" ([socket-api.mdx:926](https://github.com/herdrdev/herdr/blob/b431a063e2cfdbfc3ca41bcd0dcbc329f92d217f/docs/next/website/src/content/docs/socket-api.mdx#L926)).
- **Per-agent source** ([integrations](https://herdr.dev/docs/integrations/)): hook/plugin state for Pi, OMP, Kimi, OpenCode, Kilo, MastraCode. **Claude Code and Codex use the screen for state**; their hooks only report session identity (for resume after a server restart). Docs say some agents' hooks "don't cover every lifecycle transition" (Droid, Qoder) or are unreliable after cancel/interrupt (Devin).
- **Display-only vs semantic:** `pane.report_metadata` changes title/labels/tokens but never `working|blocked|idle`, waits or notifications ([socket-api.mdx:753](https://github.com/herdrdev/herdr/blob/b431a063e2cfdbfc3ca41bcd0dcbc329f92d217f/docs/next/website/src/content/docs/socket-api.mdx#L753)).
- **Agent-to-agent:** socket API lets an agent spawn panes, send input, `pane.read`, and `pane.wait_for_output` / wait for another agent to be blocked ([README](https://github.com/herdrdev/herdr), [socket-api.mdx:106](https://github.com/herdrdev/herdr/blob/b431a063e2cfdbfc3ca41bcd0dcbc329f92d217f/docs/next/website/src/content/docs/socket-api.mdx#L106)).

### Resizing / PTY size

- **Model:** a background server owns panes and PTYs; clients attach. Detach leaves processes running ([concepts.mdx:67-77](https://github.com/herdrdev/herdr/blob/b431a063e2cfdbfc3ca41bcd0dcbc329f92d217f/docs/next/website/src/content/docs/concepts.mdx#L67-L77), [session-state](https://herdr.dev/docs/session-state/)).
- **Policy:** one client = all tabs follow it. Several clients = each viewed tab follows the client that most recently focused, selected, or interacted with it; when all but one detach, tabs return to the survivor's size; with none, PTYs keep their last size ([configuration.mdx:65](https://github.com/herdrdev/herdr/blob/b431a063e2cfdbfc3ca41bcd0dcbc329f92d217f/docs/next/website/src/content/docs/configuration.mdx#L65), [concepts.mdx:73](https://github.com/herdrdev/herdr/blob/b431a063e2cfdbfc3ca41bcd0dcbc329f92d217f/docs/next/website/src/content/docs/concepts.mdx#L73)). Introduced in changelog #3526 ([CHANGELOG.md:189](https://github.com/herdrdev/herdr/blob/b431a063e2cfdbfc3ca41bcd0dcbc329f92d217f/docs/next/CHANGELOG.md#L189)).
- **Mechanism:** server keeps `tab_geometry_controllers: HashMap<tab, client_id>` ([`server/headless.rs:212`](https://github.com/herdrdev/herdr/blob/b431a063e2cfdbfc3ca41bcd0dcbc329f92d217f/src/server/headless.rs#L212)). Interaction/focus events call `claim_shell_tab_geometry` (L2030, L2283-2303, L2433); on client removal the controller is dropped and geometry restored (L966, L1028-1037). Resize from a non-controller client does not resize PTYs (`resize_shell_tab_if_controller`, L2257).
- **Client side:** terminal size is polled and sent on SIGWINCH/change ([`client/terminal_geometry.rs:125-170`](https://github.com/herdrdev/herdr/blob/b431a063e2cfdbfc3ca41bcd0dcbc329f92d217f/src/client/terminal_geometry.rs#L125-L170)); the TUI renders client-side and the server streams surfaces (CHANGELOG:197).
- **Redraw nudge:** after a live server handoff, herdr briefly resizes the PTY to rows-1 (or cols-1) and back to force the child to repaint ([`pty/actor/unix.rs:983-1010`](https://github.com/herdrdev/herdr/blob/b431a063e2cfdbfc3ca41bcd0dcbc329f92d217f/src/pty/actor/unix.rs#L983-L1010)).
- **Reflow/scrollback:** herdr vendors `ghostty-vt` (`crates/ghostty-vt`) as its terminal emulator. UNVERIFIED: whether it reflows on resize, or how it protects scrollback; I did not read that path.

## iTerm2

### Status reporting

- **Session Status (3.7).** Three setters, all giving text, dot color, subtitle color, optional detail: control sequence `OSC 21337 ; status=… ; indicator=… ; status-color=… ; detail=… ST`, the `it2 session set-status` CLI, and a **Set Tab Status** trigger (regex on output). States named: Waiting, Working, Idle (text is free-form); 3.7.5 adds `done` and `error` ([Session Status](https://iterm2.com/documentation-session-status.html), [3.7 notes](https://iterm2.com/downloads.html)). Toolbelt tool sorts waiting, then working, then idle; Snooze; "Notify on Status Change" (⇧⌘X); a floating Cockpit across windows.
- **Claude Code integration.** Menu "Install Claude Code Integration" adds a hook to `~/.claude/settings.json` (honours `$CLAUDE_CONFIG_DIR` since 3.7.5), enables the Python API, adds a Workgroup (Chat / Diff / Code Review peers) ([doc](https://iterm2.com/claude-code-integration.html)). The hook is the `cc-status` Swift binary, source at [`cc-status/Sources/cc-status/`](https://gitlab.com/gnachman/iterm2/-/tree/master/cc-status/Sources/cc-status).
- **Event mapping** ([`HookEvent.swift:248-300`](https://gitlab.com/gnachman/iterm2/-/blob/master/cc-status/Sources/cc-status/HookEvent.swift#L248-L300)): `UserPromptSubmit`/`PreToolUse`/`PostToolUse` -> working; `PermissionRequest` and `Notification` (except `idle_prompt`) -> waiting; `Stop` -> done/error/idle via one shared `endTurn` rule; `idle_prompt` is ignored when the tab already shows a turn ending. Same binary serves Codex via `--agent codex` (main.swift:3-6).
- **Subagents / background tasks:** the hook counts `background_tasks` in the payload and stays "working" while any run (`main.swift:80-120`). Same insight as Hive's own `Stop.background_tasks` reconcile in agent-activity.md.
- **Interrupt gap closed with OSC 9;4.** The code comments (measured on Claude Code 2.1.274) state: Esc runs **no hook**; Claude emits `OSC 9;4;3` ~40 ms after a prompt is submitted and `OSC 9;4;0` within ~120 ms of turn end, however the turn ended. iTerm2 is asked to expire "working/waiting" back to idle on that edge (`expiresOnProgressEnd`, [`HookEvent.swift:40-70`](https://gitlab.com/gnachman/iterm2/-/blob/master/cc-status/Sources/cc-status/HookEvent.swift#L40-L70)). 3.7.5: "Interrupting Claude Code with Esc now returns its Session Status to idle".
- **Addressing:** outside tmux use `TERM_SESSION_ID`; in a tmux -CC pane use `$TMUX`/`$TMUX_PANE`, because `TERM_SESSION_ID` there names the wrong session ([`Addressing.swift:1-30`](https://gitlab.com/gnachman/iterm2/-/blob/master/cc-status/Sources/cc-status/Addressing.swift)). The hook always exits 0 and is quiet on per-tool-call failures (`It2Runner.swift:1-30`).
- **Older generic primitives** ([escape codes](https://iterm2.com/documentation-escape-codes.html)): `OSC 9 ; msg` notification; `OSC 9;4;st;pr` progress; `OSC 133 ; A/B/C/D;status` shell-integration prompt/command marks (iTerm2 shell integration turns failed-command marks red and offers "alert when next prompt returns", [doc](https://iterm2.com/documentation-shell-integration.html)); `OSC 1337 SetUserVar`, `RequestAttention`, `SetMark`. Triggers include Post Notification, Bounce Dock Icon, Set Tab Status, Invoke Script Function; a Job field limits a trigger to while e.g. Claude Code runs ([triggers](https://iterm2.com/documentation-triggers.html)).
- UNVERIFIED: exact Claude Code settings.json hook JSON that the installer writes (docs do not list it; I read the binary's source, not the installer).

### Resizing / PTY size

- **Rule:** "Windows are never larger than the smallest attached client"; all tmux windows in the session get the same rows/cols ([tmux integration](https://iterm2.com/documentation-tmux-integration.html)). Window resize "tells tmux that the client size has changed, causing all windows to resize" (same page). This is tmux's behaviour, not an iTerm2 policy; the page does not mention `window-size`.
- **Wire:** iTerm2 sends `refresh-client -C @<win>:<W>x<H>` (per-window size, tmux 3.x), falling back to `resize-window -x -y`; it skips the send when the size is unchanged ([`sources/tmux/TmuxController.m:1205-1222`](https://gitlab.com/gnachman/iterm2/-/blob/master/sources/tmux/TmuxController.m#L1205-L1222)).
- **Backpressure:** on tmux >= 3.2 it enables pause mode (`refresh-client -fpause-after=<age>`), and the gateway handles `%extended-output` / `%pause` ([`TmuxController.m:1327-1355`](https://gitlab.com/gnachman/iterm2/-/blob/master/sources/tmux/TmuxController.m#L1327-L1355), `TmuxGateway.m:187,504,858-883`). Nearest analogue to Hive's slow-client policy.
- **Scrollback:** the doc only says plain tmux scrollback is hard to access; it does not describe how scrollback survives resize. UNVERIFIED how iTerm2 avoids reflow corruption in -CC; reflow is done by tmux, and I did not trace the capture-pane/restore path.
- **Limits** noted by the docs: a tmux tab may not contain non-tmux split panes; split dividers cause "empty" areas.

## Comparison with Hive today

Sources: [agent-activity.md](../design-docs/agent-activity.md), [laya-state-classifier.md](../design-docs/laya-state-classifier.md), [slow-client-policy.md](../design-docs/slow-client-policy.md), [control-plane.md](../design-docs/control-plane.md), `DESIGN.md`, `internal/daemon/daemon.go:2115-2120`, `internal/session/session.go:480-530`.

| Area | herdr | iTerm2 | Hive |
|---|---|---|---|
| State for Claude | screen manifests (hooks only for identity) | hooks via `cc-status` + OSC 9;4 expiry | hooks (`hived hook`), heuristic floor, optional Laya |
| Precedence | pushed beats screen; `seq` ordering | hook sets, progress edge expires | tier trust with stale timeouts (30 s), ordering guard |
| Interrupt (Esc) | debounced screen idle | OSC 9;4;0 edge | no hook fires; relies on heuristic/Tick. Hive does not parse `OSC 9;4` (grep of `internal/session`, `internal/registry`: only bell/title OSC). |
| Screen rules updatable | yes, remote manifests | n/a | hardcoded in Go; Laya model optional |
| Third-party agent push | `pane.report_agent` + CLI | `OSC 21337`, `it2`, trigger | per-agent hook/extension code; no generic report frame |
| Resize owner | last-interacting client per tab | tmux smallest client | last `FrameResize` from any attach wins (`daemon.go:2115`); no policy |
| Reflow recovery | UNVERIFIED | UNVERIFIED | `REQUEST_REPLAY` re-streams the ring on width change (`session.go:486`) |
| Slow clients | not found in what I read | tmux pause mode | bounded queue, then disconnect (slow-client-policy.md) |

## Candidate adoptions for Hive

1. **Parse `OSC 9;4` turn edges from Claude and use them to end `working`.** Evidence: iTerm2's measured behaviour above. Hive already scans OSC in `internal/session/bell.go` and has a known gap where Esc-interrupt fires no hook. Tradeoff: a Claude Code implementation detail (not in Hive's documented-hooks rule, control-plane.md:228), so it must stay a heuristic input and be probe-tested like `TestClaudeProbeTaskToolsOptIn`; needs verification that `OSC 9;4` reaches Hive's PTY (Claude may gate it on the terminal). Effort: S-M. Needs `DaemonContract` bump.
2. **Explicit resize-owner policy for multi-attach.** Evidence: herdr's "last interacting client controls" with controller handoff on detach, versus today's implicit last-writer-wins. Tradeoff: last-writer-wins can thrash two GUIs; smallest-client (tmux) is stable but shrinks the big window; herdr's rule needs an interaction signal on the wire. Decide per Open questions. Effort: M (new attach frame or reuse of focus events; registry holds owner; contract bump).
3. **Generic "report state" entry point** (CLI + frame, `source` + `seq`, never overrides plan/metadata). Evidence: herdr `pane.report_agent` / iTerm2 `it2 session set-status`. Tradeoff: another trust surface (plugin principals, DESIGN.md plugin rules); duplicates hook tier for Claude. Gains custom agents without Go changes. Effort: M.
4. **Show a "done / error" state distinct from idle**, dismissed when the user types. Evidence: iTerm2 3.7.5. Tradeoff: UI/state-model change touching `needs_attention`. Effort: M.
5. **Data-driven screen rules (manifests) for non-hook agents**, with a debounced working-to-idle (3 confirmations). Evidence: herdr. Tradeoff: overlaps Laya; maintenance of rules per agent release. Effort: L. Probably only the debounce idea is cheap (S).
6. **Pause/backpressure instead of disconnect.** Evidence: tmux pause mode. Tradeoff: Hive's disconnect-and-replay is already correct and simpler; no concrete need. Not recommended.

Not adopting: herdr's redraw nudge (resize rows-1 then back) is a handoff workaround; Hive has no live-handoff path.

## Open questions

- Does Claude Code emit `OSC 9;4` unconditionally or only for certain `TERM_PROGRAM`/terminals? Hive sessions set a different environment; must be probed on a real session.
- Resize: which policy do you want (last-interacting, smallest, or explicit "primary viewer")? Is multi-GUI attach to one session a supported case, or only GUI plus hivebar/plugins?
- herdr: how does `ghostty-vt` reflow and protect scrollback on resize? Not read.
- iTerm2: how does -CC restore history after resize (capture-pane path)? Not read.
- Exact Claude hook JSON written by iTerm2's installer: not read.
