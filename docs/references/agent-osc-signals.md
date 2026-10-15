# Agent state via terminal escape sequences: what agents emit, what Hive reinvents

Sources: per-claim links and `file:line`. Fetched 2026-10-15.
Pinned: Claude Code 2.1.300 (local binary), Codex 0.157.1 (local binary) and `openai/codex@0884c80` (main, 2026-10-15), `google-gemini/gemini-cli@4718fa4`, Pi 1.0.4 (local npm install), `copilot-cli` changelog through 1.0.88, Aider `main` `io.py`.
Refresh when Codex 0.163 goes stable, or when OSC 7501 gains a revision (spec is at 0.1, 2026-09-28). This area moved weekly in Sep-Oct 2026.
Builds on [herdr-iterm-agent-integration.md](herdr-iterm-agent-integration.md); it resolves that file's open question on `OSC 9;4`.

## Summary

- **New standard.** `OSC 7501` (Program Status Protocol, [spec 0.1](https://gist.github.com/mitchellh/7acae3abd8355c1c00287d67e96c913a)) carries `idle|working|blocked|done|error`, a `kind` (`permission|question|auth`) and `progress`. Claude Code 2.1.295+ and Codex (main, first in 0.163.0-alpha.8) emit it. Hive parses none of it.
- **Claude gates 7501 on a query reply.** It sends `OSC 7501 ; ?` at startup and emits status only if the terminal answers within 2 s. Verified on a bare PTY. Nothing in Hive answers, so Claude emits nothing today.
- **Codex emits 7501 unconditionally on a TTY, no query.** But the installed 0.157.1 and latest stable 0.162.1 predate it. The same Codex *does* ship lifecycle hooks (12 events incl. `PermissionRequest`, `Stop`, `Interrupt`), contradicting `control-plane.md:302` ("no comparable surface").
- **`OSC 9;4` is not usable from Hive's env.** Claude emits it only when `TERM_PROGRAM` is `ghostty` >=1.2.0, `iTerm.app` >=3.6.6 or ConEmu. The herdr note's candidate 1 therefore needs a `TERM_PROGRAM` spoof with wide side effects; 7501 supersedes it.
- **Hive's env is nondeterministic.** `sessionEnv` forces `TERM` and drops only `NO_COLOR` (`internal/session/env.go`), so `TERM_PROGRAM` leaks in from whatever launched `hived`. Agents may already see `iTerm.app`.
- **Title is the free signal.** Gemini (default on) and Codex write state into OSC 0 (`✋ Action Required`, `⏲ Working…`, `◇ Ready`). Hive stores titles for display only.
- **Hooks stay the primary tier for Claude and Pi.** OSC is worth adding as a lower-trust tier for Claude's Esc-interrupt gap (unverified), and as the cheapest path for Codex, Gemini, Aider.
- **Trust.** All PTY-byte signals are forgeable by data (`cat`, tool output). Hive already accepts one such signal (BEL becomes `waiting_input` on every tier). A new OSC tier must sit below `hook`/`extension`/`acp`.

## Matrix: agent x signal

Catalog read from `internal/agent/agent.go:19-25` (shell, claude, codex, gemini, copilot, aider, pi). No OpenCode in the catalog. Evidence tags: **S** = read in source, **B** = read in the local binary, **E** = empirical on a captured PTY, **D** = vendor docs/changelog only. Anything else is UNVERIFIED.

| Agent | Sequence | Trigger | Gating (default) | Enable from Hive's PTY? | Evidence |
|---|---|---|---|---|---|
| Claude 2.1.300 | `OSC 7501 ; ?` query | process start | always, in any TTY | n/a (it is the probe) | **E**: bytes `ESC]7501;?BEL` captured next to `ESC[c`, `ESC[?u`, `ESC[>0q` with `TERM=xterm-256color`, no `TERM_PROGRAM` |
| Claude | `OSC 7501 ; state=<s>:app=claude-code[:id][:kind][:progress][:title=b64][:msg=b64]`, `state=clear` on teardown | working / blocked (permission, question) / done / error per [Ghostty PR](https://github.com/ghostty-org/ghostty/pull/14560) and [docs](https://code.claude.com/docs/en/terminal-config#see-session-status-in-your-terminal) | only after the terminal replies to the query. No reply by the 2000 ms deadline settles `programStatus=false`. Off if `CLAUDE_CODE_DISABLE_TERMINAL_TITLE` is set; off for bg sessions; inside tmux it asks tmux instead | **Yes, if the daemon answers the query** (reply is the same body `OSC 7501 ; ? ST`). No env switch forces it on | **B**: encoder `v_r`, probe `ob`, gate `g()`; **E** for the query only. Exact state-to-trigger mapping: **D** only. Mapping on Esc UNVERIFIED (needs credentials) |
| Claude | `OSC 9 ; <title>: <msg>` | Notification events (permission prompt, idle) when "away" | `preferredNotifChannel` in `auto\|iterm2\|terminal_bell\|iterm2_with_bell\|kitty\|ghostty\|notifications_disabled`. `auto` emits only for terminal=`iTerm.app`/`kitty`/`ghostty`, `terminal_bell` for Apple Terminal; else nothing ([docs](https://code.claude.com/docs/en/terminal-config#get-a-terminal-bell-or-notification)) | `--settings '{"preferredNotifChannel":"terminal_bell"}'` (Hive already passes `--settings`, `internal/agent/claude.go:343`). `ghostty` -> `OSC 777;notify`, `kitty` -> `OSC 99`, `iterm2` -> `OSC 9` | **B** (`$Wr`, `A()`, `I()`, `y()`) + **D** |
| Claude | `OSC 9;4;3` (indeterminate), `;4;0` (clear), `;4;1/2;<pct>` | turn start / turn end, "however the turn ended" incl. Esc ([iTerm2 `HookEvent.swift:40-70`](https://gitlab.com/gnachman/iterm2/-/blob/master/cc-status/Sources/cc-status/HookEvent.swift#L40-L70), measured on 2.1.274) | `terminalProgressBarEnabled` (default `true`) AND `rtt()`: stdout TTY, not `WT_SESSION`, and `TERM_PROGRAM=ghostty`>=1.2.0, `iTerm.app`>=3.6.6, or ConEmu vars | Only by spoofing `TERM_PROGRAM`+`TERM_PROGRAM_VERSION` (see side effects below) | **B** (`rtt`, settings default) + **D** (iTerm2 comment). Turn-edge timing not re-measured |
| Claude | `OSC 133;A;redraw=0` (turn start), `133;C`+`133;D` (turn done) | turn start/end | only in screen-reader mode, non-fullscreen, not WezTerm (`p4()`) | not without enabling accessibility mode | **B** (`sCe`, `p4`, `Pm`) |
| Claude | `OSC 0/2` title; `OSC 21337` tab status | title: session; tab status: code path disabled (`m7t()` returns false) | title off with `CLAUDE_CODE_DISABLE_TERMINAL_TITLE=1` | title already flows; content not characterised (UNVERIFIED) | **B** |
| Codex main (0.163.0-alpha.8+; NOT 0.157.1, NOT stable 0.162.1) | `OSC 7501 ; state=<s>:app=codex[:kind=permission\|question\|auth]` ST, `state=clear` at exit/handoff | `Blocked(kind)` on approval/question/auth, `Working` on task/turn/background process, `Idle`, `Done`, `Error` | emitted whenever stdout is a TTY. No query, no config. Plus `OSC 21337` only for direct iTerm2 | already on, once Hive runs a 0.163+ Codex | **S**: [`tui/src/terminal_program_status.rs`](https://github.com/openai/codex/blob/0884c8047af8b02df31da8c622110a724c6b3cbc/codex-rs/tui/src/terminal_program_status.rs) (PR #52725 `4bad6d7`, 2026-10-09; #54114 `dec5491` adds done/error + reasons), `chatwidget/status_surfaces.rs:327`. Contained in `rust-v0.163.0-alpha.8` (GitHub compare API); absent in 0.162.1 (diverged, commit is 4 ahead) |
| Codex | `OSC 9 ; <msg>` or BEL | `agent-turn-complete`, `approval-requested`, `plan-mode-prompt`, `async-question` | `tui.notifications` (default true), `tui.notification_method` (`auto`=OSC 9 only for Ghostty/iTerm2/Kitty/Warp/WezTerm terminal names, else BEL; `osc9`; `bel`), `tui.notification_condition` (`unfocused` default, or `always`). Unfocused needs terminal focus events (`CSI ? 1004`) | CLI `-c tui.notification_method=bel -c tui.notification_condition=always` (the `-c` form is UNVERIFIED against 0.157.1) or a `config.toml` Hive writes | **S**: `tui/src/notifications/mod.rs`, `chatwidget/notifications.rs`, `config/src/types.rs:710-812`; **D**: [config reference](https://learn.chatgpt.com/docs/config-file/config-reference); `notification_method`/`osc9`/`bel` strings present in the 0.157.1 binary (**B**) |
| Codex | `OSC 0` title | spinner/`activity` while working, action-required message when blocked | `tui.terminal_title`; `null` disables. Default: docs say `["spinner","project"]`, source comment says `activity, thread-name, project-name`. Discrepancy, UNVERIFIED which ships | already on | **S** `tui/src/terminal_title.rs:104-108`; **D** |
| Codex | hooks (not an escape sequence) | `PreToolUse`, `PermissionRequest`, `PostToolUse`, `SessionStart/End`, `UserPromptSubmit`, `Stop`, `Subagent*`, `Pre/PostCompact`, `Interrupt` | `features.hooks`, `hooks.json` or inline `[hooks]` | writable by Hive | **S** `codex-rs/hooks/src/lib.rs:25-38`; **B** event names in 0.157.1 (`strings`); **D** config reference |
| Gemini (main) | `OSC 0 ; <title>` | title encodes state: `✋  Action Required`, `⏲  Working…`, `◇  Ready`, `✦  <thought>` | `ui.hideWindowTitle` false, `ui.dynamicWindowTitle` true (both default) | already on | **S** `packages/cli/src/utils/windowTitle.ts:32-110`, `ui/AppContainer.tsx:2439`, `config/settingsSchema.ts:547-584` |
| Gemini | `OSC 9 ; t \| s \| b`, `OSC 777;notify;title;body`, or BEL | attention (confirmation, auth, loop detection) and `session_complete` (turn Responding->Idle), suppressed when focused (if focus events were seen) | `general.enableNotifications` default **false**; `general.notificationMethod` `auto\|osc9\|osc777\|bell` (`auto`: iTerm2->OSC 9; Alacritty/Apple/VS Code/Windows Terminal->BEL; everything else incl. unknown->OSC 777) | settings file only; `GEMINI_CLI_SYSTEM_SETTINGS_PATH` env points at one (`config/settings.ts:107`). No CLI flag found | **S** `packages/cli/src/utils/terminalNotifications.ts`, `ui/hooks/useRunEventNotifications.ts:60-175` |
| Gemini | hooks | `BeforeTool`, `AfterTool`, `BeforeAgent`, `Notification`, `AfterAgent`, `SessionStart/End`, ... | settings | writable | **S** `packages/core/src/hooks/types.ts:43-55` |
| Copilot CLI | `OSC 9;4` progress | during work | `terminalProgress` setting (since 1.0.51); default UNVERIFIED | settings file (key location UNVERIFIED) | **D** [changelog](https://github.com/github/copilot-cli/blob/main/changelog.md) lines 1567, 385 |
| Copilot | `OSC 777` notification | attention/idle | "optional", only direct Ghostty/WezTerm (1.0.88). Desktop toasts since 1.0.66 | no (needs terminal name) | **D** changelog 197, 1004 |
| Copilot | BEL | agent finishes | off unless enabled in config since 1.0.55; config key UNVERIFIED | UNVERIFIED | **D** changelog 1494, 2611 |
| Copilot | `OSC 0` title | session title + "GitHub Copilot" | on; `COPILOT_DISABLE_TERMINAL_TITLE` opts out | already on | **D** changelog 966, 973, 1918 |
| Copilot | hooks | `agentStop`, prompt submit, error, and a `notification` event (permission, elicitation, completion) | `~/.copilot/config.json` (per a third-party doc, UNVERIFIED) | writable | **D** changelog 2043 |
| Aider | BEL (`\a`) | LLM finished and waits for input | `--notifications` (default off). With it on and a notification command resolvable, runs that command instead (macOS: `terminal-notifier`/`osascript`, which pops a *host* notification, not a PTY byte). BEL only when no command exists (Linux without `notify-send`/`zenity`, or unknown OS) | `--notifications --notifications-command "printf '\a'"` makes it deterministic | **S** [`aider/io.py:1054-1103`](https://github.com/Aider-AI/aider/blob/main/aider/io.py), **D** [docs](https://aider.chat/docs/usage/notifications.html) |
| Pi 1.0.4 | `OSC 9;4;3` repeated every 1 s, `;4;0` clear | `turn_start` -> `agent_end`; compaction start/end | `terminal.showTerminalProgress`, default **false** | would need Hive to edit user settings. Not needed: the extension tier is exact | **S** `dist/modes/interactive/interactive-mode.js` (turn_start ~2743, agent_end ~2936), `pi-tui/dist/terminal.js:8-9,425-445`, `core/settings-manager.js:940` |
| Pi | `OSC 0` title | static `<APP_TITLE> - <session> - <cwd>` | always | already on | **S** `interactive-mode.js:814-817`, `terminal.js:423` |
| Pi | `OSC 133;A/B/C` prompt zones | in rendered lines (zone marks, stripped in alt-screen) | n/a | n/a | **S** `pi-tui/dist/layout.js:6`, `tui-alt-screen.js:23-24`. Emission conditions UNVERIFIED |
| Shell | `OSC 133;A/B/C/D;status` | only if shell integration is sourced | rc-file injection; Hive injects none | M-L | [iTerm2 shell integration](https://iterm2.com/documentation-shell-integration.html); no Hive code (grep) |

### Side effects of spoofing `TERM_PROGRAM=iTerm.app` + `TERM_PROGRAM_VERSION>=3.6.6`

All read in source or binary. This is why it is not a recommendation.

- Claude: `auto` notifications switch to `iterm2` (OSC 9) (`I()`), Shift+Enter/clipboard setup paths change, iTerm2-specific fullscreen clear ([changelog 2.1.292](https://raw.githubusercontent.com/anthropics/claude-code/main/CHANGELOG.md)).
- Codex: `supports_osc9` true; `OSC 21337` emitted (direct iTerm2 and no multiplexer) (`terminal_program_status.rs`).
- Gemini: `auto` chooses OSC 9.
- xterm.js in the GUI does not implement iTerm2's proprietary sequences; unhandled OSC is dropped (standard parser behaviour, UNVERIFIED for the pinned xterm.js).

## Hive's bespoke channels and their OSC equivalents

| Hive channel | Where | What it provides | Standard-sequence overlap |
|---|---|---|---|
| `hived hook` (`AGENT_EVENT` over the `event` socket) | `cmd/hived/hook.go:271-340`, `control-plane.md:72-100` | Claude turn boundaries, permission/question waits, tool start/end, plan, subagents, prompt/summary text | 7501 `working/blocked(kind)/done/error` overlaps *state* only. Tools, plan, subagents, text have no OSC equivalent |
| Pi extension | `internal/agent/pi/hive.ts`, `agent-activity.md` | exact state, tools, plan, heartbeat | Pi emits only `9;4` (off by default) and static title. No overlap worth taking |
| ACP stream | `control-plane.md:41` | exact state, permissions, tools | none (the daemon owns the stream) |
| Heuristic: output cadence | `internal/agentstate/machine.go` | working/idle | `OSC 9;4`/7501 `working` is a direct replacement for "bytes arrived" |
| Heuristic: BEL scanner | `internal/session/bell.go`, `session.go:377-390`, `registry.go:579`, `machine.go:361` | any real BEL -> `waiting_input` on EVERY tier | BEL is the lowest common denominator of Codex/Gemini/Copilot/Aider notifications. The scanner already discards OSC terminators correctly |
| Heuristic: OSC 0/2 title | `session.go:63-90`, `registry.go:560`, `vt.go:120-131` | display only (`SessionInfo.title`). State is not derived from it (`registry.go:850` only announces) | Gemini and Codex titles carry state words today. Unused |
| Laya classifier | `laya-state-classifier.md`, `registry/classify.go` | screen -> state for sessions with no live tier | 7501 `blocked/kind` and Codex/Gemini titles answer the same question exactly, for free, for those agents |
| Screen-scrape (none beyond Laya) | | | |
| Generic report frame | absent (herdr note, "Generic report state") | | 7501 is the standard version of herdr's `pane.report_agent` |

What Hive does **not** parse at all (grep of `internal/session`, `internal/registry`, `cmd/hivegui/frontend/src`): OSC 7501, 9, 9;4, 99, 133, 777, 1337, 21337. The GUI registers no `registerOscHandler` and has no `onBell` (`session-term.ts:770` comment). The daemon answers no terminal queries (no DA, XTVERSION, or OSC replies in `internal/`); xterm.js in an attached GUI does.

## Recommendations per overlap

Trust tier key: **hook** = same as `hook`/`extension`/`acp`; **osc** = a new tier above `heuristic`/`laya`, below the three trusted tiers, never overriding a fresh one; **bell** = today's BEL rule.

| # | Overlap | Recommendation | Trust | Tradeoffs | Effort |
|---|---|---|---|---|---|
| 1 | Claude state (hooks vs 7501) | **Both, hook wins.** Keep hooks (richer: tools, plan, subagents, text, plan-review decisions). Add 7501 as `osc` tier to close the **Esc-interrupt gap** (Esc fires no hook per the herdr note) and to cover hook breakage. Gate on a probe first: confirm on a credentialed 2.1.300 what Esc/`done`/`error` emit | osc | Needs the daemon to answer `OSC 7501 ; ?` within 2 s of spawn (see 2). Claude-version-specific behaviour, so probe-test like `TestClaudeProbeTaskToolsOptIn`. Blocked on empirical check I could not run without credentials | M (+ S probe) |
| 2 | Answering the capability query | **Daemon answers, agent sessions only, live stream only.** Reply `ESC ] 7501 ; ? ESC \` from `readLoop` when it sees the query in the first N seconds of a non-shell agent session. Not the GUI | n/a | GUI answering is wrong: it is absent for headless sessions, and ring replay on attach would re-trigger every stored query. Daemon replying writes bytes into the PTY, so a hostile `cat` of a file containing the query injects keystrokes into whatever reads stdin. Hence agent sessions only, once per spawn. Bump `DaemonContract` (daemon behaviour, `AGENTS.md`) | S-M |
| 3 | Codex state | **Stage it.** (a) now: `-c tui.notification_method=bel -c tui.notification_condition=always` through `SpawnArgs`, feeding the existing bell scanner (turn-complete and approvals, indistinguishable). (b) when 0.163 stable ships: parse 7501 (no query, no config, includes `kind`). (c) separately evaluate Codex hooks as a true hook tier, which supersedes both and fixes `control-plane.md:302` | (a) bell, (b) osc, (c) hook | (a) loses permission-vs-turn-end distinction and relies on focus semantics being bypassed by `always`; `-c` unverified on 0.157.1. (b) cheap once parsing from rec 1 exists. (c) is the real answer but is a feature (payload shapes, version probe, fixtures) | (a) S, (b) S, (c) L |
| 4 | Gemini state | **Parse the OSC 0 title as an `osc`-tier hint**, no config needed (`✋ Action Required` -> blocked, `⏲/✦` -> working, `◇ Ready` -> idle). Optional: enable `general.enableNotifications` via a Hive-written settings file for BEL/OSC 777 on completion. Hooks are the exact path (L) | osc | Title text is a UI string, can change in any release (pin with a fixture). Title strings are also attacker-influenced via the model's "thought subject" (`✦  <thought>` branch) so only match the three fixed prefixes, never the free text. Notifications: setting is global to the user's Gemini, intrusive to write | S (title), M (notifications) | 
| 5 | Copilot state | **Do nothing now.** Closed source, config keys UNVERIFIED, bell off by default, OSC 777 only for Ghostty/WezTerm. Revisit with its hooks (`notification` event) | n/a | Cannot be probed without the binary | n/a |
| 6 | Aider state | **Pass `--notifications --notifications-command "printf '\a'"`** via `SpawnArgs`. Gives a BEL when it waits for input, deterministic on every OS | bell | One more flag Hive owns; it silences Aider's host notification (macOS `osascript`), which a user may want. Make it a setting like `pi_todo_tool` | S |
| 7 | Pi | **Keep the extension.** Pi's `9;4` is off by default and weaker | hook | none | n/a |
| 8 | `OSC 9;4` from Claude (herdr note, candidate 1) | **Drop it.** Superseded by 7501 (rec 1), and enabling it needs a `TERM_PROGRAM` spoof | n/a | See spoof side effects above | n/a |
| 9 | Hive env hygiene (`TERM_PROGRAM` leak) | **Scrub or set `TERM_PROGRAM` deterministically in `sessionEnv`.** Today agents detect "iTerm.app" or nothing depending on who launched `hived`. This changes Claude/Codex/Gemini notification channels per launch | n/a | Unsetting forces Claude `auto` notifications off (already the common case from Finder). Setting a Hive value (`TERM_PROGRAM=hive`) is safe: it matches none of the allowlists above (UNVERIFIED for agents not read) | S |
| 10 | Laya | **Keep, but with a smaller audience.** 7501/title tiers feed Claude/Codex/Gemini, leaving Laya for Aider, Copilot, shells, custom. `Classifiable` must treat a live `osc` tier like it treats a trusted tier | n/a | 7501 has no heartbeat, so an `osc` `working` from a crashed or forged source can stick; see trust | S |
| 11 | Shell sessions | **Out of scope here.** OSC 133 from shell integration would give prompt vs running + exit status for shells, none of the agents rely on it | osc | rc-file injection per shell, Windows/PowerShell parity | L |

## Trust model

- **Forgery surface.** Any bytes reaching the terminal are indistinguishable from the agent's. That includes `cat`/`curl`/`git log` output in a shell, and whatever an agent TUI prints from a file or tool result. Claude strips control characters in its own notification text (`m(r)` in the binary) but that is the agent's discipline, not a guarantee. Spec says the same: all report content is untrusted, `msg`/`title` must not contain control characters, records must not be readable back ([spec](https://gist.github.com/mitchellh/7acae3abd8355c1c00287d67e96c913a)).
- **Compared with the hook socket.** `HIVE_SESSION_ID` and the `event` socket are inherited by every process in the session (`control-plane.md:89-95`), so a process in the tree can forge hook events too. What the hook channel resists, and PTY bytes do not, is forgery by *data* that only gets displayed. That is the reason for a separate lower tier.
- **Direction matters.** A forged `blocked`/`working` costs a false alert or a stuck "working" (annoying, bounded). A forged `idle`/`done`/`clear` can hide a real wait (worse). Hive's BEL rule already takes the first risk (`noteBell`, `registry.go:579`: every bell becomes `waiting_input`, forgeable by `cat` of a binary).
- **Rules for an `osc` tier.**
  1. Never override a fresh `hook`/`extension`/`acp` state, and never write `hookSeenAt`/`orderAt`/`lastEventAt` (the same rule `laya` follows).
  2. Take only `state` and `kind`. Do not display, log, or act on `msg`/`title`/`id`; `app=` must equal the session's agent id (spoofable, but raises the bar).
  3. Let `blocked` raise attention freely. Let `idle`/`done`/`clear` demote `working` only if PTY output has been quiet (the spinner redraws constantly while an agent works).
  4. Cross-check `working` against output cadence, so a forged or orphaned `working` expires; 7501 itself has no heartbeat or timer.
  5. Parse only the live stream, never ring replay or snapshot.
  6. Cap at one record per session (root id), drop on process exit, clear on `state=clear`.
  7. Replying to the query only for agent sessions (rec 2), so a hostile file cannot make the daemon type into a shell.

## Open questions

- What does Claude 2.1.300 emit over 7501 on Esc-interrupt, on a permission prompt vs `AskUserQuestion`, and at turn end (`done` vs `idle`)? Needs one credentialed run; I replied to the probe on a bare PTY and saw no state before login.
- Does Claude's `idle` stay set at the prompt after a turn, and does it clear on exit? Matters for stale-state handling.
- Is the hooked tier's `PermissionRequest` wait *earlier* than the 7501 `blocked`? If 7501 lags hooks, it adds nothing except on Esc.
- When does Codex 0.163 go stable, and does the `-c tui.notification_*` override apply on the version users run?
- Does `TERM_PROGRAM` leaking from the launcher already change behaviour for users who start `hived` from iTerm2 (e.g. Claude's `iterm2` notification channel)? Worth a `launch_diag`-style check.
- xterm.js version pinned in the GUI: does it drop unknown OSC (7501, 21337) without echoing? Standard, not verified.
- Copilot: where does `terminalProgress` live and what is its default? Closed source; ask the user to run `copilot --help`.
- Gemini: is `GEMINI_CLI_SYSTEM_SETTINGS_PATH` highest or lowest precedence versus the user's own settings? Decides whether Hive can enable notifications without writing into `~/.gemini`.
- Do you want an `osc` tier name on the wire (`state_source`), or reuse `heuristic` with a sub-reason? A new tier value is a wire change (all three clients, `DESIGN.md` rule) and a contract bump.

## Probe results (credentialed, 2026-10-15)

Claude Code (`claude --model haiku`, user's own login, real Anthropic backend) in a bare Python PTY: `TERM=xterm-256color`, no `TERM_PROGRAM`, harness replies `ESC]7501;?ESC\` to the startup query. Evidence: harness output of two runs, scratchpad `probe.py` / `probe2.py` (not committed).

| Event | Observed 7501 |
|---|---|
| Startup, after reply | `state=idle:app=claude-code` (about 0.5 s) |
| Turn start | `state=working` |
| Tool use | second `state=working:...:msg=<b64 tool description>` |
| Turn end | `state=done` (not `idle`) |
| `AskUserQuestion` shown | `state=blocked:kind=question:msg=<b64 question>` |
| Esc during a turn | `state=idle` (so Esc is observable; no hook fires) |
| Esc on a pending question | `state=idle` |
| Exit | `state=clear` (x3) |

- The reply format `ESC]7501;?ESC\` is accepted: status flows. Settles the query-reply open question.
- `blocked:kind=permission` is emitted for a permission dialog (Claude 2.1.301, `--permission-mode default`, `touch probe.txt`): `state=blocked:app=claude-code:kind=permission:msg=<b64>`. Esc on the dialog then reports `idle`. (The first probe, under this account's own permission settings, never showed a dialog; the Go probe forces one.)
- Esc during a running tool (`sleep 25`) also reports `idle`, then the interrupt's own hooks (`PostToolUseFailure`) fire after it. Recorded by `TestClaudeProbeOSC7501EscGoesIdle` (opt-in, `HIVE_PROBE_CLAUDE=1`) into `internal/session/testdata/osc7501/claude-2.1.301-probe.bin`.
- Note `done` follows a turn and `idle` follows Esc/startup: `done` is "finished", `idle` is "nothing running".
- Every sequence in the recorded stream, status reports included, ended with BEL, not ST (the `testdata/osc7501/claude-2.1.300.bin` fixture); the scanner accepts both.
