// ---------- session terminal ----------
//
// Moved verbatim from main.js: the SessionTerm class plus the font
// helpers and ensureTerm factory that manage its instances.

import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { WebglAddon } from '@xterm/addon-webgl';
import { WebLinksAddon } from '@xterm/addon-web-links';

import {
  OpenSession, CloseAttach, WriteStdin, ResizeSession,
  RequestScrollbackReplay, KillSession, SetClipboardText,
  ClipboardGetText, OpenURL,
} from '../bridge.js';
import { state } from './state.js';
import { flashStatus, reportFailure } from './dom.js';
import { isMac } from '../lib/platform.js';
import { isShiftEnter, NEWLINE_SEQ } from '../lib/keymap.js';
import { DEFAULT_FONT_SIZE, clampFont } from '../lib/font.js';
import {
  shouldRefreshOnVisibility, recoverFromContextLoss, bindDprWatcher,
} from '../lib/renderer-recovery.js';
import {
  shouldRequestReplay, decideResizeReplay, REPLAY_DEBOUNCE_MS, applyRebaseline,
} from '../lib/scrollback.js';
import { scrollTrace, snapshotScrollJump } from './trace.js';
import { classifyViewportMove } from '../lib/scroll-debug.js';
import { wheelToScrollLines, shouldScrollViewport } from '../lib/wheel-scroll.js';
import { onSessionBell, clearAttention } from './events.js';
import { minimizeSession, updateAppTitle, showSingle, renderGrid } from './view.js';
import { updateSidebarSelection } from './sidebar.js';
import { setActive, setFocusedTile, refocusActiveTerm } from './focus.js';


// Monotonic millisecond clock for the scroll-jump detector. Falls back
// to 0 where performance isn't available (never in a real renderer).
function nowMs() {
  try { return performance.now(); } catch { return 0; }
}

// A viewport within this many lines of the bottom counts as "at the
// bottom". Tolerates TUIs (codex etc.) that park a line or two short.
const STICKY_BOTTOM_LINES = 2;

// How recently a user scroll gesture must have fired for an onScroll to
// count as user-driven (vs parse-driven cap-trim drift).
const USER_SCROLL_GRACE_MS = 250;

