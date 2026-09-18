---
issue: 431
title: "Find text in a session, including alt-screen agents"
type: enhancement
complexity: L
priority: P2
stage: IMPLEMENT
supersedes: 430
---

# Find text in a session, including alt-screen agents

- **Issue:** [#431](https://github.com/lucascaro/hive/issues/431)
- **Supersedes:** [#430](430-find-text-in-a-session-with-cmd-f.md) (in-terminal ⌘F), merged into this spec

## Problem

There is no way to find text in a session. Scrolling and reading is the whole workflow, and on agent sessions it is worse than tedious — it is impossible: Claude, Codex and pi run on the terminal's alternate screen buffer, which by definition keeps no scrollback, so the GUI holds exactly one screenful. Text from five minutes ago is simply gone.

That split is why this is one feature and not two. An in-terminal find that searches the xterm buffer is the right answer for a shell session and a useless answer for an agent — which is what held [#430](430-find-text-in-a-session-with-cmd-f.md) at PLAN. Meanwhile the agent's full history is sitting on disk in its own JSONL transcript the entire time. The user pressing ⌘F does not care which of these is true; they want to find text in the session in front of them.

## Desired behavior

⌘F opens a find box for the focused session, in place, with the input focused. Typing searches incrementally with matches highlighted and a live `n/total` count; Enter / Shift+Enter and prev/next controls step between matches. Esc or the close control dismisses it and returns the terminal exactly as it was.

**The source is chosen from the terminal's buffer type, and the user is never asked.** On a regular text session the search runs over what the terminal buffer holds, highlighting in place. On an alt-screen session it runs over the agent's transcript on disk, rendered over the terminal, so it finds text that scrolled off long ago. Alt-screen sessions with no readable transcript say so plainly instead of offering an empty box.

## Success criteria

1. ⌘F on macOS (Ctrl+Shift+F elsewhere) opens the find box for the focused session, input focused, no click required.
2. Typing updates highlights and the `n/total` count on every keystroke, with no explicit submit.
3. Enter, Shift+Enter, and the prev/next controls each move the active match; the active match is centered and readable in its surrounding context.
4. Esc and the close control both dismiss it and restore the terminal to its prior state: same scroll position, same keyboard focus, no visible repaint artifact.
5. On a regular text session, the search finds text anywhere in what the terminal buffer holds, including scrollback that is off-screen.
6. On an alt-screen session, a string that scrolled off long ago — and is therefore unfindable in the terminal buffer — is found.
7. The source is selected automatically from the buffer type. The user is never asked which, and no separate binding exists for the two.
8. Text that arrives while the find box is open becomes findable without closing and reopening it, on both sources.
9. Alt-screen sessions with no readable transcript (Codex, gemini, copilot, a `vim` on a shell session) render an explicit "no searchable history" state, not an empty or broken box.
10. Opening and closing while a session is actively streaming output neither stutters the terminal nor loses the pre-existing scroll position.
11. After an alt-screen agent exits, find still works on that same session's normal buffer — searching a running agent must not silently break search for the rest of the session's life.

## Non-goals

- **Reducing match noise.** No grouping of consecutive matches from one tool-result block, no filtering by role or message kind. The spike showed `error` returns 41 hits mostly from a single `npm` failure; shipping raw matches first is a deliberate choice.
- **Cross-session search.** One session at a time. No "which of my sessions mentioned X".
- **Transcript search for agents other than Claude and pi.** Codex, gemini and copilot get criterion 9's explicit empty state, not a transcript. Named as next, not built here.
- **Grid view.** The chord is gated to single-session view; in grid it does nothing.
- **Regex, whole-word, or case-sensitive toggles.** Plain substring, case-insensitive. The UI leaves room for them.
- **A durable archive or index.** Reads whatever the agent wrote; builds no store, no index, no retention of its own.
- **Editing, resuming, or acting on transcript content.** Read-only.
- **The daemon's byte ring.** Not this feature's source; it remains available for a future shell-scrollback-beyond-xterm route (`../design-docs/session-history-search.md`).

## Notes

**Why one binding.** Operator decision, verbatim: *"cmd-f for both, this is the same as 430, but for alt-screen sessions. cmd-f is text search for regular text sessions and transcript search for alt-screen agents."* The discriminator is the buffer type rather than the agent type, which is the sharper rule — alt-screen is precisely *why* buffer search fails, so it is the condition that should select the source.

**The merge removes a defect rather than adding one.** #430's PoC found that **any** search performed while the alt buffer is active permanently poisons `@xterm/addon-search` for the normal buffer — untreated, that is "search a Claude session, Claude exits, search again, silently find nothing, forever". Under the buffer-type rule the addon is never run against the alt buffer at all, so the failure mode is designed out. Criterion 11 pins it.

**UX validated by a throwaway spike.** Three variants were built against a real 3000-line Claude transcript and driven in a real browser (Playwright): A = transcript takeover over the terminal, B = side results panel, C = centered command-palette modal. The operator chose **A**. The spike was ephemeral; the verdict is recorded here and in [session-history-search.md](../design-docs/session-history-search.md).

**pi transcript mapping — resolved, and it is exact.** `internal/registry/create.go:587-590` passes Hive's own entry id as pi's `--session-id`, and pi writes that string verbatim as the transcript filename suffix (proven by a Hive probe transcript named `..._hive-probe-1788745415.jsonl`, which is not a uuid). Across 91 real pi transcripts: 46 UUIDv4 (Hive spawns), 44 UUIDv7 (pi's own), every id unique. Resolution is `glob(<encoded-cwd>/*_<hiveSessionID>.jsonl)`. The directory encoding **preserves dots**, unlike Claude's, so `encodeClaudeProjectDir` must not be reused.

**Open question — the noise tension.** Match-noise reduction is a non-goal, yet "lands me on the right line, not near it — failure = I find 41 hits and still have to hunt" was also chosen as a done-signal. Criterion 3 takes the narrow reading (navigation precision), not the strong one (fewer hits). If real use shows the strong reading was meant, the noise non-goal is the thing to revisit first.

**Prior research, already done — do not re-derive.** [session-history-search.md](../design-docs/session-history-search.md) documents three candidate sources with citations and costs: the daemon's 8 MiB raw byte ring, un-skipping the VT's alt-screen scrollback eviction, and agent transcripts (chosen). #430's exec plan carries the reviewed terminal-layer findings, all folded into this feature's plan: plain Ctrl+F is `0x06` (readline `forward-char`) so non-mac chords need Shift; the macOS native menu accelerator intercepts before the webview (`cmd/hivegui/frontend/src/app/keyboard.ts:899-902`); four distinct viewport-yank sites exist around bottom-follow and need a dedicated `_searchActive` flag; and the addon caps `resultCount` at 1000.
