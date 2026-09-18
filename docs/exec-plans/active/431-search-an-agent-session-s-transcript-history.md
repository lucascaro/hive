# Find text in a session, including alt-screen agents

- **Spec:** [docs/product-specs/431-search-an-agent-session-s-transcript-history.md](../../product-specs/431-search-an-agent-session-s-transcript-history.md)
- **Issue:** #431
- **Status:** active
- **PR:** [#432](https://github.com/lucascaro/hive/pull/432)
- **Branch:** `feature/431-find-text-in-a-session`

## Summary

Adds a transcript-history search to the GUI: a dedicated keybinding opens the focused session's
on-disk agent transcript as an overlay above its terminal, with incremental search, live match
count, and prev/next navigation. The source is the agent's own JSONL file rather than the xterm
buffer, so it finds text that scrolled off an alt-screen agent long ago. Claude and pi only in
this pass.

## Research

### Conventions card

```
module: github.com/lucascaro/hive
build:  ./build.sh
test:   scripts/test.sh            # layers: go · unit · dom · e2e
        npm run test:e2e:real      # separate; real hived, isolated temp dirs
bins:   hived · hivegui · hived-ws-bridge
```

- **TDD is mandatory.** No behaviour change ships without the test that verifies it. "Boil the lake": golden path *and* key edge cases in the same PR.
- **Layer rule** (`DESIGN.md`): `wire -> session/agent/worktree -> registry -> daemon`. The GUI never opens a PTY and never imports `internal/session`; everything crosses the wire.
- **Wire JSON is `snake_case` on the wire, `CamelCase` in Go** (`json:"snake_case"` tags). JS readers use `snake_case ?? camelCase` at the boundary.
- **Keybinding changes touch five surfaces** (`src/lib/shortcuts.ts:1-14`): the handler in `app/keyboard.ts` (+ a `lib/keymap.ts` predicate when platform-conditional), `shortcuts.ts`'s `shortcutGroups()` **and** `paletteShortcuts()`, the palette command table in `main.tsx`, `cmd/hivegui/menu_darwin.go` (Cmd chords), and the Keybinds table in `README.md`.
- **CSS:** global files in `src/theme/components/*.css`, no CSS Modules. `hv-*` class names are a contract with the Playwright specs (`docs/design-docs/ui/README.md`). `scripts/ui-lint.sh --strict` rejects raw hex, `font-size: Npx`, `border-radius: Npx` outside `tokens.css`, and icon-shaped Unicode glyphs (use the SVG sprite via `icon()`).
- **Changeset required** (user-visible): `.changesets/<slug>.md`, `type: added`, `bump: minor`. Never edit `CHANGELOG.md`.

### Prior lessons

`brain-search` over transcript/search/overlay/keybinding/jsonl/scrollback/xterm returned zero hits. No prior lessons matched.

### Transcript sources on disk

**Claude** — `~/.claude/projects/<encoded-cwd>/<session-uuid>.jsonl`. The encoder already exists and is tested: `encodeClaudeProjectDir` (`internal/agent/claude.go:18-31`) folds `/`, `.` and `:` to `-`. Hive pins its own session id via `--session-id` (`internal/agent/agent.go:134-147`), so `AgentSessionID` == Hive's session id and the path is an **exact derivation**. `claudeSessionExists` (`claude.go:42-53`) only `os.Stat`s it.

**pi** — `~/.pi/agent/sessions/<encoded-cwd>/<timestamp>_<session-id>.jsonl`. **The spec's open question is resolved: this is an exact mapping, not a heuristic.** `internal/registry/create.go:587-590` appends `def.SessionIDFlag, id` at first spawn, passing Hive's own entry id as pi's `--session-id`, and pi writes that string **verbatim** as the filename suffix. Evidence, gathered on this machine:

- A Hive probe transcript is named `2026-09-07T01-43-35-209Z_hive-probe-1788745415.jsonl` — the suffix is not a uuid at all, which proves it is the caller-supplied `--session-id` echoed literally rather than an id pi generated.
- Across all 91 pi transcripts: 46 carry a UUIDv4 suffix (Hive spawns — `internal/registry/create.go:305` uses `uuid.NewString()`, which is v4), 44 carry UUIDv7 (pi's own generation for non-Hive launches), and one carries the literal probe string.
- **Every id is unique** — no session id appears in two files, so there is no restart-fragmentation case to merge today.
- The filename suffix equals the first record's `id` field, verified on a real file.

Resolution is therefore `glob(<encoded-cwd>/*_<hiveSessionID>.jsonl)`: exactly one file, no recency ranking, no first-record `cwd` confirmation, and no sibling-session ambiguity. The glob (rather than a pure path join) is needed only because the timestamp prefix is not derivable; if pi ever does write a second file for one session id, sorting the glob by filename timestamp concatenates them in the right order for free.

Directory encoding, verified empirically: `"--" + cwd[1:].replace("/", "-") + "--"`. **It preserves dots** — `/Users/lucascaro/checkout/hive/.worktrees/twilight-gate` encodes to `--Users-lucascaro-checkout-hive-.worktrees-twilight-gate--`, with `.worktrees` intact. `encodeClaudeProjectDir` folds `.` to `-` and must not be reused. No pi path code exists in the repo today (`internal/agent/pi.go` only plumbs Hive's own extension file).

**Everything else** — codex and copilot capture ids by reading a first line for `cwd`/`id` only (`codex.go:64-77`, `copilot.go:111`); gemini and aider have no path code at all; shells have no transcript. All of these render the "no searchable history" state.

**Confirmed:** no code path in `internal/agent/` reads transcript *content* today. This feature introduces the first one.

### Record shape (Claude, verified against a real 183-record file)

Displayable text is nested and polymorphic at two levels:

| record | key path | shape |
|---|---|---|
| `user` (plain) | `.message.content` | string |
| `user` (tool result) | `.message.content[] type=="tool_result" .content` | string **or** array of blocks |
| `assistant` text | `.message.content[] type=="text" .text` | string |
| `assistant` thinking | `.message.content[] type=="thinking" .thinking` | string |
| `assistant` tool call | `.message.content[] type=="tool_use" .input` | object (needs stringify) |

`system` records carry text scattered across `hookAdditionalContext` / `hookInfos` / `hookErrors` / `stopReason` with no single key path. Records of type `user`/`assistant`/`system`/`attachment` carry an ISO-8601 `timestamp` and a stable per-record `uuid`; `parentUuid` makes the file a thread, so file order is the only reliable display order. Non-message record types (`ai-title`, `cost-state`, `mode`, `queue-operation`, `file-history-snapshot`, ...) carry no transcript text and are skipped.

### The size constraint — this is the load-bearing finding

- `wire.MaxPayload = 1 << 20` — **1 MiB per frame**, enforced on both write and read (`internal/wire/frame.go:32-34,274-310`).
- Real transcripts on this machine: median ~365 KB, this worktree's own are 4.0 and 4.5 MB, **largest is 21.9 MB**.
- There is no JSON chunking precedent. The only large-payload precedent is `FrameRequestReplay`, which streams a begin-event, a run of `FrameData` frames, then a done-event (`frame.go:65-71`).

So shipping the transcript to the GUI is off the table, and so is rendering it: the frontend has **no virtualization anywhere** (grep for `virtual` across the frontend: zero hits; `ActivityPanel.tsx:57-64` and the sidebar tree both render full lists). Both problems have the same answer — the daemon searches, and the GUI only ever holds a window of lines.

### Wire: the additive frame-pair checklist

GET_ACTIVITY / ACTIVITY (spec 416) is the template to mirror exactly:

1. `internal/wire/frame.go:170-171` — new `FrameType` consts, appended monotonically; `String()` cases at `frame.go:260-263`. New frame types never bump `PROTOCOL_VERSION` (`frame.go:22-29`).
2. `internal/wire/control.go:656-681` — request/response payload structs.
3. `internal/wire/client.go:100-134` — `controlEvents` map + `ControlEventName`.
4. `cmd/hivegui/app_calls.go:643-649` — the Wails-exported method.
5. `cmd/hivegui/app_control.go:452` — generic dispatch; covered by step 3, no per-frame code.
6. `cmd/hived-ws-bridge/main.go:370-378` — the JSON-RPC verb.
7. `internal/wire/testclient/client.go:208-210` — test-client wrapper.
8. `internal/daemon/daemon.go:1280-1293` — the handler arm.
9. `cmd/hivegui/frontend/src/app/events.ts:448-454` + the hand-kept TS payload mirror (`src/lib/activity.ts:35`). No codegen exists.
10. `internal/buildinfo/contract.go:147` — `DaemonContract` is **14**; bump when a GUI built against this tree could not drive an older daemon. `scripts/check-daemon-contract.sh` fails CI on a daemon-touching PR without a bump.

The handler arm's shape (`daemon.go:1280-1293`): `decodeReq[T](payload, ops.sendError)` -> domain lookup -> `ops.writeJSON(FrameX, resp)`, answering on the same connection. Note `sessionModeFrames`: a `ModeSession` connection is refused frames not on that allowlist, and `TestSessionModeCannotGetActivity` (`internal/daemon/activity_test.go:226-256`) pins that gate.

### Frontend: overlay, focus, and the resize trap

- **Dialog roots are static siblings of `#terms`** in `index.html:126-159`, `class="hv-dialog hidden" role="dialog" aria-modal="true"`, stacked over the terminal purely by CSS. They do **not** occupy terminal layout, which matters: `session-term.ts:300` installs a `ResizeObserver` whose `_onBodyResize()` (`session-term.ts:1027-1160`) calls `fit.fit()` (`:1062`) and can trigger scrollback replay. An overlay that altered the terminal's box would repaint it — the existing sibling-root pattern avoids this by construction.
- **Store-driven visibility:** `modals: ModalEntry[]` (`store/store.ts:114`), `openModal` (`:1027`), `closeModal` (`:1037`), `isModalOpen` (`:1043`). The `seq` field is the opening generation — components key a remount on it to reset per-open state (search text, selection), which is exactly what this overlay needs.
- **Shared chrome:** `components/modals/ModalShell.tsx` owns Escape, backdrop click (with the down/up guard at `:97-112`) and Tab containment via `trapFocus` (`:88-92`). It deliberately does **not** own initial focus or hide-on-close.
- **The close dance is the delicate part**, and `app/modals/help-overlay.ts:32-40` already solves it: `releaseFocus(root)` **before** unmount so focus is not stranded on a removed node, then `flushSync(() => closeModal(id))` because a plain store write lands a microtask later and `focusActiveTerm()` would fire while the overlay is still visually up, then `focusActiveTerm()`. `toggleX` exists as one entry point because the macOS native menu accelerator must both open and close through it.
- **Focus primitives:** `lib/focus-trap.ts` — `focusableWithin` judges visibility by the `.hidden` class rather than layout, deliberately so jsdom can test it.

### Keybinding surface

There is no declarative keymap registry: `lib/keymap.ts` is a set of named predicates over a structural `KeyEventLike`, called from the imperative ladder in `app/keyboard.ts` (dispatch begins at `:394` after `const meta = cmdOrCtrl(e)`). `lib/platform.ts:26-31`'s `cmdOrCtrl` is Cmd-only on mac, Ctrl-only elsewhere, and rejects the cross-modifier combo. Help overlay and command palette both read `lib/shortcuts.ts` exclusively so the two cannot drift.

Taken chords include ⌘T/⇧, ⌘P/⇧, ⌘W/⇧, ⌘Z, ⌘1-9, ⌘↑↓←→, ⌘N/⇧, ⌘⌫/⇧, ⌘[/], ⌘E, ⌘I/⇧, ⌘G/⇧, ⌘↩, ⌘S, ⌘J/⇧, ⌘=/-/0, ⌘⇧K, ⌘,, ⌘/ and ⌘?, Ctrl+`, and the in-terminal Ctrl+⇧C/V/A. **⌘F is unclaimed** in both `shortcuts.ts` and `keyboard.ts` — confirmed — and stays reserved for 430. **⌘⇧F / Ctrl+Shift+F is free** and matches the app's own "Shift gets the secondary variant" convention (⌘I -> ⌘⇧I, ⌘G -> ⌘⇧G).

### Test prior art to mirror

| Target | Model |
|---|---|
| Path encoder | `internal/agent/claude_test.go:9-53` `TestEncodeClaudeProjectDir` — table-driven, incl. a dotted-worktree case. `SetClaudeSessionExistsForTest` (`claude.go:64-71`) is the FS-stub pattern. |
| Wire round trip | `internal/wire/wire_test.go:13-42` `TestFrameRoundTrip`; `:44-59` `TestFrameTooLargeOnWrite`/`OnRead` for the 1 MiB boundary; `:72+` `TestHelloWelcomeRoundTrip` for typed JSON pairs. |
| Daemon handler | `internal/daemon/activity_test.go:108-146` `TestGetActivityReturnsRing` — real test daemon, control handshake, request frame, assert response. `:226-256` for the mode gate. |
| Pure JS module | `test/unit/keymap.test.ts` (fake-event builder, table assertions), `test/unit/shortcuts.test.ts` (structural invariants). |
| Overlay DOM | `test/dom/help-overlay.test.tsx` — jsdom, RTL, `document.body.innerHTML` fixture, dynamic `await import`, `resetStore()`, `vi.fn()` dep spies, asserts `.hidden` toggling and spy calls. Supporting: `test/dom/modal-shell.test.tsx`, `test/dom/focus-trap.test.ts`. |
| E2E chord | `test/e2e/keymap-activity.spec.ts` — `page.keyboard.press('Control+Shift+J')`, asserts visibility; `bootAsLinux` (`:9-16`) spoofs platform via `addInitScript`. |
| E2E focus restore | `test/e2e/focus-invariants.spec.ts` — `assertAlignedFocus(page)` polls that `document.activeElement` is an `.xterm-helper-textarea` inside a `.term-focused` tile. This is the criterion-4 assertion. |


## Approach

**One find box, two sources, selected by buffer type.** ⌘F opens a single find box. `term.buffer.active.type === 'alternate'` selects the source: normal buffer → `@xterm/addon-search` over the terminal buffer, highlighting in place; alt screen → the agent's on-disk transcript, rendered over the terminal. The user is never asked and never sees two UIs.

This merges #430 into this feature (operator decision). The two halves share the box, the chord, the count, the prev/next controls and the close-and-restore path; they differ only in where matches come from and how they are painted. Building them behind one binding is *less* work than building them separately, because the shell is built once.

**The merge designs out #430's worst defect.** Its PoC established that **any** search run while the alt buffer is active permanently poisons `SearchAddon` for the normal buffer — `clearDecorations()` does not clear it, an empty query does not clear it, only a fresh instance recovers. Untreated that is: "search a Claude session, Claude exits, search again, silently find nothing, forever." Under the buffer-type rule **the addon is never invoked while the alt buffer is active**, so the poisoning path is unreachable rather than mitigated. The addon is still recreated on `term.buffer.onBufferChange` as defence in depth, and criterion 11 pins the behaviour.

### Source A — normal buffer (folded in from #430's reviewed plan)

Five things the addon does not handle, all of them grounded in #430's PoC and its two review rounds:

1. **Per-session box state in `TileChromeState`** (`store/store.ts:657-670`), patched via the existing `patchTileChrome`, portalled into `term.overlays` as a third portal alongside `TileOverlays` and `ActivityTileMount` (`TileChrome.tsx:69-83`). Keyed by session id, so the box cannot render on another session. **Not** a global `modals` entry: `anyModalOpen()` means "owns the keyboard app-wide" (`store.ts:1059-1077`) and a per-tile find box must not block the app. Not `term.header` either — it is pinned to 28px and that pin is load-bearing for `fit()` row measurement.
2. **A dedicated `_searchActive` flag guarding four viewport-yank sites.** `findNext` moves the viewport, and four sites would drag it back. They do not all read the same way — `scrollback.ts:303/:344`'s replay-done `finish()` is the trap: clearing `_followBottom` alone **enables** its restore branch, which then overwrites `_followBottom` from geometry at `:347`. `resetFollowIntent()` (`scrollback.ts:200-206`) must also early-return while searching, or any mid-search `ensureAttached()` force-sets `_followBottom = true` and silently revokes search's claim. An earlier #430 draft tried to avoid the flag by clearing `_followBottom`; review round 2 proved that wrong on both counts.
3. **Count renders `n/1000+` past the cap.** The addon hard-caps `resultCount` at 1000 (PoC measured 3000 occurrences reported as 1000). A bare `1000` would be a confidently wrong number.
4. **Live count coalesced to one search per frame.** `SessionTerm.writeData` schedules a re-search on rAF so a flooding session runs at most one search per frame. The pending frame is cancelled on close **and** in `destroy()`, following `session-term.ts:1297`'s precedent — without that, a frame landing after `term.dispose()` calls into a disposed addon.
5. **No flicker, without suppressing real changes.** Operator rule, verbatim: *"avoid flicker if possible (flicker is a visual artifact that is not the same as number changing). count can change if new matches enter or existing matches exit the buffer."* So: the store is patched only when `{count, index}` actually differs; the count field is fixed-width with `tabular-nums` so `9/17 → 10/17` cannot reflow; decorations are not rebuilt when the match set is unchanged. An earlier #430 draft proposed a 150ms alt-screen settle — dropped, because it suppressed genuine count changes to hide a rendering artifact, which is the wrong layer.

**Binding: ⌘F on macOS, Ctrl+Shift+F elsewhere.** Not plain Ctrl+F — xterm converts Ctrl+letter to a C0 control char, so Ctrl+F is `0x06`, readline's `forward-char`, live in bash, zsh and agent input. On macOS the entry point is the **native menu**, not the keydown: `keyboard.ts:899-902` documents that the accelerator intercepts before the webview, so ⌘F arrives as `menu:find-in-session` and that handler is a toggle/refocus. `findKey()` in `keymap.ts` therefore fires only off-mac. **Stated coverage gap:** Playwright drives the webview directly, so it cannot exercise the real mac accelerator; the e2e asserts the off-mac chord and the menu-event handler separately, and the accelerator is a manual check in the built app.

**Single-session view only.** Gated on `view === VIEW_SINGLE`; the box closes on view change, which is not cosmetic — the mode snap force-sets `_followBottom = true` (`view-scroll.ts:85`), so a ⌘G mid-search would revoke search's claim and leave the restore path never firing.

**Keeping typed keys out of the terminal** is the `findBoxActive()` gate in `keyboard.ts`, beside `inlineRenameActive()` (`:155-162`) — not the input's own listener, because `keyboard.ts`'s window listener is capture-phase (`:121`, `:534`) and a capture `stopPropagation()` on the input cannot stop it.

### Source B — alt screen: the daemon searches the transcript

**The daemon searches; the GUI only ever holds a window.** Two facts force this and nothing else: a wire frame is capped at 1 MiB (`internal/wire/frame.go:32-34`) while real transcripts reach 21.9 MB, and the frontend has no virtualization anywhere. Shipping the file is impossible and rendering it would be a first-of-its-kind widget. Searching daemon-side answers both — the GUI receives a capped list of match anchors and, separately, a bounded window of lines around whichever match is active. Nothing large ever crosses the wire and nothing large is ever in the DOM.

The obvious alternative — stream the transcript to the GUI in `FrameData` chunks like `FrameRequestReplay` does, then search it in JS with the same code path as 430 — was rejected on those same two numbers: it moves 21 MB per open and then needs the virtualized list this codebase does not have.

**Projection is deterministic and cached incrementally.** The daemon turns JSONL records into an ordered list of display lines; a line's index in that list is the only addressing scheme, shared by both requests. Re-projecting a 21 MB file on every keystroke is not viable, so the daemon keeps the projection for **one** session — the most recently searched, which is all the feature needs since cross-session search is a non-goal — keyed by path + size + mtime. Transcripts are append-only, so a grown file re-parses only from the last consumed byte offset — **and that offset advances only to the last complete newline.** A transcript can be observed mid-write with a half-flushed final record; consuming those bytes while skipping the unparseable line would drop it permanently, and criterion 6 would fail silently for exactly the most recent line, which is the one the user is most likely looking for. That single design point delivers criterion 6 (lines appended while the view is open become findable) as a property rather than as a feature: every search re-reads the tail.

**Responses are addressed by query, not by arrival order.** Per-keystroke search plus the output-debounced re-issue means two searches can be in flight at once and land out of order, silently painting matches for a query the user has already moved past. `TranscriptMatchesMsg` echoes `Query` and carries `Revision` for exactly this: `events.ts` drops any response whose `query` differs from the store's current query, or whose `revision` is older than the one already applied. This is a correctness requirement, not an optimization.

The same hazard exists for **window** responses and needs a different key: `TranscriptLinesMsg` has no query, and its `Revision` does not change when the user presses Enter twice quickly on an unchanged file — so two window requests differing only in `Start` would both be accepted and could land out of order, painting the wrong window around the active match. Hence `ReqID`: the client keeps a monotonic counter and discards any window response whose `ReqID` is not the latest it issued. Both rules also check `session_id`, since focus can change while the overlay is open.

**The window is centered on the active match and clamped at both ends.** Criterion 3 asks for the active match centered and readable, so the window request is `Start = activeLine - Count/2`, clamped to `[0, max(0, TotalLines-Count)]`. Without the clamp a match on line 2 requests a negative `Start`.

**Re-search is driven by the data the GUI already receives.** Criterion 6 needs no polling and no new push channel: the GUI is already streaming `FrameData` for the focused session, so a debounced re-issue of the current query on session output covers it for free.

**The overlay is a standard `hv-dialog`, not a new per-session widget.** Dialog roots are static siblings of `#terms` (`index.html:126-159`) stacked by CSS, so they never occupy terminal layout — which matters because `session-term.ts:300`'s `ResizeObserver` calls `fit.fit()` and can trigger scrollback replay on any box change. Using the existing pattern satisfies criteria 4 and 8 by construction rather than by careful avoidance. `ModalShell` brings Escape, backdrop and Tab containment; `app/modals/help-overlay.ts:32-40`'s close dance (`releaseFocus` → `flushSync(closeModal)` → `focusActiveTerm`) is copied exactly, because each of its three steps fixes a specific focus bug already found once.

**Availability is a daemon answer, not a frontend guess.** The search response carries `available` plus a `reason`; Claude and pi resolve to a path, everything else returns unavailable and the overlay renders the explicit "no searchable history" state (criterion 7). The frontend never enumerates agent types.

### Decisions taken at Phase 3Q

- **One PR, not phased.** Operator's call; criterion 7 is met on first merge.
- **Projection includes** user messages, assistant text, and tool results. It **excludes** thinking blocks and `tool_use` input JSON — closest to "what I saw on screen", which is what the user is re-finding.
- **Empty query shows the tail** of the transcript, so the overlay opens as history rather than as a blank box.
- **pi resolution is an exact glob** on Hive's own session id (see Research) — the three heuristic options offered were all unnecessary.

### Files to change

1. `internal/wire/frame.go` — add `FrameSearchTranscript` / `FrameTranscriptMatches` / `FrameGetTranscriptLines` / `FrameTranscriptLines` consts, appended monotonically, plus `String()` cases. No `PROTOCOL_VERSION` bump.
2. `internal/wire/control.go` — the payload structs, `json:"snake_case"` tags throughout, **with every member named and every cap a declared const**:

    ```go
    // Clamps. The daemon enforces these; a client's request is a
    // suggestion. These are RAW-byte caps and are a fast path only --
    // they do NOT by themselves bound the encoded frame (see below).
    // The guarantee is the marshalled-size check.
    const (
        MaxTranscriptMatches  = 500   // per search response
        MaxTranscriptPreview  = 300   // bytes of context per match
        MaxTranscriptWindow   = 200   // lines per window response
        MaxTranscriptLineText = 2000  // bytes per line, head-truncated
    )

    type TranscriptMatch struct {
        Line    int    `json:"line"`     // index into the projection
        Col     int    `json:"col"`      // byte offset within the line
        Len     int    `json:"len"`      // match length
        Role    string `json:"role"`
        Preview string `json:"preview"`  // <= MaxTranscriptPreview
    }

    type TranscriptLine struct {
        Line      int    `json:"line"`
        Role      string `json:"role"`
        Text      string `json:"text"`       // <= MaxTranscriptLineText, rune-safe
        Truncated bool   `json:"truncated"`  // text was cut
    }
    ```

    **`Col` and `Text` must share one coordinate system.** `TranscriptMatch.Col` is an offset into the *projected* line, but `TranscriptLine.Text` is truncated — so on any line over the cap the two disagree and `highlightSegments` highlights the wrong span or reads past the end. The rule: **truncation is always tail-truncation anchored at byte 0**, and a match whose `Col` falls beyond the emitted `Text` is re-based by emitting a window of the line centered on the match instead, with `Col` adjusted to index the emitted `Text`. `Col` indexes `Text`, always, with no caller-side arithmetic.

    **Truncation is rune-safe.** `MaxTranscriptLineText` is a byte cap over arbitrary UTF-8 and a naive slice splits a rune. `capBytes` (`internal/daemon/daemon.go:861`) already solves exactly this and is pinned by `TestCapBytesRuneSafe` (`activity_test.go:487`); reuse it rather than re-deriving it.

    ```go
    ```

    `SearchTranscriptReq{SessionID, Query, MaxMatches}` — `MaxMatches` clamped to `MaxTranscriptMatches`.
    `TranscriptMatchesMsg{SessionID, Query, Available, Reason, Total, Truncated, TotalLines, Revision, Matches}` — `Query` is echoed so the client can discard stale responses.
    `GetTranscriptLinesReq{SessionID, ReqID, Start, Count}` — `Count` clamped to `MaxTranscriptWindow`, `Start` clamped to `[0, TotalLines-1]`. `ReqID` is a client-side monotonic counter echoed on the response; see the stale-response rule.
    `TranscriptLinesMsg{SessionID, ReqID, Start, TotalLines, Revision, Lines}`.

    **Why this is load-bearing:** a single Claude `tool_result` line routinely exceeds 1 MiB on its own. Without per-line truncation `WriteFrame` returns `ErrFrameTooLarge` (`internal/wire/frame.go:32-34,274-289`), nothing reaches the wire, and the overlay waits forever on a response that was never sent.

    **The byte caps alone are not sufficient, and this is the subtle part.** They bound the text *before* JSON encoding. Transcript text is terminal output, so it is full of ANSI escapes, and `json.Marshal` expands every control byte to a six-byte `\u001b`. At 6x worst case the declared products — 500x300 = 200 KB of matches, 200x2000 = 400 KB of lines — encode to 1.2 MB and 2.4 MB, both **over** `MaxPayload`. Two measures together, not either alone:

    1. **Projection strips control characters** (keeping `\t`), replacing runs with a single space. Wanted independently: raw escapes must not reach the DOM, and a match column counted over escape bytes would highlight the wrong place. This removes the 6x case.
    2. **The cap is enforced on the marshaled bytes, not the field lengths.** The daemon marshals, and while `len(payload) > MaxPayload - headroom` it drops the tail of `Matches` (or halves `Count`) and re-marshals. This is a loop with a strictly decreasing bound that terminates at an empty list, so it cannot fail to produce a sendable frame — the field caps become a fast path, not the guarantee.
3. `internal/wire/client.go` — add both response frames to the `controlEvents` map.
4. `internal/agent/claude.go` — export a `claudeTranscriptPath` resolver (the path half of the existing `claudeSessionExists`, which currently inlines the join).
5. `internal/agent/pi.go` — new `encodePiSessionsDir(cwd)` (preserves dots, `--`-wrapped) and `piTranscriptPaths(home, cwd, sessionID)` doing the `*_<sessionID>.jsonl` glob, sorted by filename.
6. `internal/agent/agent.go` — a `TranscriptPaths` field or accessor on `Def` so the daemon asks the catalog rather than switching on agent id; nil for every agent but claude and pi. It takes the **`Entry.AgentSessionID`**, never "Hive's session id" — the two diverge in two real cases:

    - `internal/registry/create.go:585-591` is an `else if`: a session created with `ContinueConversation` and a `ResumeCmd` never gets `SessionIDFlag` injected, yet `:869-872` still stamps `e.AgentSessionID = p.id` whenever `spec.Cmd` is empty and the agent declares a `SessionIDFlag`. The agent therefore recorded its conversation under an id Hive did not give it.
    - A caller-supplied `spec.Cmd` leaves `AgentSessionID` empty entirely (`:869`, and the comment at `:862-868` says so).

    Both produce a derived path that does not exist. The resolver must return a **distinct** "resolved but file missing" reason, separate from "this agent has no transcripts", so the overlay does not tell the user a real Claude session has no searchable history.
7. `internal/daemon/daemon.go` — two handler arms mirroring `FrameGetActivity` (`:1280-1293`): `decodeReq` → lookup → clamp → `ops.writeJSON`. **Both frames stay off the `sessionModeFrames` allowlist** — control-mode only, exactly as GET_ACTIVITY is (`daemon.go:1280-1283`, pinned by `activity_test.go:226-256`). A `ModeSession` connection is an agent running *inside* a session; letting it read its own transcript is a separate decision with its own privacy question, and adding a frame to the allowlist later is additive.
8. `internal/buildinfo/contract.go` — bump `DaemonContract` 14 → 15 with a dated history entry. Required: a GUI built here cannot drive a daemon without these frames.
9. `cmd/hivegui/app_calls.go` — `SearchTranscript` and `GetTranscriptLines` Wails methods.
10. `cmd/hived-ws-bridge/main.go` — the two matching JSON-RPC verbs.
11. `internal/wire/testclient/client.go` — the two test-client wrappers.
12. `cmd/hivegui/frontend/src/app/session-term.ts` — the `_searchActive` flag and `_followBeforeSearch`; early-outs at the `onScroll` re-pin (`:779-791`), `_onBodyResize` (`:1057`, `:1072`) and `_replayWantsBottom` (`:1129`); the rAF-coalesced re-search in `writeData` (`:1274-1292`) and its cancellation in `destroy()`; the `onBufferChange` subscription that recreates the addon **and re-selects the source**.
13. `cmd/hivegui/frontend/src/lib/scrollback.ts` — early-out in `finish()`'s restore branch (`:303`, `:344-347`) and in `resetFollowIntent()` (`:200-206`) while `_searchActive`.
14. `cmd/hivegui/frontend/src/components/TileChrome.tsx` — the third portal for the find box, alongside `TileOverlays` and `ActivityTileMount` (`:69-83`).
15. `cmd/hivegui/frontend/package.json` / `package-lock.json` — add `@xterm/addon-search@0.16.0` properly. It currently sits in `node_modules` from #430's PoC via `--no-save`, so `npm ci` would drop it.
16. `cmd/hivegui/frontend/src/app/events.ts` — subscribe to both response events.
17. `cmd/hivegui/frontend/src/store/store.ts` — transcript-search slice: query, matches, active index, window lines, availability.
18. `cmd/hivegui/frontend/index.html` — the `hv-dialog` root for the overlay.
19. `cmd/hivegui/frontend/src/app/keyboard.ts` — handle the chord in the ladder.
20. `cmd/hivegui/frontend/src/lib/keymap.ts` — `transcriptSearchKey()` predicate via `cmdOrCtrl`.
21. `cmd/hivegui/frontend/src/lib/shortcuts.ts` — entries in **both** `shortcutGroups()` and `paletteShortcuts()`.
22. `cmd/hivegui/frontend/src/main.tsx` — palette command entry.
23. `cmd/hivegui/menu_darwin.go` — the ⌘⇧F menu item under View, routed through a single toggle entry point (the native accelerator intercepts before the webview).
24. `README.md` — Keybinds table row.
25. `cmd/hivegui/frontend/test/e2e/wails-mock.ts` — stubs for both new Wails methods, mirroring `GetActivity` at `:1026-1027`. Without them the e2e spec cannot drive the overlay at all.
26. `cmd/hivegui/frontend/wailsjs/go/main/App.d.ts` / `App.js` — regenerated by the Wails build, not hand-edited, but named here because `npm run typecheck` fails without them and a fresh worktree needs `./scripts/ci-bootstrap.sh` first.
27. `docs/design-docs/session-history-search.md:69` — still advertises the pi path mapping as an open question. Update it in the same PR as the spec, not one or the other.
28. `docs/product-specs/431-search-an-agent-session-s-transcript-history.md` — replace the now-resolved "pi transcript path mapping" open question in `## Notes` with the exact-glob finding, so the spec does not keep advertising an unknown that research closed.

### New files

- `internal/transcript/transcript.go` — the package. Reads a resolved path, projects records to `[]Line{Index, Role, Text}`, and searches. Claude and pi projectors live here; both handle `.message.content` being a string **or** a block array, and `tool_result.content` being a string or array one level deeper.
- `internal/transcript/cache.go` — the single-session incremental cache: keyed on path+size+mtime, re-parses only from the last byte offset on growth, drops on session change. **`size < cachedOffset` forces a full re-parse** rather than seeking past EOF — append-only JSONL makes a shrink or rewrite unlikely, not impossible.
- `internal/transcript/transcript_test.go`, `internal/transcript/cache_test.go`.
- `internal/daemon/transcript_test.go`.
- `cmd/hivegui/frontend/src/lib/transcript-search.ts` — pure: split a line into highlighted segments given a match column and length; format `n/total`.
- `cmd/hivegui/frontend/src/app/modals/transcript-search.ts` — open/close/toggle orchestration, mirroring `help-overlay.ts`.
- `cmd/hivegui/frontend/src/components/modals/TranscriptSearchOverlay.tsx` — `ModalShell`-based, keyed on the modal entry's `seq` so each open resets query and selection.
- `cmd/hivegui/frontend/src/theme/components/transcript-search.css` — tokens only, `hv-*` class names.
- `cmd/hivegui/frontend/test/unit/transcript-search.test.ts`
- `cmd/hivegui/frontend/test/dom/transcript-search-overlay.test.tsx`
- `cmd/hivegui/frontend/test/e2e/transcript-search.spec.ts`
- `.changesets/transcript-history-search.md` — `type: added`, `bump: minor`, `issue: 431`.

### Tests

**Go**

- `internal/agent/pi_test.go` — `TestEncodePiSessionsDir`: table-driven, mirroring `TestEncodeClaudeProjectDir` (`claude_test.go:9-53`), with an explicit case pinning that `.worktrees` keeps its dot (the exact way reusing Claude's encoder would break).
- `internal/agent/pi_test.go` — `TestPiTranscriptPathsMatchesSessionIDSuffix`: temp dir with three files, two for other session ids, asserts only the `*_<id>.jsonl` one is returned; plus a non-uuid session id case (the `hive-probe-` shape).
- `internal/agent/pi_test.go` — `TestPiTranscriptPathsSortsByTimestamp`: two files for one id, asserts filename order.
- `internal/agent/claude_test.go` — `TestClaudeTranscriptPath`.
- Split across layers so neither test crosses the boundary `DESIGN.md` pins — `internal/registry/create_test.go` asserts `AgentSessionID` is stamped without flag injection; `internal/agent` asserts the distinct missing-file reason. `TestContinueConversationLeavesNoResolvableTranscript`: a session created with `ContinueConversation` + a `ResumeCmd` has `AgentSessionID` stamped (`create.go:869-872`) but never had `--session-id` injected (`:585-591`), so the resolver must report "resolved but file missing", **not** "no searchable history". This is the criterion-7 false-negative the plan would otherwise ship.
- `internal/transcript/transcript_test.go` — `TestProjectClaudeStringContent`, `TestProjectClaudeBlockArrayContent`, `TestProjectClaudeNestedToolResultContent` (the string-or-array-inside-tool_result case), `TestProjectSkipsThinkingAndToolUse`, `TestProjectSkipsNonMessageRecordTypes`, `TestProjectTolueratesMalformedLine` (one bad JSON line must not abort the file).
- `internal/transcript/transcript_test.go` — `TestSearchIsCaseInsensitiveSubstring` (asserts line index **and** column, since the column drives highlighting), `TestSearchTruncatesAtMaxMatches` (asserts `Truncated` is set).
- `internal/transcript/cache_test.go` — `TestCacheReusesProjectionWhenFileUnchanged` (asserts the file is not re-read), `TestCacheReparsesOnlyAppendedTail` (append whole lines, assert new matches found and the prefix not re-parsed — **criterion 6's golden path**), `TestCacheHoldsBackPartialFinalLine` (write half a record with no trailing newline, search and assert it is absent *and that the offset did not advance*; then complete the line and assert it becomes findable — **this fails on the naive implementation and is the reason the boundary rule exists**), `TestCacheDropsOnSessionChange`.
- `internal/wire/wire_test.go` — `TestTranscriptFramesRoundTrip` (typed JSON pairs, mirroring `TestHelloWelcomeRoundTrip`, `wire_test.go:72+`).
- `internal/transcript/transcript_test.go` — `TestSearchClampsToMaxMatches` and `TestWindowClampsCountAndStart`: call the daemon-side entry points with **over-cap input** (`MaxMatches: 100000`, `Count: 100000`, `Start: -50`) and assert the returned response respects `MaxTranscriptMatches` / `MaxTranscriptWindow` / a non-negative `Start`. These fail when a clamp is *missing*, which a test built from the constants would not.
- `internal/transcript/transcript_test.go` — `TestLineTextTruncatedToCap`: project a record whose `tool_result` text is 2 MB, assert the emitted `TranscriptLine.Text` is at most `MaxTranscriptLineText` with `Truncated: true`, and — with multi-byte runes straddling the cap — that the result is **valid UTF-8** (`utf8.ValidString`), mirroring `TestCapBytesRuneSafe` (`activity_test.go:487`).
- `internal/transcript/transcript_test.go` — `TestMatchColIndexesEmittedText`: a match beyond `MaxTranscriptLineText` on a long line returns a re-based `Col` that indexes the emitted `Text`, so the highlight lands on the match. Without this the coordinate systems diverge silently and only on long lines.
- `internal/wire/wire_test.go` — `TestTranscriptResponseUnderMaxPayloadAtWorstCase`: build a response from **over-cap input run through the clamps**, `WriteFrame` it, and assert no `ErrFrameTooLarge`. The fixture text must be **escape-heavy — raw ESC sequences and multi-byte runes, never ASCII filler** — or the test passes vacuously while proving nothing about the real worst case. Because the input is over-cap, this also fails if any clamp is absent, not merely if a constant was edited.
- `internal/transcript/transcript_test.go` — `TestProjectStripsControlCharacters`: a record whose text carries ANSI escapes emits text with none, tabs preserved, and **match columns counted over the stripped text** (a column counted over escape bytes highlights the wrong characters).
- `internal/transcript/transcript_test.go` — `TestResponseFitsMaxPayloadWithAllEscapes`: build max-cap responses whose every character is a control byte (the 6x JSON-expansion worst case), marshal, and assert the result is under `wire.MaxPayload`. **This is the test that fails if the byte caps are trusted without the marshaled-size check** — the arithmetic 500x300 and 200x2000 is safe pre-encoding and unsafe post-encoding.
- `internal/daemon/transcript_test.go` — `TestSearchTranscriptReturnsMatches` (real test daemon + control handshake, mirroring `TestGetActivityReturnsRing` at `activity_test.go:108-146`), `TestGetTranscriptLinesWindowIsCenteredAndClamped` (a match near line 0 and one near the last line both return an in-range `Start`, **criterion 3's centering half**), `TestSearchTranscriptUnavailableForShellSession` (asserts `Available:false` with a reason, **criterion 7**), `TestSearchTranscriptReportsMissingFileDistinctly` (the resolved-but-absent reason differs from the no-transcripts reason), `TestSessionModeCannotSearchTranscript` (mirrors `activity_test.go:226-256`).

**Frontend — source selection and the normal-buffer half (criteria 5, 7, 11)**

- `test/dom/find-box-source.test.tsx` — `TestFindBoxPicksBufferSourceOnNormalBuffer` / `...PicksTranscriptOnAltScreen`: with `term.buffer.active.type` stubbed, opening the box selects the right source and the user is offered no choice (**criterion 7**).
- `test/dom/find-box-source.test.tsx` — `TestFindBoxSwitchesSourceOnBufferChange`: fire `onBufferChange` with the box open, assert the current query is re-run against the new source and the count updates.
- `test/dom/find-box-source.test.tsx` — `TestAddonNeverSearchedWhileAltBufferActive`: spy on the addon's `findNext`/`findPrevious` and assert **zero** calls while the buffer type is `alternate`. This is **criterion 11's** real test — it pins the invariant that makes the poisoning path unreachable, rather than trying to observe the poisoning itself (which only manifests after the app exits and is therefore untestable in-process).
- `test/e2e/find-in-session.spec.ts` — the off-mac chord opens the box on a normal-buffer session, a string in off-screen scrollback is found and the viewport moves to it (**criterion 5**), then Esc restores the prior scroll position and `assertAlignedFocus` passes (**criterion 4**).
- `test/unit/keymap.test.ts` — `findKey()` fires only off-mac, and never on plain Ctrl+F (which is `0x06`).

**Frontend — shared box behaviour**

- `test/unit/transcript-search.test.ts` — `highlightSegments` splits a line at the match with correct offsets, handles a match at index 0, at end of line, multiple matches on one line, and a match inside a truncated line (where `Col` was re-based); `formatCount` renders `n/total` and the zero case.
- `test/dom/transcript-search-overlay.test.tsx` — jsdom + RTL, mirroring `help-overlay.test.tsx`: opening toggles `.hidden` off and focuses the input **without a click** (criterion 1); typing re-issues the search (spy call count, criterion 2); Enter / Shift+Enter move the active index and wrap (criterion 3); Escape and the close control both call `releaseFocus` then `focusActiveTerm` in that order (criterion 4); an unavailable response renders the "no searchable history" state rather than an empty list (criterion 7); **a response whose `query` does not match the current input is discarded** rather than painted (dispatch two searches, resolve them out of order, assert the newer query's matches survive); the window request's `start` is centered on the active index and never negative; **out-of-order window responses are discarded by `ReqID`** (issue two window requests, resolve them in reverse, assert the latest `start` survives); and **a session-output event with the overlay open re-issues the current query after the debounce** — this is criterion 6's GUI half, which the Go cache tests cannot reach.
- `test/e2e/transcript-search.spec.ts` — Playwright, mirroring `keymap-activity.spec.ts`: the chord opens the overlay and Escape closes it, on both the mac and spoofed-Linux platform paths via `bootAsLinux`; after closing, re-run `assertAlignedFocus` from `focus-invariants.spec.ts` to prove the terminal got keyboard focus back with its `.term-focused` tile intact (criterion 4), and assert the terminal's scroll position is unchanged across open/close while output is streaming (criterion 8).

**Criterion 5** — "a string that scrolled off the alt screen is found" — is satisfied **structurally**, by sourcing from disk rather than from the terminal buffer: there is no code path by which the alt screen's contents could limit what this search sees. `internal/daemon/transcript_test.go` searches a fixture string to prove the path works end to end; it does not and cannot assert anything about a terminal buffer, since the daemon test has none.

### Verification

```bash
scripts/test.sh go                 # transcript pkg, agent paths, wire caps, daemon handlers
scripts/test.sh unit dom e2e       # highlight module, overlay DOM, chord + focus restore
scripts/ui-lint.sh --strict        # tokens + icons in the new CSS
scripts/check-daemon-contract.sh origin/main HEAD   # takes two refs; bare call exits 2
scripts/check-changeset.sh
```

Manual row, runnable without eyes per `docs/verifying-the-gui-by-hand.md`: `wails dev`, then a throwaway Playwright script against `http://localhost:34115` that opens the overlay on a real Claude session, searches a string known to be in the transcript but scrolled off, and reads the match count from the DOM.

### PR convergence ledger

Append-only, one line per /hs-review-loop iteration.

## Open questions / risks

- **First-search latency on a 21.9 MB transcript.** The cache makes every search after the first cheap, but the first one parses the whole file. Mitigation: parse on overlay open rather than on first keystroke, so the cost lands during the open animation. **The overlay renders an explicit pending state until the first response arrives — never a frozen empty box**, which is the failure mode that would otherwise only be discovered by using the GUI. If the parse is still slow once measured, the fallback is a byte-offset index built on a streaming first pass. Not designed now — measure first.
- **Daemon memory.** One cached projection can hold ~15 MB of text. Bounded by holding exactly one session's, and by dropping it when the overlay closes. Worth a hard cap with truncation-from-the-head if a transcript ever exceeds it.
- **`sessionModeFrames` gate.** Defaulting to control-mode only. If the `hive` CLI inside a session should be able to search its own transcript later, that is an additive change, not a rework.
- **Codex is deliberately excluded** though it has the most mature path-capture code in the repo (`codex.go:64-152`) and would be the cheapest third agent. Spec non-goal; noted as the obvious next one.
- **A session can change buffer type while the box is open** — an agent starts, or `vim` opens on a shell. The source is selected at open time and re-evaluated on `term.buffer.onBufferChange`: the box stays open, re-runs the current query against the new source, and updates the count. Tested by `TestFindBoxSwitchesSourceOnBufferChange` in the dom layer.
- **The noise tension from the spec stands.** Criterion 3 is implemented as navigation precision, not fewer hits.


## Second opinion

Two reviewer rounds, both `revise`, both at confidence 8. **All thirteen must-fix items were applied** (7 in round 1, 6 in round 2); the plan below is the revised version. Round 2 also confirmed every cited `file:line` in the Research section is real, and that neither document contained text attempting to direct the reviewer's actions.

**Round 1** found the wire contract had no enforced caps at all (`TranscriptMatch`'s members were never defined, `MaxMatches` was client-supplied, a single `tool_result` line can exceed 1 MiB on its own), no stale-response discard despite two searches being in flight at once, a tail-parse boundary that would silently eat a half-flushed final record, and — the sharpest one — that resolving from "Hive's session id" rather than `Entry.AgentSessionID` breaks criterion 7 for real sessions, because `create.go:585-591` skips `--session-id` injection on `ContinueConversation` while `:869-872` stamps `AgentSessionID` anyway. It also caught a wrong verification invocation (`check-daemon-contract.sh` needs two refs) and two missing blast-radius files.

**Round 2** confirmed round 1's fixes landed, then found three holes the fixes themselves introduced, all in the new clamp block: per-line truncation put `TranscriptMatch.Col` and `TranscriptLine.Text` in **different coordinate systems** (so highlighting reads the wrong span on any long line), the byte cap was not rune-safe when `capBytes` already exists for exactly that, and the stale-response rule keyed on `Query`/`Revision` does not discriminate **window** responses — two window requests differing only in `Start` could land out of order. It also noted criterion 6's GUI half had no test.

**One hole was found by the drafter, not the reviewers**, between the two rounds: the declared caps bounded *raw* bytes while `MaxPayload` is checked on the *marshalled* frame, and `json.Marshal` expands each control byte to a six-byte escape. Transcript text is terminal output, so at 6x the declared 500x300 and 200x2000 products encode to 1.2 MB and 2.4 MB — both over the cap the clamps existed to respect. Fixed by stripping control characters during projection (wanted independently, so raw escapes never reach the DOM) and by enforcing the limit on marshalled size with a shrink-and-remarshal loop that provably terminates. Round 2 independently flagged the same arithmetic, which is some evidence the fix was aimed at a real defect rather than an imagined one.

No third round was run: the pipeline allows one revise-and-recheck cycle, and looping further trades the operator's time for diminishing returns.

## Decision log

- **2026-09-17** — Scope: one PR, not phased. Why: operator's call, re-confirmed after the #430 merge roughly doubled the diff.
- **2026-09-17** — Projection excludes thinking blocks and `tool_use` input JSON. Why: closest to "what I saw on screen", which is what the user is re-finding.
- **2026-09-17** — Empty query shows the transcript tail. Why: the overlay should open as history, not as a blank box.
- **2026-09-17** — pi transcript resolution is an exact glob on Hive's own session id, not a recency heuristic. Why: `create.go:587-590` passes Hive's id as `--session-id` and pi echoes it verbatim into the filename; verified across 91 real transcripts, all ids unique.
- **2026-09-17** — **⌘F drives both sources, selected by `term.buffer.active.type`.** Why: operator decision — "cmd-f is text search for regular text sessions and transcript search for alt-screen agents". Alt-screen is *why* buffer search fails, so it is the right discriminator. Supersedes criterion 1's original "not ⌘F" and the coexist non-goal.
- **2026-09-17** — #430 merged into #431 and closed; its reviewed plan content folded in rather than re-derived.
- **2026-09-17** — Caps are enforced on **marshalled** size, not raw byte length, and projection strips control characters. Why: `json.Marshal` expands control bytes 6x, so the raw-byte caps did not actually bound the frame.
- **2026-09-17** — Both frames stay off the `sessionModeFrames` allowlist (control-mode only). Why: matches GET_ACTIVITY; letting an in-session agent read its own transcript is a separate decision with its own privacy question.

- **2026-09-17** — Buffer find reported 0/0: addon decorations are a proposed xterm API and every findNext threw without `allowProposedApi`; the throw was swallowed. Enabled the option and stopped swallowing silently. Why: found by operator in an isolated run, reproduced in a real browser.
- **2026-09-17** — Find bar pinned top-right in both modes. Why: operator — "search box should be in the same place for both modes".
- **2026-09-17** — Highlights from the theme accent; ordinary matches outline-only, active match a solid fill via the selection colour while the box is open. Why: operator asked for a much brighter highlight, and screenshots showed the active match was indistinguishable — the ordinary fill and the selection both painted over it.
- **2026-09-17** — Find input disables autocorrect/autocapitalize/autocomplete/spellcheck. Why: operator — macOS was autocorrecting queries.
- **2026-09-17** — **Search runs bottom to top in both modes; spec criteria 3, 3a and 8 amended.** Why: operator — "most recent output first, in both modes". Consequences taken with it: the daemon searches newest-first so truncation keeps the newest matches; a live refresh re-anchors on the match being read (by line and column) instead of jumping to the newest; the arrow icons follow screen direction (up = older = Enter).
- **2026-09-17** — Live refresh was never wired in production (`onSessionOutput` had no caller; buffer mode never forwarded the addon's own re-search). Wired both, plus a second trailing refresh 1.5s after output settles, because the agent writes its transcript record when a message finishes. Why: operator asked for live transcript updates; checking for it found the gap.

## Progress

- **2026-09-17** — Plan created; research started.
- **2026-09-17** — Research complete; pi path mapping resolved as an exact glob. Stage → PLAN.
- **2026-09-17** — Second opinion round 1: `revise` (confidence 8, 7 must-fix). All 7 applied.
- **2026-09-17** — Second opinion round 2: `revise` (confidence 8, 6 must-fix). All 6 applied. Drafter found an eighth independently (JSON-encoding cap hole).
- **2026-09-17** — Operator feedback at the plan stop merged #430 into this feature behind one ⌘F binding. Spec rewritten, plan re-drafted, re-rendered.
- **2026-09-17** — **Plan approved** (html, 2 rounds). Stage → IMPLEMENT.
- **2026-09-17** — Implemented. Full suite green: 574 unit, 857 dom, 371 e2e, whole Go suite. PR [#432](https://github.com/lucascaro/hive/pull/432) opened. Stage → REVIEW.

## Open questions

- Resolved at research: pi transcript path mapping is an exact glob on Hive's session id. See Research.