export class SessionTerm {
  constructor(info) {
    this.info = info;
    // Per-session UTF-8 decoder. Streaming mode buffers partial multi-byte
    // sequences at chunk boundaries; sharing one decoder across sessions
    // contaminates session B's bytes with session A's pending tail bytes,
    // producing garbled glyphs — most visible with multi-byte-heavy output
    // (emojis, box-drawing, Powerline glyphs from Claude, etc.).
    this.decoder = new TextDecoder('utf-8', { fatal: false });
    this.host = document.createElement('div');
    this.host.className = 'term-host';
    this.host.dataset.sid = info.id;
    this.host.style.setProperty('--session-color', info.color || '#888');

    // Tile header (only visible in grid mode via CSS).
    this.header = document.createElement('div');
    this.header.className = 'tile-header';
    this.header.setAttribute('aria-label', `Session ${info.name}`);
    this.tileColor = document.createElement('span');
    this.tileColor.className = 'tile-color';
    this.tileName = document.createElement('span');
    this.tileName.className = 'tile-name';
    this.tileName.textContent = info.name;
    this.tileWorktree = document.createElement('span');
    this.tileWorktree.className = 'worktree-glyph';
    this.tileWorktree.textContent = '⎇';
    {
      const wtBranch = info.worktreeBranch ?? info.worktree_branch;
      if (wtBranch) {
        this.tileWorktree.title = `Worktree: ${wtBranch}`;
      } else {
        this.tileWorktree.style.display = 'none';
      }
    }
    this.tileProject = document.createElement('span');
    this.tileProject.className = 'tile-project';
    // OSC-set window title from the running TUI (vim, htop, claude…).
    // Sits between the session name and the project label so the
    // user can tell at a glance what each tile is currently doing.
    this.tileTermTitle = document.createElement('span');
    this.tileTermTitle.className = 'tile-term-title';
    this.tileMinimize = document.createElement('button');
    this.tileMinimize.className = 'tile-minimize';
    this.tileMinimize.type = 'button';
    this.tileMinimize.title = 'Minimize (hide from grid)';
    this.tileMinimize.setAttribute('aria-label', 'Minimize session');
    this.tileMinimize.textContent = '–';
    this.tileMinimize.addEventListener('mousedown', (e) => {
      // Block the surrounding tile mousedown so minimizing doesn't
      // also select / switch to this tile.
      e.stopPropagation();
    });
    this.tileMinimize.addEventListener('click', (e) => {
      e.stopPropagation();
      minimizeSession(this.info.id);
    });
    this.header.append(this.tileColor, this.tileName, this.tileWorktree, this.tileTermTitle, this.tileProject, this.tileMinimize);

    this.body = document.createElement('div');
    this.body.className = 'term-body';

    this.host.append(this.header, this.body);
    document.getElementById('terms').appendChild(this.host);

    this.term = new Terminal({
      fontFamily: 'Menlo, "DejaVu Sans Mono", monospace',
      fontSize: state.fontSize,
      // cursorBlink causes a repaint twice a second per terminal —
      // material on older machines with many tiles. Off by default.
      cursorBlink: false,
      scrollback: 5000,
      smoothScrollDuration: 0,
      theme: { background: '#000000' },
      // Route OSC 8 hyperlinks (used by Claude CLI and others) through
      // the OS default browser via the Wails backend.
      linkHandler: {
        activate: (_e, uri) => { if (uri) OpenURL(uri).catch(reportFailure('open link')); },
      },
    });
    this.fit = new FitAddon();
    this.term.loadAddon(this.fit);
    this.term.open(this.body);

    // Single source of truth for "tile geometry changed". Fires post-
    // layout, only when the body's box actually resizes — covers window
    // resize, sidebar drag, single↔grid flip, show/hide, and grid row-
    // span changes without any push-based refit calls. Font-size changes
    // don't change the body size and call _onBodyResize() explicitly.
    this._pendingAttach = false;
    this._revealRaf = 0;
    this.ro = new ResizeObserver(() => this._onBodyResize());
    this.ro.observe(this.body);

    // WebGL renderer is dramatically faster than the default DOM
    // renderer on older machines (VS Code uses the same approach).
    // Load it lazily after open() and silently fall back to DOM if
    // the GPU / driver doesn't support it. On context loss (e.g. the
    // browser caps simultaneous WebGL contexts and kills ours when too
    // many tiles exist), dispose the dead addon and try to re-attach;
    // if re-attach fails, fall back to DOM and force a full repaint so
    // we don't leave stale glyphs frozen on the canvas.
    this._attachWebgl();

    // Recover the renderer from the silent triggers that leave a stale
    // backbuffer until the next resize: device-pixel-ratio changes
    // (window dragged between displays with different scale, OS zoom)
    // and visibility transitions (occlusion, GPU sleep). Both are cheap:
    // clearTextureAtlas() rebuilds the glyph cache; term.refresh()
    // forces a full repaint so the stale pixels are overwritten.
    this._installRendererRecoveryListeners();

    // Detect URLs in terminal output and route activation through
    // the OS default browser. Hover underlines the URL; click (or
    // ⌘-click when mouse reporting is active) follows it.
    try {
      this.term.loadAddon(new WebLinksAddon((event, uri) => {
        if (uri) OpenURL(uri).catch(reportFailure('open link'));
      }));
    } catch (err) {
      // Non-fatal; sessions still work without clickable links.
    }

    // When the running program enables mouse reporting (e.g. Claude,
    // vim), xterm sends mousedown/mouseup to the PTY and cancels the
    // event before the Linkifier can process it. Work around this by
    // intercepting clicks on the xterm screen: if a recognized link
    // is under the cursor, suppress the event so it doesn't reach the
    // mouse protocol handler, letting the Linkifier's own handlers
    // process it and call activate.
    const screen = this.body.querySelector('.xterm-screen');
    if (screen) {
      screen.addEventListener('mousedown', (e) => {
        const link = this.term._core?.linkifier?.currentLink;
        if (link && link.link) {
          this._pendingLink = link.link;
          this._pendingLinkX = e.clientX;
          this._pendingLinkY = e.clientY;
          // Stop all other handlers — both the terminal's mouse-
          // protocol handler and the Linkifier. We call activate
          // manually on mouseup.
          e.stopImmediatePropagation();
        } else {
          this._pendingLink = null;
        }
      }, { capture: true });
      screen.addEventListener('mouseup', (e) => {
        if (!this._pendingLink) return;
        // Only treat as a click if the mouse barely moved (not a drag).
        const dx = e.clientX - this._pendingLinkX;
        const dy = e.clientY - this._pendingLinkY;
        if (dx * dx + dy * dy < 25) {
          e.stopImmediatePropagation();
          this._pendingLink.activate(e, this._pendingLink.text);
        }
        this._pendingLink = null;
      }, { capture: true });

      // Click-to-position: send arrow keys to move the line-editor cursor
      // to the clicked cell. Best-effort — only safe in the normal buffer
      // with mouse reporting off; alt-buffer TUIs (vim/htop) own the screen.
      screen.addEventListener('mousedown', (e) => {
        if (e.button !== 0 || e.shiftKey || e.metaKey || e.ctrlKey || e.altKey) return;
        this._pendingClickX = e.clientX;
        this._pendingClickY = e.clientY;
        this._pendingClick = true;
      });
      screen.addEventListener('mouseup', (e) => {
        if (!this._pendingClick) return;
        this._pendingClick = false;
        const dx = e.clientX - this._pendingClickX;
        const dy = e.clientY - this._pendingClickY;
        if (dx * dx + dy * dy >= 25) return; // dragged → selection, leave it
        const buf = this.term.buffer.active;
        // Same "is this gesture ours to interpret?" test as the wheel
        // handler — only in the normal buffer with mouse reporting off.
        if (!shouldScrollViewport({
          bufferType: buf?.type,
          mouseTrackingMode: this.term.modes?.mouseTrackingMode,
        })) return;
        const rect = screen.getBoundingClientRect();
        const cellW = rect.width / this.term.cols;
        const cellH = rect.height / this.term.rows;
        if (!(cellW > 0) || !(cellH > 0)) return;
        const col = Math.floor((e.clientX - rect.left) / cellW);
        const row = Math.floor((e.clientY - rect.top) / cellH);
        if (col < 0 || row < 0 || col >= this.term.cols || row >= this.term.rows) return;
        // Only act when click is on the cursor's row — otherwise we'd send
        // arrow-key spam that line editors partially consume and partially
        // echo as literal "[D".
        if (row !== buf.cursorY) return;
        // Clamp the target column to the last non-space cell on this row,
        // so clicking in the empty area past end-of-input does nothing.
        const line = buf.getLine(buf.viewportY + row);
        if (!line) return;
        const text = line.translateToString(true);
        const lastCol = text.replace(/\s+$/, '').length;
        const target = Math.min(col, lastCol);
        const delta = target - buf.cursorX;
        if (delta === 0) return;
        const seq = delta > 0 ? '\x1b[C'.repeat(delta) : '\x1b[D'.repeat(-delta);
        this._writePty(seq);
      });
    }

    // Single custom key handler. xterm.js keeps only ONE custom key event
    // handler — a second attachCustomKeyEventHandler() call silently replaces
    // the first — so every app-level binding must live here, not in a second
    // registration.
    this.term.attachCustomKeyEventHandler((e) => {
      if (e.type !== 'keydown') return true;

      // macOS Cmd+Backspace → Ctrl+U (kill to start of line). Browser doesn't
      // translate this for us when xterm's helper-textarea is focused. Gated
      // to mac so the Windows key on Linux/Windows can't accidentally fire it.
      if (isMac && e.metaKey && !e.ctrlKey && !e.altKey && e.key === 'Backspace') {
        e.preventDefault();
        this._writePty('\x15');
        return false;
      }
      // Shift+Enter → insert a newline in the agent's input instead of
      // submitting. xterm sends a bare \r for Shift+Enter and drops the
      // Shift, so Claude/Codex can't tell it from Enter and submit.
      // NEWLINE_SEQ (Ctrl+J / \x0a) is the newline byte both agents accept
      // with no terminal config. Plain Enter still submits. Cmd/Ctrl+Enter
      // is intentionally not used here — it is the grid-project toggle, and
      // the capture-phase window handler consumes it before xterm sees it.
      if (isShiftEnter(e)) {
        e.preventDefault();
        this._writePty(NEWLINE_SEQ);
        return false;
      }
      // App-level shortcuts that xterm would otherwise translate into a
      // control sequence and forward to the PTY (where the shell beeps because
      // the binding is meaningless). Returning false tells xterm to ignore the
      // event; it still bubbles to the window-level keydown handler that runs
      // the actual shortcut. Ctrl+` is intentionally Ctrl-only on every
      // platform (mirrors VS Code; macOS already uses ⌘` for window cycling).
      if (e.ctrlKey && !e.metaKey && e.code === 'Backquote') {
        return false;
      }

      // Ctrl+Shift copy/paste/select-all. Required because when an inner
      // program (Claude CLI, vim) enables DEC mouse tracking, xterm.js
      // forwards mouse events to the PTY instead of using them for text
      // selection — leaving no way to copy output. Ctrl+Shift+A selects the
      // full scrollback, Ctrl+Shift+C copies the current selection,
      // Ctrl+Shift+V pastes from the system clipboard.
      if (!e.ctrlKey || !e.shiftKey || e.altKey || e.metaKey) return true;
      const key = e.key.toLowerCase();
      if (key === 'c') {
        const sel = this.term.getSelection();
        // SetClipboardText (Go-side via atotto/clipboard) rather than
        // wails runtime.ClipboardSetText — the latter is broken on
        // Windows (non-STA goroutine, OpenClipboard fails silently).
        if (sel) SetClipboardText(sel).catch(reportFailure('copy'));
        return false;
      }
      if (key === 'v') {
        ClipboardGetText().then((text) => {
          if (text) this._writePty(text);
        }).catch(reportFailure('paste'));
        return false;
      }
      if (key === 'a') {
        this.term.selectAll();
        return false;
      }
      return true;
    });

    // Visual focus (.term-focused) and keyboard focus (xterm's
    // helper-textarea) are reconciled atomically by setFocusedTile(id),
    // which is the sole writer of .term-focused. Driving the class off
    // browser focusin/focusout events used to race with DOM churn during
    // view transitions (single ↔ grid, renderGrid's appendChild reorder,
    // xterm.open mounting new helper-textareas), leaving a tile visually
    // focused while keystrokes went nowhere. Visual focus is now a pure
    // projection of state.activeId, gated by whether a modal/rename
    // owns the keyboard — they can't drift.

    this.attached = false;
    // needsReattach is set by pty:disconnect when our attach connection
    // drops (e.g. Restart Session closes the daemon-side PTY). The next
    // session:event(updated, alive=true) consumes the flag and triggers
    // ensureAttached so the new PTY's stream resumes without the user
    // having to switch sessions and back.
    this.needsReattach = false;

    // Track the OSC-set window title from the running TUI (vim, htop,
    // claude code, etc.) so the app title bar can show it after the
    // session name when this session is active.
    this.termTitle = '';
    this.term.onTitleChange((title) => {
      this.termTitle = title || '';
      this._renderTermTitle();
      if (state.activeId === this.info.id) updateAppTitle();
    });

    this._writePty = (data) => {
      const bytes = new TextEncoder().encode(data);
      let bin = '';
      for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
      // Intentionally no reportFailure: this fires per keystroke, so a
      // dead daemon would flood the status bar with one error per key.
      // The disconnect itself is surfaced once ("control disconnected").
      WriteStdin(this.info.id, btoa(bin));
    };
    this.term.onData((data) => this._writePty(data));

    // Click anywhere on the tile (header or body) selects this session.
    this.host.addEventListener('mousedown', () => {
      if (state.activeId !== this.info.id) {
        setActive(this.info.id);
        updateSidebarSelection();
        if (state.view === 'single') {
          // Switch terms in single mode; in grid mode every tile is
          // already visible so there's nothing else to do.
          showSingle(this.info.id);
        } else {
          renderGrid();
        }
      } else {
        // Reclick on the active tile — still clears any leftover
        // attention indicator.
        clearAttention(this.info.id);
      }
    });

    // Double-click the tile name to rename inline (same affordance
    // as the sidebar). The header's mousedown selects the tile;
    // dblclick on the name then opens the editor.
    this.tileName.addEventListener('dblclick', (e) => {
      e.stopPropagation();
      this._beginRename();
    });

    // BEL on a non-focused session marks it as needing attention and
    // fires a desktop notification. xterm.js v5 exposes onBell.
    this.term.onBell(() => {
      onSessionBell(this.info);
    });

    // Dead-session overlay: hidden until the underlying process exits
    // (Alive transitions true→false). Centered card with primary
    // "Close session" (Enter) and secondary "Dismiss" (Escape).
    this.deadOverlay = document.createElement('div');
    this.deadOverlay.className = 'dead-overlay';
    this.deadOverlay.setAttribute('role', 'alertdialog');
    this.deadOverlay.setAttribute('aria-label', 'Session ended');
    this.deadOverlay.hidden = true;
    const card = document.createElement('div');
    card.className = 'dead-card';
    const title = document.createElement('div');
    title.className = 'dead-title';
    title.textContent = 'Session ended';
    const subtitle = document.createElement('div');
    subtitle.className = 'dead-subtitle';
    subtitle.textContent = 'The process running in this session has exited.';
    const buttons = document.createElement('div');
    buttons.className = 'dead-buttons';
    this.deadCloseBtn = document.createElement('button');
    this.deadCloseBtn.className = 'dead-btn primary';
    this.deadCloseBtn.textContent = 'Close session';
    this.deadCloseBtn.addEventListener('click', (e) => {
      e.stopPropagation();
      this._closeDead();
    });
    this.deadDismissBtn = document.createElement('button');
    this.deadDismissBtn.className = 'dead-btn secondary';
    this.deadDismissBtn.textContent = 'Dismiss';
    this.deadDismissBtn.addEventListener('click', (e) => {
      e.stopPropagation();
      this._dismissDead();
    });
    buttons.append(this.deadCloseBtn, this.deadDismissBtn);
    card.append(title, subtitle, buttons);
    this.deadOverlay.append(card);
    this.host.append(this.deadOverlay);
    this.deadOverlayShown = false;

    // Take over wheel handling. xterm's default wheel→lines math
    // honors raw deltaY, which on macOS trackpads with momentum
    // pumps in events of 200–400px each — enough to fly past a
    // 5000-line scrollback in a single swipe. We cap each event to
    // a sane line count so the user can actually read history.
    // Capture phase + stopPropagation prevents xterm's own handler
    // from firing. wheelToScrollLines normalizes deltaMode / legacy
    // wheelDeltaY so the cap math doesn't collapse to zero (and the
    // terminal become unscrollable) on WKWebView builds that report
    // wheel events in line/page mode — see lib/wheel-scroll.js.
    const linesPerPixel = 1 / 14; // ~one line per ~14 px of delta
    const maxLinesPerEvent = 8;   // about half a screen on a small tile
    this.host.addEventListener('wheel', (e) => {
      // Only take over the wheel in the normal buffer with mouse reporting
      // off. In the alternate buffer (Claude, vim, htop) scrollLines is a
      // no-op, and with mouse tracking on the app expects the wheel as mouse
      // events — swallowing it here is why Claude wouldn't scroll while pi
      // (a plain line buffer) did. Let xterm handle those natively.
      const buf = this.term.buffer.active;
      if (!shouldScrollViewport({
        bufferType: buf?.type,
        mouseTrackingMode: this.term.modes?.mouseTrackingMode,
      })) return;
      e.preventDefault();
      e.stopPropagation();
      // Stamp user-scroll intent so the jump detector attributes the
      // resulting onScroll to the user, not to a renderer/replay event.
      this._lastUserScrollTs = nowMs();
      const lines = wheelToScrollLines(e, { linesPerPixel, maxLinesPerEvent });
      // Gated wheel trace: on a machine where the terminal won't scroll,
      // set localStorage `hive.debug` = '1', reload, try to scroll, then
      // dump window.__hive_scrolltrace to see the raw delta the webview
      // delivered vs. the line count we derived from it.
      if (scrollTrace.rec.enabled) {
        scrollTrace.rec('wheel', {
          id: this.info.id,
          deltaY: e.deltaY, deltaMode: e.deltaMode,
          wheelDeltaY: e.wheelDeltaY, lines,
        });
      }
      if (lines !== 0) this.term.scrollLines(lines);
    }, { capture: true, passive: false });

    // Follow-intent tracking (ALWAYS ON — this is the fix for the
    // scroll-jump bug). "Is the user at the bottom?" must be derived from
    // the user's own scroll gestures, never from buffer geometry: during
    // heavy output at the scrollback cap, xterm transiently drops the
    // viewport off the bottom (cap-trim pins baseY at the cap while
    // viewportY drifts) even though the user never scrolled. Inferring
    // wasAtBottom from `baseY - viewportY` in _onBodyResize then mis-read
    // "not at bottom" and armed a restore-into-history replay. We instead
    // latch _followBottom from real scroll gestures and ignore parse-
    // driven drift.
    this._followBottom = true;
    this._lastUserScrollTs = -Infinity;
    this._lastReplayTs = -Infinity;
    this._lastViewportY = this.term.buffer.active?.viewportY ?? 0;
    // Set by handleScrollbackEvent while a replay restream is in flight, and
    // a reentrancy guard for the bottom re-pin below.
    this._replaysInFlight = 0;
    this._repinning = false;

    // Keyboard scrollback (Shift+PageUp/Down, Shift+Home/End) is user
    // intent too. xterm handles these internally; we only timestamp them
    // so the onScroll below attributes the resulting move to the user.
    this.body.addEventListener('keydown', (e) => {
      if (e.shiftKey && (e.key === 'PageUp' || e.key === 'PageDown'
        || e.key === 'Home' || e.key === 'End')) {
        this._lastUserScrollTs = nowMs();
      }
    }, { capture: true });

    this.term.onScroll(() => {
      const buf = this.term.buffer.active;
      if (!buf) return;
      const from = this._lastViewportY;
      const to = buf.viewportY;
      this._lastViewportY = to;
      const now = nowMs();
      // Only a recent user gesture may change follow-intent. A move with
      // no gesture behind it is parse-driven cap-trim drift — ignore it,
      // so a wobbling viewport never clears "follow the bottom".
      const userDriven = (now - this._lastUserScrollTs) <= USER_SCROLL_GRACE_MS;
      if (userDriven) {
        this._followBottom = (buf.baseY - to) <= STICKY_BOTTOM_LINES;
      }

      // Keep a FOLLOWING viewport glued to the bottom for the WHOLE replay
      // restream, not just at replay-done. A full-buffer restream spans many
      // frames; the begin handler's term.reset() wipes the viewport to the
      // top and cap-trim then strands it in history, so without this the
      // viewport drifts up mid-restream and only re-snaps at done — a visible
      // scroll-jump (the user-reported signature: following:true, tiny
      // sinceReplayMs). Re-pin only a genuine follower (never a reader
      // scrolled up: _followBottom is false for them, and a user gesture this
      // tick set it above). Reentrancy-guarded — scrollToBottom re-enters
      // onScroll, which then sees us at the bottom and stops.
      if (this._replaysInFlight > 0 && this._followBottom && !this._repinning
        && (buf.baseY - to) > STICKY_BOTTOM_LINES && !userDriven) {
        this._repinning = true;
        try { this.term.scrollToBottom(); } finally { this._repinning = false; }
      }

      // Scroll-jump auto-detector (gated on hive.debug=1): record any
      // UPWARD move no user gesture explains, and freeze the trace so
      // heavy output can't rotate the evidence away before it's dumped.
      // Skip when `from` exceeds the current baseY: the buffer just shrank
      // (term.reset() on replay-begin / reattach), so the stale pre-reset
      // viewportY would read as a huge spurious jump and pollute the trace.
      if (scrollTrace.rec.enabled
        && from <= buf.baseY
        && classifyViewportMove({
          from, to, lastUserScrollTs: this._lastUserScrollTs, now,
          userGraceMs: USER_SCROLL_GRACE_MS,
        }) === 'auto-up') {
        scrollTrace.rec('viewport-jump', {
          id: this.info.id,
          from, to, baseY: buf.baseY, bufType: buf.type,
          cols: this.term.cols, rows: this.term.rows, view: state.view,
          attached: this.attached, following: this._followBottom,
          sinceReplayMs: this._lastReplayTs > -Infinity ? Math.round(now - this._lastReplayTs) : null,
          sinceUserScrollMs: this._lastUserScrollTs > -Infinity ? Math.round(now - this._lastUserScrollTs) : null,
        });
        snapshotScrollJump();
      }
    });
  }

