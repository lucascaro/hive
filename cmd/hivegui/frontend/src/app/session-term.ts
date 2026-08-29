// ---------- session terminal ----------
//
// Moved verbatim from main.js: the SessionTerm class plus the font
// helpers and ensureTerm factory that manage its instances.

import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { WebglAddon } from '@xterm/addon-webgl';
import { WebLinksAddon } from '@xterm/addon-web-links';

import {
  OpenSession,
  CloseAttach,
  WriteStdin,
  ResizeSession,
  RequestScrollbackReplay,
  KillSession,
  SetClipboardText,
  ClipboardGetText,
  OpenURL,
  UpdateSession,
} from '../bridge.js';
import { state, type SessionInfo } from './state.js';
import { flashStatus, reportFailure } from './dom.js';
import { mustEl } from './el.js';
import { anyModalOpen } from './modals/registry.js';
import { openWorktrees } from './modals/worktrees.js';
import { isMac } from '../lib/platform.js';
import { displayTitle } from '../lib/term-title.js';
import {
  PHASE,
  phaseOf,
  isReady,
  isClosing,
  phasePanel,
} from '../lib/phase-steps.js';
import { isShiftEnter, macLineEditSeq, NEWLINE_SEQ } from '../lib/keymap.js';
import { DEFAULT_FONT_SIZE, clampFont } from '../lib/font.js';
import {
  shouldRefreshOnVisibility,
  recoverFromContextLoss,
  bindDprWatcher,
} from '../lib/renderer-recovery.js';
import {
  acquireWebglSlot,
  releaseWebglSlot,
  recordWebglLoss,
  type WebglLossState,
} from '../lib/webgl-budget.js';
import { LogFrontend } from '../bridge.js';

// A WebGL context that dies, gets reattached, and dies again in a tight
// loop pins a CPU core and freezes the whole GUI (the "works for days
// then locks up" report). After this many losses within the window we
// stop reattaching and leave the tile on the DOM renderer for good.
const WEBGL_LOSS_STORM_MAX = 3;
const WEBGL_LOSS_STORM_WINDOW_MS = 10000;

// Best-effort disk log — the webview console is /dev/null under
// LaunchServices, so renderer/freeze evidence has nowhere else to land.
function feLog(msg: string) {
  try {
    LogFrontend(msg);
  } catch {
    /* bridge absent in tests */
  }
}
import {
  shouldRequestReplay,
  decideResizeReplay,
  REPLAY_DEBOUNCE_MS,
  applyRebaseline,
  resetFollowIntent,
} from '../lib/scrollback.js';
import { scrollTrace, snapshotScrollJump } from './trace.js';
import { classifyViewportMove } from '../lib/scroll-debug.js';
import {
  wheelToScrollLines,
  shouldScrollViewport,
  type WheelEventLike,
} from '../lib/wheel-scroll.js';
import { onSessionBell, clearAttention } from './events.js';
import {
  minimizeSession,
  updateAppTitle,
  showSingle,
  renderGrid,
} from './view.js';
import { updateSidebarSelection } from './sidebar.js';
import { setActive, setFocusedTile, refocusActiveTerm } from './focus.js';
import { beginInlineRename } from './inline-rename.js';

// Monotonic millisecond clock for the scroll-jump detector. Falls back
// to 0 where performance isn't available (never in a real renderer).
function nowMs() {
  try {
    return performance.now();
  } catch {
    return 0;
  }
}

// isClick tells a click from a drag by squared distance between
// mousedown and mouseup — cheaper than Math.hypot, and we only ever
// compare against a fixed threshold. CLICK_RADIUS_SQ = 25 = a 5px
// radius, used by both the link-activation and click-to-position
// hit-tests below.
const CLICK_RADIUS_SQ = 25;
function isClick(dx: number, dy: number) {
  return dx * dx + dy * dy < CLICK_RADIUS_SQ;
}

// A viewport within this many lines of the bottom counts as "at the
// bottom". Tolerates TUIs (codex etc.) that park a line or two short.
const STICKY_BOTTOM_LINES = 2;

// How recently a user scroll gesture must have fired for an onScroll to
// count as user-driven (vs parse-driven cap-trim drift).
const USER_SCROLL_GRACE_MS = 250;

// How long the loading panel is held past PhaseReady while waiting for
// the attach replay to paint. Only a fallback: a tile that never
// attaches (hidden, minimized) gets no replay, and must not keep a
// spinner forever.
const PHASE_REVEAL_CAP_MS = 2000;

// The link the Linkifier currently has under the cursor, reached through
// xterm's private `_core`: the public API exposes no way to ask "is a
// link here?", and the mouse-protocol workaround below needs exactly that.
interface TermLink {
  text: string;
  activate(event: MouseEvent, text: string): void;
}
type LinkifierPeek = {
  _core?: { linkifier?: { currentLink?: { link?: TermLink } | null } | null };
};

export class SessionTerm {
  info: SessionInfo;
  decoder: TextDecoder;
  host: HTMLDivElement;
  header: HTMLDivElement;
  body: HTMLDivElement;
  tileColor: HTMLSpanElement;
  tileName: HTMLSpanElement;
  tileWorktree: HTMLSpanElement;
  tileProject: HTMLSpanElement;
  tileTermTitle: HTMLSpanElement;
  tileMinimize: HTMLButtonElement;
  term: Terminal;
  fit: FitAddon;
  ro: ResizeObserver;
  attached: boolean;
  needsReattach: boolean;
  termTitle: string;
  // Assigned in the constructor body (it closes over `this.info`), so no
  // initializer — unlike the fields below, which are written by helpers
  // the constructor calls and would otherwise trip strictPropertyInitialization.
  _writePty: (data: string) => void;