  _attachWebgl() {
    // Build / re-build the WebGL addon. Called at init and on
    // context-loss recovery. After a fresh attach the renderer's atlas
    // is empty, so force a full repaint to overwrite whatever stale
    // pixels were left behind by the lost context.
    try {
      const webgl = new WebglAddon();
      webgl.onContextLoss(() => this._onWebglContextLoss());
      this.term.loadAddon(webgl);
      this.webgl = webgl;
      try { this.term.refresh(0, this.term.rows - 1); } catch {}
      return true;
    } catch {
      this.webgl = null;
      return false;
    }
  }

  _onWebglContextLoss() {
    // The current addon's context died (commonly: too many WebGL
    // contexts process-wide). Recovery logic — dispose, reattach, fall
    // back to refresh — lives in lib/renderer-recovery.js so it can be
    // unit-tested without xterm or a real WebGL context.
    const dead = this.webgl;
    this.webgl = null;
    if (scrollTrace.rec.enabled) scrollTrace.rec('webgl-context-loss', { id: this.info.id });
    const { reattached } = recoverFromContextLoss({
      dispose: () => dead?.dispose(),
      reattach: () => this._attachWebgl(),
      refresh: () => this.term.refresh(0, this.term.rows - 1),
    });
    // reattached=false means we just fell back to the DOM renderer — its
    // char metrics differ from WebGL's, so the next fit can shift cols
    // and fire a replay no user action explains. Logging it lets the
    // trace tie a "spontaneous" jump back to a renderer fallback.
    if (scrollTrace.rec.enabled) scrollTrace.rec('webgl-recover', { id: this.info.id, reattached });
  }

  _installRendererRecoveryListeners() {
    // Clear the glyph atlas and force a full repaint. Cheap; safe to
    // call when no WebGL addon is loaded (DOM renderer ignores the
    // atlas hint and still benefits from the refresh).
    this._refreshRenderer = () => {
      if (scrollTrace.rec.enabled) scrollTrace.rec('renderer-refresh', { id: this.info.id });
      try { this.webgl?.clearTextureAtlas(); } catch {}
      try { this.term.refresh(0, this.term.rows - 1); } catch {}
    };

    // DPR change: move-to-different-display or OS zoom. A
    // `(resolution: Xdppx)` MQL only fires `change` on the single
    // transition away from X, so the helper rebinds against the new
    // DPR inside each handler — feature-detected and self-teardown so
    // it's safe on Chromium-CEF builds that don't expose matchMedia.
    this._dprWatcher = bindDprWatcher({
      matchMedia: (q) => window.matchMedia(q),
      getDpr: () => window.devicePixelRatio || 1,
      onChange: () => this._refreshRenderer(),
    });

    // Visibility transitions: occlusion / GPU sleep can invalidate the
    // backbuffer without firing context-loss. Repaint on return.
    this._onVisibility = () => {
      if (shouldRefreshOnVisibility(document.visibilityState)) this._refreshRenderer();
    };
    document.addEventListener('visibilitychange', this._onVisibility);
  }