  // Dead-session overlay.
  deadOverlay: HTMLDivElement;
  deadCloseBtn: HTMLButtonElement;
  deadDismissBtn: HTMLButtonElement;
  deadOverlayShown: boolean;

  // Lifecycle-phase overlay: the loading panel shown while the daemon
  // is still creating (or tearing down) this session. See
  // lib/phase-steps.ts for the model.
  phaseOverlay: HTMLDivElement;
  phaseStatus: HTMLDivElement;
  phaseSteps: HTMLUListElement;
  phase: string = PHASE.ready;
  // The panel outlives PhaseReady until the terminal has painted (see
  // _revealAfterPhase), so "is the overlay up" is its own flag.
  phaseOverlayShown = false;
  _phaseRevealTimer = 0;

  // Renderer.
  webgl: WebglAddon | null = null;
  _hasWebglSlot = false;
  _webglGaveUp = false;
  _webglLoss?: WebglLossState;
  _dprWatcher: { teardown(): void } | null = null;
  _onVisibility: (() => void) | null = null;

  // Attach / geometry.
  _pendingAttach = false;
  _revealRaf = 0;
  // Optional (not `= 0`) because the code branches on `=== undefined` to
  // mean "no baseline measured yet".
  _replayBaselineCols?: number;
  _replayTimer = 0;
  // Deleted, not zeroed, at every cancel site — so it must be optional.
  _replayWantsBottom?: boolean;
  _replaysInFlight = 0;

  // Scroll follow-intent.
  _followBottom = true;
  _lastUserScrollTs = -Infinity;
  _lastReplayTs = -Infinity;
  _lastViewportY = 0;
  _repinning = false;
  _pointerDown = false;
  _onWindowMouseUp: (() => void) | null = null;

  // Link / click-to-position hit-testing.
  _pendingLink: TermLink | null = null;
  _pendingLinkX = 0;
  _pendingLinkY = 0;
  _pendingClick = false;
  _pendingClickX = 0;
  _pendingClickY = 0;

  _renameInput: HTMLInputElement | null = null;

  // writeData burst probe.
  _wroteBytes = 0;
  _wroteCount = 0;
  _writeWindowStart?: number;
  _writeBurstLogged = false;