  setInfo(info) {
    this.info = info;
    this.host.style.setProperty('--session-color', info.color || '#888');
    this.tileName.textContent = info.name;
    this.header.setAttribute('aria-label', `Session ${info.name}`);
    const wtBranch = info.worktreeBranch ?? info.worktree_branch;
    if (wtBranch) {
      this.tileWorktree.style.display = '';
      this.tileWorktree.title = `Worktree: ${wtBranch}`;
    } else {
      this.tileWorktree.style.display = 'none';
      this.tileWorktree.title = '';
    }
    this._renderTermTitle();
  }

  _renderTermTitle() {
    // Hide the slot when the TUI hasn't set a title or it just echoes
    // the session name (avoids "foo — foo").
    const t = this.termTitle || '';
    if (!t || t === this.info.name) {
      this.tileTermTitle.textContent = '';
      this.tileTermTitle.style.display = 'none';
    } else {
      this.tileTermTitle.textContent = t;
      this.tileTermTitle.title = t;
      this.tileTermTitle.style.display = '';
    }
  }

  // _beginRename hides the tile-name span, drops an input next to
  // it, and calls UpdateSession on Enter / blur. Escape cancels.
  // The next session:event(updated) calls setInfo which refreshes
  // tileName.textContent; we just need to put the span back in DOM.
  _beginRename() {
    if (this._renameInput) return; // already editing
    const input = document.createElement('input');
    input.type = 'text';
    input.className = 'tile-name-input';
    input.value = this.info.name;
    this._renameInput = input;
    this.tileName.style.display = 'none';
    this.tileName.parentNode.insertBefore(input, this.tileName);
    // Drop the visual focus border before stealing keyboard focus —
    // setFocusedTile is the only writer of .term-focused, so without
    // this the border would linger while the rename input owns input.
    setFocusedTile(null);
    input.focus();
    input.select();
    let done = false;
    const finish = (commit) => {
      if (done) return;
      done = true;
      const next = input.value.trim();
      input.remove();
      this._renameInput = null;
      this.tileName.style.display = '';
      if (commit && next && next !== this.info.name) {
        UpdateSession(this.info.id, next, '', -1).catch(reportFailure('rename'));
      }
      refocusActiveTerm();
    };
    // ONE capture-phase listener handles Enter/Escape AND shields the
    // input from xterm / global hotkey handlers. It must be a single
    // listener: stopPropagation() from a capture listener at the target
    // also cancels the target's own bubble-phase listeners (DOM dispatch
    // skips the bubble invocation once the flag is set), so a separate
    // bubble-phase Enter handler would never run — Enter/Escape were
    // dead and renames only ever committed via blur.
    input.addEventListener('keydown', (e) => {
      e.stopPropagation();
      if (e.key === 'Enter') finish(true);
      else if (e.key === 'Escape') finish(false);
    }, { capture: true });
    input.addEventListener('blur', () => finish(true));
  }

  setProject(name, color) {
    this.tileProject.textContent = name || '';
    this.host.style.setProperty('--project-color', color || '#888');
  }

  show() {
    // Becoming visible flips display from none → block. fit.fit()
    // updates xterm's cols/rows synchronously, but the WebGL renderer
    // schedules the *canvas pixel resize* on rAF — so for one frame
    // the stale (grid-cell-sized, or last-zoom-sized) canvas is CSS-
    // stretched into the new body box, producing a huge-text flash.
    // Gate paint with visibility:hidden across the rAF: layout still
    // runs (fit measures the real body box), only the pixel paint is
    // suppressed until after the renderer has caught up.
    this.body.style.visibility = 'hidden';
    this.host.classList.add('visible');
    void this.body.clientWidth;
    this._onBodyResize();
    if (this._revealRaf) cancelAnimationFrame(this._revealRaf);
    this._revealRaf = requestAnimationFrame(() => {
      this._revealRaf = 0;
      this.body.style.visibility = '';
    });
  }

  hide() {
    this.host.classList.remove('visible');
    // Cancel any in-flight reveal so the next show() starts from a
    // known-good state if the user switches away during the rAF gate.
    if (this._revealRaf) {
      cancelAnimationFrame(this._revealRaf);
      this._revealRaf = 0;
    }
    this.body.style.visibility = '';
  }