  constructor(info: SessionInfo) {
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
    this.tileName.textContent = info.name ?? '';
    this.tileWorktree = document.createElement('span');
    this.tileWorktree.className = 'worktree-glyph clickable';
    this.tileWorktree.textContent = '⎇';
    this.tileWorktree.setAttribute('role', 'button');
    // Clicking the worktree marker opens the worktree browser for this
    // session's project — the same thing the identical glyph does in
    // the sidebar and on the project row. An indicator that looks like
    // a control but ignores clicks reads as broken.
    this.tileWorktree.addEventListener('click', (e) => {
      // The tile header also focuses/activates the tile; this click is
      // about the worktree, not about switching sessions.
      e.stopPropagation();
      const pid = this.info?.projectId ?? this.info?.project_id ?? '';
      const proj = state.projects.find((p) => p.id === pid);
      if (proj) openWorktrees(proj);
    });
    {
      const wtBranch = info.worktreeBranch ?? info.worktree_branch;
      if (wtBranch) {
        this.tileWorktree.title = `Worktree: ${wtBranch} — click to manage worktrees`;
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
    this.header.append(
      this.tileColor,
      this.tileName,
      this.tileWorktree,
      this.tileTermTitle,
      this.tileProject,
      this.tileMinimize,
    );

    this.body = document.createElement('div');
    this.body.className = 'term-body';

    this.host.append(this.header, this.body);
    mustEl('terms').appendChild(this.host);

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
        activate: (_e, uri) => {
          if (uri) OpenURL(uri).catch(reportFailure('open link'));
        },
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
      this.term.loadAddon(
        new WebLinksAddon((_event, uri) => {
          if (uri) OpenURL(uri).catch(reportFailure('open link'));
        }),
      );
    } catch (_err) {
      // Non-fatal; sessions still work without clickable links.
    }

    // When the running program enables mouse reporting (e.g. Claude,
    // vim), xterm sends mousedown/mouseup to the PTY and cancels the
    // event before the Linkifier can process it. Work around this by
    // intercepting clicks on the xterm screen: if a recognized link
    // is under the cursor, suppress the event so it doesn't reach the
    // mouse protocol handler, letting the Linkifier's own handlers
    // process it and call activate.
    const screen = this.body.querySelector<HTMLElement>('.xterm-screen');
    if (screen) {
      screen.addEventListener(
        'mousedown',
        (e) => {
          const link = (this.term as Terminal & LinkifierPeek)._core?.linkifier
            ?.currentLink;
          if (link?.link) {
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
        },
        { capture: true },
      );
      screen.addEventListener(
        'mouseup',
        (e) => {
          if (!this._pendingLink) return;
          // Only treat as a click if the mouse barely moved (not a drag).
          const dx = e.clientX - this._pendingLinkX;
          const dy = e.clientY - this._pendingLinkY;
          if (isClick(dx, dy)) {
            e.stopImmediatePropagation();
            this._pendingLink.activate(e, this._pendingLink.text);
          }
          this._pendingLink = null;
        },
        { capture: true },
      );

      // Click-to-position: send arrow keys to move the line-editor cursor
      // to the clicked cell. Best-effort — only safe in the normal buffer
      // with mouse reporting off; alt-buffer TUIs (vim/htop) own the screen.
      screen.addEventListener('mousedown', (e) => {
        if (e.button !== 0 || e.shiftKey || e.metaKey || e.ctrlKey || e.altKey)
          return;
        this._pendingClickX = e.clientX;
        this._pendingClickY = e.clientY;
        this._pendingClick = true;
      });
      screen.addEventListener('mouseup', (e) => {
        if (!this._pendingClick) return;
        this._pendingClick = false;
        const dx = e.clientX - this._pendingClickX;
        const dy = e.clientY - this._pendingClickY;
        if (!isClick(dx, dy)) return; // dragged → selection, leave it
        const buf = this.term.buffer.active;
        // Same "is this gesture ours to interpret?" test as the wheel
        // handler — only in the normal buffer with mouse reporting off.
        if (
          !shouldScrollViewport({
            bufferType: buf?.type,
            mouseTrackingMode: this.term.modes?.mouseTrackingMode,
          })
        )
          return;
        const rect = screen.getBoundingClientRect();
        const cellW = rect.width / this.term.cols;
        const cellH = rect.height / this.term.rows;
        if (!(cellW > 0) || !(cellH > 0)) return;
        const col = Math.floor((e.clientX - rect.left) / cellW);
        const row = Math.floor((e.clientY - rect.top) / cellH);
        if (
          col < 0 ||
          row < 0 ||
          col >= this.term.cols ||
          row >= this.term.rows
        )
          return;
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
        const seq =
          delta > 0 ? '\x1b[C'.repeat(delta) : '\x1b[D'.repeat(-delta);
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
      if (
        isMac &&
        e.metaKey &&
        !e.ctrlKey &&
        !e.altKey &&
        e.key === 'Backspace'
      ) {
        e.preventDefault();
        this._writePty('\x15');
        return false;
      }
      // macOS ⌘←/⌘→ → start / end of line. Same reason as ⌘⌫ above: the
      // chord is an emulator-level mapping everywhere else, and xterm
      // emits nothing at all for a meta-modified arrow, so releasing the
      // key from the app's shortcut handler (which grid mode still uses)
      // only produces silence unless we write the bytes here.
      const lineEdit = macLineEditSeq(e, isMac);
      if (lineEdit) {
        e.preventDefault();
        this._writePty(lineEdit);
        return false;
      }
      // Shift+Enter → insert a newline in the agent's input instead of
      // submitting. xterm sends a bare \r for Shift+Enter and drops the
      // Shift, so Claude/Codex can't tell it from Enter and submit.
      // NEWLINE_SEQ (Ctrl+J / \x0a) is the newline byte both agents accept
      // with no terminal config. Plain Enter still submits. Cmd/Ctrl+Enter
      // is deliberately NOT bound here either: it used to be the
      // grid-project toggle and was unbound outright in #249, with no
      // replacement behavior asked for.
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
        ClipboardGetText()
          .then((text) => {
            if (text) this._writePty(text);
          })
          .catch(reportFailure('paste'));
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
      for (let i = 0; i < bytes.length; i++)
        bin += String.fromCharCode(bytes[i]);
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

    // Phase overlay: opaque panel over the terminal body while the
    // session is being created. It covers the window in which the
    // shell paints its startup output, so the user lands on a settled
    // screen instead of watching rc-files scroll past.
    this.phaseOverlay = document.createElement('div');
    this.phaseOverlay.className = 'phase-overlay';
    this.phaseOverlay.setAttribute('role', 'status');
    this.phaseOverlay.setAttribute('aria-live', 'polite');
    this.phaseOverlay.hidden = true;
    const phaseCard = document.createElement('div');
    phaseCard.className = 'phase-card';
    const spinner = document.createElement('div');
    spinner.className = 'phase-spinner';
    spinner.setAttribute('aria-hidden', 'true');
    this.phaseStatus = document.createElement('div');
    this.phaseStatus.className = 'phase-status';
    this.phaseSteps = document.createElement('ul');
    this.phaseSteps.className = 'phase-steps';
    phaseCard.append(spinner, this.phaseStatus, this.phaseSteps);
    this.phaseOverlay.append(phaseCard);
    this.host.append(this.phaseOverlay);

    // Take over wheel handling. xterm's default wheel→lines math
    // honors raw deltaY, which on macOS trackpads with momentum
    // pumps in events of 200–400px each — enough to fly past a
    // 5000-line scrollback in a single swipe. We cap each event to
    // a sane line count so the user can actually read history.
    // Capture phase + stopPropagation prevents xterm's own handler
    // from firing. wheelToScrollLines normalizes deltaMode / legacy
    // wheelDeltaY so the cap math doesn't collapse to zero (and the
    // terminal become unscrollable) on WKWebView builds that report
    // wheel events in line/page mode — see lib/wheel-scroll.ts.
    const linesPerPixel = 1 / 14; // ~one line per ~14 px of delta
    const maxLinesPerEvent = 8; // about half a screen on a small tile
    this.host.addEventListener(
      'wheel',
      (e) => {
        // Only take over the wheel in the normal buffer with mouse reporting
        // off. In the alternate buffer (Claude, vim, htop) scrollLines is a
        // no-op, and with mouse tracking on the app expects the wheel as mouse
        // events — swallowing it here is why Claude wouldn't scroll while pi
        // (a plain line buffer) did. Let xterm handle those natively.
        const buf = this.term.buffer.active;
        if (
          !shouldScrollViewport({
            bufferType: buf?.type,
            mouseTrackingMode: this.term.modes?.mouseTrackingMode,
          })
        )
          return;
        e.preventDefault();
        e.stopPropagation();
        // Stamp user-scroll intent so the jump detector attributes the
        // resulting onScroll to the user, not to a renderer/replay event.
        this._lastUserScrollTs = nowMs();
        const lines = wheelToScrollLines(e, {
          linesPerPixel,
          maxLinesPerEvent,
        });
        // Gated wheel trace: on a machine where the terminal won't scroll,
        // set localStorage `hive.debug` = '1', reload, try to scroll, then
        // dump window.__hive_scrolltrace to see the raw delta the webview
        // delivered vs. the line count we derived from it.
        if (scrollTrace.rec.enabled) {
          scrollTrace.rec('wheel', {
            id: this.info.id,
            deltaY: e.deltaY,
            deltaMode: e.deltaMode,
            wheelDeltaY: (e as WheelEventLike).wheelDeltaY,
            lines,
          });
        }
        if (lines !== 0) this.term.scrollLines(lines);
      },
      { capture: true, passive: false },
    );

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

    // Pointer drag is user scroll intent too. xterm auto-scrolls the
    // viewport while a selection drag is held past the top edge, and that
    // scroll arrives with NO wheel and NO keydown — so the bottom re-pin
    // below reads it as parse-driven drift and yanks the viewport back,
    // which makes selecting text upwards impossible. (Only visible once
    // the re-pin stopped being gated on an in-flight replay.)
    //
    // A boolean, not a timestamp: the auto-scroll repeats on xterm's own
    // timer while the button is held STILL, so there is no second event to
    // re-stamp from and a mousedown timestamp alone would go stale after
    // USER_SCROLL_GRACE_MS mid-drag. onScroll refreshes the stamp while
    // this is set.
    this._pointerDown = false;
    this.body.addEventListener(
      'mousedown',
      () => {
        this._pointerDown = true;
        this._lastUserScrollTs = nowMs();
      },
      { capture: true },
    );
    // Listen for the release on `window`: a drag that leaves the tile (or
    // the window) releases outside `body`, and a stuck flag would make
    // every later drift look user-driven.
    this._onWindowMouseUp = () => {
      if (!this._pointerDown) return;
      this._pointerDown = false;
      this._lastUserScrollTs = nowMs();
    };
    window.addEventListener('mouseup', this._onWindowMouseUp, true);

    // Keyboard scrollback (Shift+PageUp/Down, Shift+Home/End) is user
    // intent too. xterm handles these internally; we only timestamp them
    // so the onScroll below attributes the resulting move to the user.
    this.body.addEventListener(
      'keydown',
      (e) => {
        if (
          e.shiftKey &&
          (e.key === 'PageUp' ||
            e.key === 'PageDown' ||
            e.key === 'Home' ||
            e.key === 'End')
        ) {
          this._lastUserScrollTs = nowMs();
        }
      },
      { capture: true },
    );

    this.term.onScroll(() => {
      const buf = this.term.buffer.active;
      if (!buf) return;
      const from = this._lastViewportY;
      const to = buf.viewportY;
      this._lastViewportY = to;
      const now = nowMs();
      // A held pointer is a live gesture for as long as it's held — keep
      // the stamp fresh so a selection auto-scroll that spans more than
      // USER_SCROLL_GRACE_MS stays attributed to the user.
      if (this._pointerDown) this._lastUserScrollTs = now;
      // Only a recent user gesture may change follow-intent. A move with
      // no gesture behind it is parse-driven cap-trim drift — ignore it,
      // so a wobbling viewport never clears "follow the bottom".
      const userDriven = now - this._lastUserScrollTs <= USER_SCROLL_GRACE_MS;
      if (userDriven) {
        this._followBottom = buf.baseY - to <= STICKY_BOTTOM_LINES;
      }

      // Keep a FOLLOWING viewport glued to the bottom against ANY non-user
      // upward drift — during a replay restream AND in steady state under a
      // high-output ("firehose") session. Two mechanisms strand a follower
      // off the bottom with no user gesture:
      //   1. replay: begin's term.reset() wipes the viewport to the top and
      //      cap-trim leaves it in history until done re-snaps it;
      //   2. cap-trim drift: at the 5000-line scrollback cap, heavy output
      //      shifts baseY faster than xterm updates viewportY, so xterm
      //      loses bottom-follow and the viewport slides up on its own.
      // #228 deliberately left (2) uncorrected — but on a real firehose
      // (multi-MB/s, e.g. Pi) that drift IS the "constant scrolling, never
      // anchored to the bottom" report, so we now re-pin it too. Safe: a
      // genuine reader has _followBottom=false (a user gesture this tick set
      // it above), so we never fight them; the _repinning guard absorbs the
      // scrollToBottom → onScroll re-entry.
      if (
        this._followBottom &&
        !this._repinning &&
        buf.baseY - to > STICKY_BOTTOM_LINES &&
        !userDriven
      ) {
        this._repinning = true;
        try {
          this.term.scrollToBottom();
        } finally {
          this._repinning = false;
        }
      }

      // Scroll-jump auto-detector (gated on hive.debug=1): record any
      // UPWARD move no user gesture explains, and freeze the trace so
      // heavy output can't rotate the evidence away before it's dumped.
      // Skip when `from` exceeds the current baseY: the buffer just shrank
      // (term.reset() on replay-begin / reattach), so the stale pre-reset
      // viewportY would read as a huge spurious jump and pollute the trace.
      if (
        scrollTrace.rec.enabled &&
        from <= buf.baseY &&
        classifyViewportMove({
          from,
          to,
          lastUserScrollTs: this._lastUserScrollTs,
          now,
          userGraceMs: USER_SCROLL_GRACE_MS,
        }) === 'auto-up'
      ) {
        scrollTrace.rec('viewport-jump', {
          id: this.info.id,
          from,
          to,
          baseY: buf.baseY,
          bufType: buf.type,
          cols: this.term.cols,
          rows: this.term.rows,
          view: state.view,
          attached: this.attached,
          following: this._followBottom,
          sinceReplayMs:
            this._lastReplayTs > -Infinity
              ? Math.round(now - this._lastReplayTs)
              : null,
          sinceUserScrollMs:
            this._lastUserScrollTs > -Infinity
              ? Math.round(now - this._lastUserScrollTs)
              : null,
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
    //
    // A tile that lost its context repeatedly (loss storm) has sworn off
    // WebGL — never reattach, or the refresh paths (DPR / visibility)
    // would restart the loop.
    if (this._webglGaveUp) return false;

    // Gate on a process-wide context budget first: past the browser's
    // simultaneous-WebGL-context cap the atlas unbinds and tiles fill
    // with magenta. Over budget → stay on the DOM renderer and just
    // repaint so nothing stale is left frozen.
    if (!acquireWebglSlot()) {
      this.webgl = null;
      this._hasWebglSlot = false;
      try {
        this.term.refresh(0, this.term.rows - 1);
      } catch {}
      return false;
    }
    try {
      const webgl = new WebglAddon();
      webgl.onContextLoss(() => this._onWebglContextLoss());
      this.term.loadAddon(webgl);
      this.webgl = webgl;
      this._hasWebglSlot = true;
      try {
        this.term.refresh(0, this.term.rows - 1);
      } catch {}
      return true;
    } catch {
      // Construct failed (no GPU / driver) — hand the slot back so a
      // healthier tile can claim it.
      releaseWebglSlot();
      this.webgl = null;
      this._hasWebglSlot = false;
      return false;
    }
  }

  _onWebglContextLoss() {
    // The current addon's context died (commonly: too many WebGL
    // contexts process-wide). Recovery logic — dispose, reattach, fall
    // back to refresh — lives in lib/renderer-recovery.ts so it can be
    // unit-tested without xterm or a real WebGL context.
    const dead = this.webgl;
    this.webgl = null;
    // Release this tile's slot before reattaching, or _attachWebgl's
    // budget check would see the dead context still counted and refuse
    // to bring the tile back up.
    if (this._hasWebglSlot) {
      releaseWebglSlot();
      this._hasWebglSlot = false;
    }

    // Storm guard: count losses in a sliding window. A context that keeps
    // dying immediately after reattach would loop dispose→reattach→loss
    // forever, pinning a core and freezing the GUI. Past the threshold we
    // stop trying WebGL for this tile and stay on the DOM renderer.
    this._webglLoss ||= { start: 0, count: 0 };
    const { count, stormed } = recordWebglLoss(
      this._webglLoss,
      nowMs(),
      WEBGL_LOSS_STORM_MAX,
      WEBGL_LOSS_STORM_WINDOW_MS,
    );
    feLog(`webgl-context-loss id=${this.info.id} count=${count}`);
    if (scrollTrace.rec.enabled)
      scrollTrace.rec('webgl-context-loss', { id: this.info.id, count });

    if (stormed) {
      // Give up on WebGL for this tile. Dispose the dead addon, don't
      // reattach, force one repaint so the DOM renderer takes over cleanly.
      this._webglGaveUp = true;
      try {
        dead?.dispose();
      } catch {
        /* best-effort */
      }
      try {
        this.term.refresh(0, this.term.rows - 1);
      } catch {}
      feLog(`webgl-give-up id=${this.info.id} — DOM renderer (loss storm)`);
      if (scrollTrace.rec.enabled)
        scrollTrace.rec('webgl-give-up', { id: this.info.id });
      return;
    }

    const { reattached } = recoverFromContextLoss({
      dispose: () => dead?.dispose(),
      reattach: () => this._attachWebgl(),
      refresh: () => this.term.refresh(0, this.term.rows - 1),
    });
    // reattached=false means we just fell back to the DOM renderer — its
    // char metrics differ from WebGL's, so the next fit can shift cols
    // and fire a replay no user action explains. Logging it lets the
    // trace tie a "spontaneous" jump back to a renderer fallback.
    if (scrollTrace.rec.enabled)
      scrollTrace.rec('webgl-recover', { id: this.info.id, reattached });
  }

  // Clear the glyph atlas and force a full repaint. Cheap; safe to
  // call when no WebGL addon is loaded (DOM renderer ignores the
  // atlas hint and still benefits from the refresh).
  //
  // A method rather than the constructor-assigned closure it used to be:
  // it is only ever reached through `this`, so the field bought nothing
  // and would have needed a throwaway initializer to satisfy
  // strictPropertyInitialization.
  _refreshRenderer() {
    if (scrollTrace.rec.enabled)
      scrollTrace.rec('renderer-refresh', { id: this.info.id });
    try {
      this.webgl?.clearTextureAtlas();
    } catch {}
    try {
      this.term.refresh(0, this.term.rows - 1);
    } catch {}
  }

  _installRendererRecoveryListeners() {
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
      if (shouldRefreshOnVisibility(document.visibilityState))
        this._refreshRenderer();
    };
    document.addEventListener('visibilitychange', this._onVisibility);
  }

  setInfo(info: SessionInfo) {
    this.info = info;
    this.host.style.setProperty('--session-color', info.color || '#888');
    this.tileName.textContent = info.name ?? '';
    this.header.setAttribute('aria-label', `Session ${info.name}`);
    const wtBranch = info.worktreeBranch ?? info.worktree_branch;
    if (wtBranch) {
      this.tileWorktree.style.display = '';
      this.tileWorktree.title = `Worktree: ${wtBranch} — click to manage worktrees`;
    } else {
      this.tileWorktree.style.display = 'none';
      this.tileWorktree.title = '';
    }
    this._renderTermTitle();
  }

  _renderTermTitle() {
    // The "hide it when it echoes the session name" rule lives in
    // lib/term-title.ts because the sidebar row applies it too — the two
    // surfaces must not disagree about whether a session has anything
    // worth reporting.
    const t = displayTitle(this.termTitle, this.info.name);
    this.tileTermTitle.textContent = t;
    if (t) this.tileTermTitle.title = t;
    this.tileTermTitle.style.display = t ? '' : 'none';
  }

  // _beginRename hides the tile-name span, drops an input next to
  // it, and calls UpdateSession on Enter / blur. Escape cancels.
  // The next session:event(updated) calls setInfo which refreshes
  // tileName.textContent; we just need to put the span back in DOM.
  _beginRename() {
    if (this._renameInput) return; // already editing
    beginInlineRename({
      className: 'tile-name-input',
      value: this.info.name ?? '',
      mount: (input) => {
        // Set the reentrancy guard here (mount runs before focus/select)
        // to match the original's ordering: guard set, then focus stolen.
        this._renameInput = input;
        this.tileName.style.display = 'none';
        // `this.header` rather than `tileName.parentNode`: the span is
        // appended to the header in the constructor and never reparented,
        // so this is the same node with no nullable indirection.
        this.header.insertBefore(input, this.tileName);
      },
      // Drop the visual focus border before stealing keyboard focus —
      // setFocusedTile is the only writer of .term-focused, so without
      // this the border would linger while the rename input owns input.
      beforeFocus: () => setFocusedTile(null),
      unmount: (input) => {
        input.remove();
        this._renameInput = null;
        this.tileName.style.display = '';
      },
      onCommit: (next) =>
        UpdateSession(this.info.id, next, '', -1).catch(
          reportFailure('rename'),
        ),
      onDone: () => refocusActiveTerm(),
    });
  }

  setProject(name?: string, color?: string) {
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
    try {
      this.fit.fit();
    } catch {
      /* keep going with last-known dims */
    }
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
        id: this.info.id,
        prevCols,
        cols: this.term.cols,
        baseline: this._replayBaselineCols,
        wasAtBottom,
        // Raw geometry behind wasAtBottom: during heavy output xterm can
        // lose bottom-follow (baseY pinned at the scrollback cap while
        // viewportY drifts), so wasAtBottom reads false even though the
        // user never scrolled — the suspected arm of a spurious up-jump.
        baseY: buf?.baseY,
        viewportY: buf?.viewportY,
        bufType: buf?.type,
      });
    }
    if (
      this.attached &&
      shouldRequestReplay(this._replayBaselineCols, this.term.cols)
    ) {
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
        // Re-read follow-intent at FIRE time and re-stamp the wants-bottom
        // flag from that same read. The flag was stamped from `wasAtBottom`
        // one debounce interval ago; a scroll during that interval would
        // otherwise leave the skip decision (fresh) and the restore decision
        // (stale) disagreeing — e.g. a user who scrolled up mid-debounce gets
        // a replay AND gets yanked back to the bottom by its done handler.
        const following = this._followBottom;
        this._replayWantsBottom = following;
        const { replay, baseline } = decideResizeReplay({
          bufferType: this.term.buffer.active.type,
          cols: this.term.cols,
          baselineCols: this._replayBaselineCols,
          // A follower doesn't need history reflowed — skip the destructive
          // full-ring replay that makes the viewport thrash under live output.
          followingBottom: following,
        });
        this._replayBaselineCols = baseline;
        if (!replay) {
          delete this._replayWantsBottom;
          // Already re-latched by the earlier scrollToBottom; assert it once
          // more so a follower we skipped the replay for stays glued to the
          // newest output after the fit settles.
          if (following && typeof this.term.scrollToBottom === 'function')
            this.term.scrollToBottom();
          if (scrollTrace.rec.enabled) {
            scrollTrace.rec('replay-skip', {
              id: this.info.id,
              cols: this.term.cols,
              following,
              bufType: this.term.buffer.active.type,
            });
          }
          return;
        }
        if (scrollTrace.rec.enabled) {
          scrollTrace.rec('replay-request', {
            id: this.info.id,
            cols: this.term.cols,
          });
        }
        RequestScrollbackReplay(this.info.id).catch(() => {
          /* attach may have closed */
        });
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
  rebaselineReplayCols(_reason: string) {
    applyRebaseline(this);
  }

  async ensureAttached() {
    // A deliberate attach/focus means "show me the latest": re-latch
    // follow-intent and drop any stale restore-into-history intent from a
    // prior resize. BEFORE the attached-guard so re-focusing an ALREADY-live
    // tile (which early-returns below) also re-pins — otherwise a stale
    // _followBottom=false from an earlier scroll-up would strand it in
    // history. This is the choke point every path goes through (active tile,
    // deferred idle-callback attach, focus, restore), so setting the latch
    // here makes the bottom-snap timing-independent — see resetFollowIntent.
    resetFollowIntent(this);
    if (this.attached) {
      // Already live: no replay will re-fire to carry the latch to the
      // bottom, so snap synchronously for instant feedback on focus.
      if (typeof this.term?.scrollToBottom === 'function')
        this.term.scrollToBottom();
      return;
    }
    // Don't attach while the daemon is still creating or tearing down
    // this session: it would refuse (`session_starting`/`no_such_session`)
    // and the failure used to be painted as red text into the very pane
    // that was about to appear or vanish. Every attach path funnels
    // through here — grid render, deferred idle attach, focus, resize —
    // so this one guard covers them all. setPhase re-enters on ready.
    if (!isReady(this.phase)) {
      this._pendingAttach = true;
      return;
    }
    // Don't attempt to attach to a session known to be dead — the daemon
    // will refuse. Show the dead overlay with the error reason instead.
    if (state.aliveById.get(this.info.id) === false) {
      this.setDead(
        true,
        this.info.last_error || 'The process failed to start.',
      );
      return;
    }
    // If the host is still display:none, the body has no box yet and
    // fit.fit() would measure 0×0. Defer until ResizeObserver fires
    // with a real size — _onBodyResize will re-enter ensureAttached.
    if (this.body.clientWidth === 0 || this.body.clientHeight === 0) {
      this._pendingAttach = true;
      return;
    }
    const _fitStart = nowMs();
    this.fit.fit();
    const _fitMs = nowMs() - _fitStart;
    // _followBottom was already re-latched at the top of ensureAttached
    // (resetFollowIntent), so the initial attach replay snaps to bottom.
    try {
      const _openStart = nowMs();
      await OpenSession(this.info.id, this.term.cols, this.term.rows);
      // Startup-latency probe: fit.fit() is synchronous DOM measurement
      // (blocks the main thread); OpenSession awaits the Go dial+handshake.
      // On a many-tile grid launch these run per tile — this line pins
      // which half of each attach is slow.
      feLog(
        `ensureAttached id=${this.info.id} fit=${Math.round(_fitMs)}ms open=${Math.round(nowMs() - _openStart)}ms`,
      );
      this.attached = true;
      // Anchor the replay baseline to the actual fitted cols for this
      // tile. Without this, a later _onBodyResize would initialize the
      // baseline from a stale xterm default (80) while term.cols is the
      // real grid-cell width — the next resize crosses the threshold
      // and fires a spurious scrollback replay on first grid entry.
      this.rebaselineReplayCols('first-attach');
    } catch (err) {
      // A session that started closing (or restarting) while the dial
      // was in flight is *expected* to refuse. Only a genuine failure
      // on a ready session is worth painting into the pane.
      if (isReady(this.phase)) {
        this.term.write(`\r\n\x1b[31m[attach failed: ${err}]\x1b[0m\r\n`);
      }
    }
  }

  writeData(b64: string) {
    const bin = atob(b64);
    const bytes = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
    // Startup-flood probe: xterm.write parses on the main thread, so a
    // large initial scrollback replay blocks it. Sum bytes/writes for the
    // first ~2s after this tile's first byte and log once — pairs with the
    // Go-side "initial burst" line to show flood → main-thread stall.
    this._wroteBytes = (this._wroteBytes || 0) + bin.length;
    this._wroteCount = (this._wroteCount || 0) + 1;
    if (this._writeWindowStart === undefined) this._writeWindowStart = nowMs();
    if (!this._writeBurstLogged && nowMs() - this._writeWindowStart > 2000) {
      this._writeBurstLogged = true;
      feLog(
        `writeData burst id=${this.info.id} writes=${this._wroteCount} bytes=${this._wroteBytes} in ${Math.round(nowMs() - this._writeWindowStart)}ms`,
      );
    }
    this.term.write(this.decoder.decode(bytes, { stream: true }));
  }

  destroy() {
    // Intentionally silent: destroy() tears down a session that's already
    // gone; a failed CloseAttach has nothing for the user to act on.
    CloseAttach(this.info.id).catch(() => {});
    if (this._revealRaf) cancelAnimationFrame(this._revealRaf);
    if (this._phaseRevealTimer) clearTimeout(this._phaseRevealTimer);
    this.ro.disconnect();
    if (this._dprWatcher) {
      try {
        this._dprWatcher.teardown();
      } catch {}
      this._dprWatcher = null;
    }
    if (this._onVisibility) {
      document.removeEventListener('visibilitychange', this._onVisibility);
    }
    if (this._onWindowMouseUp) {
      window.removeEventListener('mouseup', this._onWindowMouseUp, true);
      this._onWindowMouseUp = null;
    }
    // Release the GL context proactively so a many-tile session doesn't
    // sit on it until GC and push another tile over the browser cap.
    try {
      this.webgl?.dispose();
    } catch {}
    if (this._hasWebglSlot) {
      releaseWebglSlot();
      this._hasWebglSlot = false;
    }
    this.webgl = null;
    this.term.dispose();
    this.host.remove();
  }

  setDead(isDead: boolean, reason?: string) {
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
      //
      // Never while a modal is open. A session can die at any moment —
      // the daemon drives this, not the user — and stealing focus out
      // of a modal mid-keystroke is hostile in every case: it drops
      // what you were typing into the project editor or the command
      // palette. For the launcher it is worse than that, because the
      // launcher closes when focus leaves it, so an unrelated session
      // exiting would make the popup and its query vanish outright.
      setTimeout(() => {
        if (this.deadOverlayShown && !anyModalOpen()) this.deadCloseBtn.focus();
      }, 0);
    }
  }

  /**
   * Apply a lifecycle phase from the daemon.
   *
   * Two edges matter. Entering a transient phase raises the loading
   * panel (and the attach gate in ensureAttached keeps the terminal
   * out of it). Reaching ready has to *drive* the attach: nothing else
   * would — _pendingAttach is only re-entered by the ResizeObserver,
   * and a phase change fires no resize.
   */
  setPhase(phase: string) {
    const prev = this.phase;
    this.phase = phase;
    this.host.classList.toggle('closing', isClosing(phase));
    if (!isReady(phase)) {
      this._showPhaseOverlay();
      return;
    }
    if (isReady(prev)) return; // no edge
    if (state.aliveById.get(this.info.id) === false) {
      // Ready but dead: the spawn failed. There is no terminal coming,
      // so drop the panel at once and let the dead overlay (which
      // events.ts raises on this same edge) own the tile — otherwise
      // the spinner would sit on top of it until the fallback timer.
      this._hidePhaseOverlay();
      return;
    }
    // Ready: attach now (we refused to while pending), and hold the
    // panel until the terminal has something to show.
    void this.ensureAttached();
    this._revealAfterPhase();
  }

  _showPhaseOverlay() {
    const panel = phasePanel({
      phase: this.phase,
      agent: this.info.agent,
      worktreeBranch: this.info.worktreeBranch ?? this.info.worktree_branch,
    });
    if (!panel) {
      this._hidePhaseOverlay();
      return;
    }
    if (this._phaseRevealTimer) {
      clearTimeout(this._phaseRevealTimer);
      this._phaseRevealTimer = 0;
    }
    this.phaseStatus.textContent = panel.status;
    this.phaseSteps.replaceChildren(
      ...panel.steps.map((step) => {
        const li = document.createElement('li');
        li.className = `phase-step ${step.state}`;
        li.textContent = step.label;
        return li;
      }),
    );
    this.phaseOverlayShown = true;
    this.phaseOverlay.hidden = false;
    this.phaseOverlay.classList.remove('fading');
  }

  _hidePhaseOverlay() {
    if (this._phaseRevealTimer) {
      clearTimeout(this._phaseRevealTimer);
      this._phaseRevealTimer = 0;
    }
    if (!this.phaseOverlayShown) return;
    this.phaseOverlayShown = false;
    this.phaseOverlay.hidden = true;
    this.phaseOverlay.classList.remove('fading');
  }

  /**
   * Hold the panel past PhaseReady until the terminal has painted, so
   * the reveal lands on a settled screen.
   *
   * The signal is the daemon's scrollback_replay_done — attaching
   * always replays the buffer, and "replay finished" is exactly "the
   * screen is as settled as it is going to get". Waiting for the PTY
   * to go quiet instead would never fire for an agent TUI, which
   * animates continuously. The timer is the fallback for a tile that
   * never replays (hidden/minimized, so never attached).
   */
  _revealAfterPhase() {
    if (!this.phaseOverlayShown) return;
    if (this._phaseRevealTimer) clearTimeout(this._phaseRevealTimer);
    this._phaseRevealTimer = window.setTimeout(() => {
      this._phaseRevealTimer = 0;
      this.revealAfterReplay();
    }, PHASE_REVEAL_CAP_MS);
  }

  /**
   * Drop the loading panel now that the terminal has content. Called
   * on scrollback_replay_done and by the cap timer above; a no-op
   * while the session is still in a transient phase, so a replay
   * arriving mid-restart can't unveil a session that isn't back yet.
   */
  revealAfterReplay() {
    if (!this.phaseOverlayShown || !isReady(this.phase)) return;
    this.phaseOverlay.classList.add('fading');
    this._hidePhaseOverlay();
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
    // Guarded rather than `st.term.options.fontSize = …`: TermTile's `term`
    // is optional because the DOM-test stubs omit it. Every real tile has
    // one, so this is the same write on every path that matters.
    const opts = st.term?.options;
    if (opts) opts.fontSize = state.fontSize;
    // Body box doesn't change on font-size change, so ResizeObserver
    // won't fire — call the resize handler explicitly so fit.fit()
    // recomputes (cols, rows) from new char metrics.
    st._onBodyResize();
  }
  localStorage.setItem('hive.fontSize', String(state.fontSize));
}

export function bumpFontSize(delta: number) {
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

export function ensureTerm(info: SessionInfo) {
  let st = state.terms.get(info.id);
  if (!st) {
    st = new SessionTerm(info);
    state.terms.set(info.id, st);
  } else {
    st.setInfo(info);
  }
  // Seed the phase from the info we were handed. A tile created for a
  // session that is still starting (the common case now — `added`
  // fires before the PTY exists) must come up showing the loading
  // panel, with its attach gated, not attach immediately and get
  // refused.
  st.setPhase(phaseOf(info));
  const proj = state.projects.find(
    (p) => p.id === (info.projectId ?? info.project_id),
  );
  st.setProject(proj?.name ?? '', proj?.color ?? '');
  return st;
}