  // _onBodyResize is the single resize entry point. ResizeObserver
  // delivers the call post-layout, so fit.fit() reads correct dims
  // — no rAF dance needed. Font-size changes call this explicitly
  // (the body box doesn't change, so RO won't fire on its own).
  _onBodyResize() {
    // RO can fire with a zero box when the host is display:none (tile
    // not .visible, or hidden because outside the grid scope). fit.fit()
    // on a zero-size body produces garbage dims — skip until visible.
    if (this.body.clientWidth === 0 || this.body.clientHeight === 0) return;

    // First-time visibility for a deferred attach: hand off to
    // ensureAttached, which does its own fit.fit() before OpenSession.
    if (this._pendingAttach) {
      this._pendingAttach = false;
      this.ensureAttached();
      return;
    }

    // Preserve "viewport pinned to bottom" across the resize. xterm's
    // own resize doesn't auto-snap to bottom after reflow; without this,
    // a user scrolled to the latest line would land mid-history.
    //
    // wasAtBottom comes from _followBottom — the user's own scroll
    // intent — NOT from `baseY - viewportY`. Under heavy output at the
    // scrollback cap, xterm transiently drops the viewport off the bottom
    // (cap-trim) for a user who never scrolled; the old geometry check
    // mis-read that as "scrolled up" and armed a restore-into-history
    // replay (the scroll-jump bug). _followBottom is latched only by real
    // gestures, so cap-trim drift can't flip it.
    const buf = this.term.buffer.active;
    const wasAtBottom = this._followBottom;
    // Swallow throw and continue: a transient FitAddon error (e.g. a
    // race against teardown) shouldn't drop the daemon-side resize.
    const prevCols = this.term.cols;
    try { this.fit.fit(); } catch { /* keep going with last-known dims */ }
    if (this.attached) {
      // Intentionally no reportFailure: resize fires continuously during
      // window/sidebar drags, so a dead daemon would flood the status
      // bar. The disconnect is surfaced once ("control disconnected").
      ResizeSession(this.info.id, this.term.cols, this.term.rows);
    }
    if (wasAtBottom) this.term.scrollToBottom();

    // If the column count changed materially relative to the
    // *baseline* (the cols active at the last replay, or initial
    // attach if no replay has fired yet), xterm's scrollback is
    // stale — its rendered rows were baked at the old width and
    // xterm.js does not reflow history on resize. Ask the daemon to
    // re-stream the raw byte ring; the EventScrollbackReplayBegin
    // handler will term.reset() before the bytes arrive, and the
    // daemon serializes the replay against live fanout so nothing
    // interleaves.
    //
    // Comparing against a baseline (rather than the just-previous
    // measurement) means a 80→84→83 sequence still triggers a replay
    // — the final width is 3 cols off the baseline, even though
    // neither single step crosses the threshold. We also unconditionally
    // clear any pending timer on every resize, then re-arm only if
    // the *current* delta still warrants a replay; otherwise an old
    // measurement that briefly crossed the threshold would leave a
    // stale timer armed.
    if (this._replayBaselineCols === undefined) {
      this._replayBaselineCols = prevCols || this.term.cols;
    }
    if (this._replayTimer) {
      clearTimeout(this._replayTimer);
      this._replayTimer = 0;
      // Cancel-without-rearm must also clear any stale wants-bottom
      // intent — otherwise a `false` captured at the previous resize
      // outlives its replay and suppresses the bottom-snap on the
      // next replay-done from any source (re-attach, daemon-initiated
      // atomic replay on subscribe, etc.).
      delete this._replayWantsBottom;
    }
    if (scrollTrace.rec.enabled) {
      scrollTrace.rec('resize', {
        id: this.info.id, prevCols, cols: this.term.cols,
        baseline: this._replayBaselineCols, wasAtBottom,
        // Raw geometry behind wasAtBottom: during heavy output xterm can
        // lose bottom-follow (baseY pinned at the scrollback cap while
        // viewportY drifts), so wasAtBottom reads false even though the
        // user never scrolled — the suspected arm of a spurious up-jump.
        baseY: buf?.baseY, viewportY: buf?.viewportY, bufType: buf?.type,
      });
    }
    if (this.attached && shouldRequestReplay(this._replayBaselineCols, this.term.cols)) {
      // Carry the user's pre-resize "at bottom?" intent through to the
      // scrollback_replay_done handler. If the user was actively reading
      // scrollback (wasAtBottom === false), the replay must not yank
      // them back to the bottom on completion.
      this._replayWantsBottom = wasAtBottom;
      this._replayTimer = setTimeout(() => {
        this._replayTimer = 0;
        // Skip the replay while on the ALTERNATE screen (full-screen TUIs):
        // no user-facing scrollback there, the program repaints from SIGWINCH,
        // and re-streaming the multi-MB ring would freeze the renderer. The
        // decision (and whether to advance the baseline) lives in a pure
        // helper so the skip + baseline-untouched behavior is unit-tested.
        // Checked at fire time so a just-attached session that has since
        // entered the alt screen via its snapshot is caught too.
        const { replay, baseline } = decideResizeReplay({
          bufferType: this.term.buffer.active.type,
          cols: this.term.cols,
          baselineCols: this._replayBaselineCols,
        });
        this._replayBaselineCols = baseline;
        if (!replay) {
          delete this._replayWantsBottom;
          if (scrollTrace.rec.enabled) {
            scrollTrace.rec('replay-skip-alt', { id: this.info.id, cols: this.term.cols });
          }
          return;
        }
        if (scrollTrace.rec.enabled) {
          scrollTrace.rec('replay-request', { id: this.info.id, cols: this.term.cols });
        }
        RequestScrollbackReplay(this.info.id).catch(() => { /* attach may have closed */ });
      }, REPLAY_DEBOUNCE_MS);
    }
  }

  // rebaselineReplayCols resets _replayBaselineCols to the current
  // term.cols and clears any pending replay timer. Use this when grid
  // geometry changes for a reason that is NOT a user-driven window
  // resize (first-attach in grid; minimize/restore reflowing the
  // remaining tiles). Those reflows shrink/widen the tile but the
  // scrollback was already written at the new width once xterm
  // re-fitted, so triggering shouldRequestReplay would be spurious and
  // visibly drops or duplicates content. Pure window resize is handled
  // by the threshold path in _onBodyResize and must not call this.
  rebaselineReplayCols(_reason) {
    applyRebaseline(this);
  }

  async ensureAttached() {
    if (this.attached) return;
    // Don't attempt to attach to a session known to be dead — the daemon
    // will refuse. Show the dead overlay with the error reason instead.
    if (state.aliveById.get(this.info.id) === false) {
      this.setDead(true, this.info.last_error || 'The process failed to start.');
      return;
    }
    // If the host is still display:none, the body has no box yet and
    // fit.fit() would measure 0×0. Defer until ResizeObserver fires
    // with a real size — _onBodyResize will re-enter ensureAttached.
    if (this.body.clientWidth === 0 || this.body.clientHeight === 0) {
      this._pendingAttach = true;
      return;
    }
    this.fit.fit();
    // Reset _followBottom on attach: it may be stale from a previous
    // session (user scrolled up, closed Hive, reopened). The initial
    // attach replay must snap to bottom — _followBottom = true ensures
    // the replay-done handler doesn't skip the snap via the restore path.
    this._followBottom = true;
    try {
      await OpenSession(this.info.id, this.term.cols, this.term.rows);
      this.attached = true;
      // Anchor the replay baseline to the actual fitted cols for this
      // tile. Without this, a later _onBodyResize would initialize the
      // baseline from a stale xterm default (80) while term.cols is the
      // real grid-cell width — the next resize crosses the threshold
      // and fires a spurious scrollback replay on first grid entry.
      this.rebaselineReplayCols('first-attach');
    } catch (err) {
      this.term.write(`\r\n\x1b[31m[attach failed: ${err}]\x1b[0m\r\n`);
    }
  }

  writeData(b64) {
    const bin = atob(b64);
    const bytes = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
    this.term.write(this.decoder.decode(bytes, { stream: true }));
  }

  destroy() {
    // Intentionally silent: destroy() tears down a session that's already
    // gone; a failed CloseAttach has nothing for the user to act on.
    CloseAttach(this.info.id).catch(() => {});
    if (this._revealRaf) cancelAnimationFrame(this._revealRaf);
    this.ro.disconnect();
    if (this._dprWatcher) {
      try { this._dprWatcher.teardown(); } catch {}
      this._dprWatcher = null;
    }
    if (this._onVisibility) {
      document.removeEventListener('visibilitychange', this._onVisibility);
    }
    // Release the GL context proactively so a many-tile session doesn't
    // sit on it until GC and push another tile over the browser cap.
    try { this.webgl?.dispose(); } catch {}
    this.webgl = null;
    this.term.dispose();
    this.host.remove();
  }

  setDead(isDead, reason) {
    this.deadOverlayShown = isDead;
    this.deadOverlay.hidden = !isDead;
    this.host.classList.toggle('dead', isDead);
    if (isDead) {
      const subtitle = this.deadOverlay.querySelector('.dead-subtitle');
      if (subtitle && reason) {
        subtitle.textContent = reason;
      }
      // Defer focus so it lands after the visibility flip and after
      // any pending blur from the dying xterm.
      setTimeout(() => {
        if (this.deadOverlayShown) this.deadCloseBtn.focus();
      }, 0);
    }
  }

  _closeDead() {
    KillSession(this.info.id, true).catch(reportFailure('close'));
  }

  _dismissDead() {
    state.dismissedDead.add(this.info.id);
    this.setDead(false);
    refocusActiveTerm();
  }
}

export function applyFontSize() {
  for (const st of state.terms.values()) {
    st.term.options.fontSize = state.fontSize;
    // Body box doesn't change on font-size change, so ResizeObserver
    // won't fire — call the resize handler explicitly so fit.fit()
    // recomputes (cols, rows) from new char metrics.
    st._onBodyResize();
  }
  localStorage.setItem('hive.fontSize', String(state.fontSize));
}

export function bumpFontSize(delta) {
  const next = clampFont(state.fontSize + delta);
  if (next === state.fontSize) return;
  state.fontSize = next;
  applyFontSize();
  // flashStatus (not setStatus): per-action feedback must auto-revert,
  // not overwrite the persistent slot ("control disconnected", session
  // name) until the next nav event.
  flashStatus(`font ${state.fontSize}px`);
}

export function resetFontSize() {
  state.fontSize = DEFAULT_FONT_SIZE;
  applyFontSize();
  flashStatus(`font ${state.fontSize}px`);
}

export function ensureTerm(info) {
  let st = state.terms.get(info.id);
  if (!st) {
    st = new SessionTerm(info);
    state.terms.set(info.id, st);
  } else {
    st.setInfo(info);
  }
  const proj = state.projects.find((p) => p.id === (info.projectId ?? info.project_id));
  st.setProject(proj?.name ?? '', proj?.color ?? '');
  return st;
}
