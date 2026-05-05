import '@xterm/xterm/css/xterm.css';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { WebglAddon } from '@xterm/addon-webgl';
import { WebLinksAddon } from '@xterm/addon-web-links';

import {
  ConnectControl, OpenSession, CloseAttach,
  WriteStdin, ResizeSession,
  CreateSession, KillSession, UpdateSession, ListAgents,
  CreateProject, KillProject, UpdateProject,
  LaunchDir, PickDirectory, OpenNewWindow, CloseWindow,
  IsGitRepo, OpenURL, Notify, Confirm,
  RestartDaemon,
} from '../wailsjs/go/main/App';
import { EventsOn, WindowSetTitle } from '../wailsjs/runtime/runtime';

// ---------- session terminal ----------

class SessionTerm {
  constructor(info) {
    this.info = info;
    this.host = document.createElement('div');
    this.host.className = 'term-host';
    this.host.dataset.sid = info.id;
    this.host.style.setProperty('--session-color', info.color || '#888');

    // Tile header (only visible in grid mode via CSS).
    this.header = document.createElement('div');
    this.header.className = 'tile-header';
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
    this.header.append(this.tileColor, this.tileName, this.tileWorktree, this.tileTermTitle, this.tileProject);

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
        activate: (_e, uri) => { if (uri) OpenURL(uri); },
      },
    });
    this.fit = new FitAddon();
    this.term.loadAddon(this.fit);
    this.term.open(this.body);

    // WebGL renderer is dramatically faster than the default DOM
    // renderer on older machines (VS Code uses the same approach).
    // Load it lazily after open() and silently fall back to DOM if
    // the GPU / driver doesn't support it.
    try {
      const webgl = new WebglAddon();
      webgl.onContextLoss(() => webgl.dispose());
      this.term.loadAddon(webgl);
      this.webgl = webgl;
    } catch (err) {
      // GPU lacks WebGL2 — keep DOM renderer. No user-visible message.
    }

    // Detect URLs in terminal output and route activation through
    // the OS default browser. Hover underlines the URL; click (or
    // ⌘-click when mouse reporting is active) follows it.
    try {
      this.term.loadAddon(new WebLinksAddon((event, uri) => {
        if (uri) OpenURL(uri);
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
        if (buf.type !== 'normal') return;
        if (this.term.modes && this.term.modes.mouseTrackingMode && this.term.modes.mouseTrackingMode !== 'none') return;
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
    // Drive the visual focus border off the xterm's real focus state
    // — not state.activeId — so the border can never claim "I'm
    // focused" while keystrokes go elsewhere. xterm.js v5 has no
    // onFocus/onBlur events, so we listen on the host: focus events
    // bubble from xterm's hidden textarea (.xterm-helper-textarea)
    // through .term-host.
    // On gain: sweep the class off every other host first, so only
    // one tile is ever marked focused. xterm's open() / fit / mount
    // sequence can fire focusin on multiple tiles in quick succession
    // during initial render; without the sweep, several end up
    // visually claiming focus simultaneously.
    this.host.addEventListener('focusin', () => {
      for (const el of document.querySelectorAll('.term-host.term-focused')) {
        if (el !== this.host) el.classList.remove('term-focused');
      }
      this.host.classList.add('term-focused');
    });
    // On loss: only drop the class if focus actually left this host.
    // Internal xterm focus juggling can briefly fire focusout with
    // relatedTarget still inside the host — ignore those.
    this.host.addEventListener('focusout', (e) => {
      if (e.relatedTarget && this.host.contains(e.relatedTarget)) return;
      this.host.classList.remove('term-focused');
    });

    this.attached = false;
    this.phase = 'replay';

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
      WriteStdin(this.info.id, btoa(bin));
    };
    this.term.onData((data) => this._writePty(data));

    // macOS Cmd+Backspace → Ctrl+U (kill to start of line). Browser doesn't
    // translate this for us when xterm's helper-textarea is focused.
    this.term.attachCustomKeyEventHandler((e) => {
      if (e.type !== 'keydown') return true;
      if (e.metaKey && !e.ctrlKey && !e.altKey && e.key === 'Backspace') {
        e.preventDefault();
        this._writePty('\x15');
        return false;
      }
      return true;
    });

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
    // from firing.
    const linesPerPixel = 1 / 14; // ~one line per ~14 px of delta
    const maxLinesPerEvent = 8;   // about half a screen on a small tile
    this.host.addEventListener('wheel', (e) => {
      e.preventDefault();
      e.stopPropagation();
      let lines = Math.round(e.deltaY * linesPerPixel);
      if (lines === 0) {
        // Sub-pixel events — preserve direction so a slow scroll
        // still moves at least one line.
        lines = e.deltaY > 0 ? 1 : e.deltaY < 0 ? -1 : 0;
      }
      if (lines > maxLinesPerEvent) lines = maxLinesPerEvent;
      if (lines < -maxLinesPerEvent) lines = -maxLinesPerEvent;
      if (lines !== 0) this.term.scrollLines(lines);
    }, { capture: true, passive: false });
  }

  setInfo(info) {
    this.info = info;
    this.host.style.setProperty('--session-color', info.color || '#888');
    this.tileName.textContent = info.name;
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
        UpdateSession(this.info.id, next, '', -1);
      }
      refocusActiveTerm();
    };
    input.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') { e.stopPropagation(); finish(true); }
      else if (e.key === 'Escape') { e.stopPropagation(); finish(false); }
    });
    input.addEventListener('blur', () => finish(true));
    // Stop xterm / global hotkey handlers from grabbing keystrokes.
    input.addEventListener('keydown', (e) => e.stopPropagation(), { capture: true });
  }

  setProject(name, color) {
    this.tileProject.textContent = name || '';
    this.host.style.setProperty('--project-color', color || '#888');
  }

  show() {
    this.host.classList.add('visible');
    this.refit();
  }

  hide() {
    this.host.classList.remove('visible');
  }

  refit() {
    try { this.fit.fit(); } catch {}
    if (this.attached) {
      ResizeSession(this.info.id, this.term.cols, this.term.rows);
    }
  }

  async ensureAttached() {
    if (this.attached) return;
    // Don't attempt to attach to a session known to be dead — the daemon
    // will refuse. Show the dead overlay with the error reason instead.
    if (state.aliveById.get(this.info.id) === false) {
      this.setDead(true, this.info.last_error || 'The process failed to start.');
      return;
    }
    // Wait one frame so the .visible / .in-grid classes that show()
    // just toggled have actually flowed through layout. fit.fit() reads
    // offsetWidth/Height; if we measure before layout settles we end up
    // sending the daemon stale (often default 80x24) dimensions, the
    // daemon's WELCOME reports the same defaults, OpenSession's "did
    // size change?" check skips the resize, and the running PTY app
    // never SIGWINCHes — appearing static until the user toggles modes.
    await new Promise((r) => requestAnimationFrame(r));
    this.fit.fit();
    try {
      await OpenSession(this.info.id, this.term.cols, this.term.rows);
      this.attached = true;
    } catch (err) {
      this.term.write(`\r\n\x1b[31m[attach failed: ${err}]\x1b[0m\r\n`);
    }
  }

  writeData(b64) {
    const bin = atob(b64);
    const bytes = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
    this.term.write(decoder.decode(bytes, { stream: true }));
  }

  destroy() {
    CloseAttach(this.info.id).catch(() => {});
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
    KillSession(this.info.id, true).catch((err) => {
      setStatus(`close failed: ${err}`, true);
    });
  }

  _dismissDead() {
    state.dismissedDead.add(this.info.id);
    this.setDead(false);
    refocusActiveTerm();
  }
}

const decoder = new TextDecoder('utf-8', { fatal: false });

// ---------- app state ----------

const DEFAULT_FONT_SIZE = 14;
const MIN_FONT_SIZE = 8;
const MAX_FONT_SIZE = 32;

const state = {
  projects: [],             // ProjectInfo[] in display order
  sessions: [],             // SessionInfo[] in display order
  collapsed: new Set(),     // project ids that are collapsed
  attention: new Set(),     // session ids that have unread bells
  aliveById: new Map(),     // session id -> last-seen Alive bool (for transition detection)
  dismissedDead: new Set(), // session ids whose dead overlay user dismissed
  terms: new Map(),         // session id -> SessionTerm
  activeId: null,
  currentProjectId: null,   // "the project I'm working in"; can be set
                            //   without a focused session (so empty
                            //   projects are reachable / launchable)
  view: 'single',           // 'single' | 'grid-project' | 'grid-all'
  gridProjectId: null,      // project shown in grid-project mode
  fontSize: clampFont(parseInt(localStorage.getItem('hive.fontSize') ?? '', 10) || DEFAULT_FONT_SIZE),
};

function clampFont(n) {
  return Math.max(MIN_FONT_SIZE, Math.min(MAX_FONT_SIZE, n));
}

// ---------- bell + attention ----------

// onSessionBell is fired by SessionTerm whenever its xterm receives
// BEL. Active + window-focused session: ignore. Otherwise: mark
// attention, repaint sidebar, and fire a desktop notification — but
// only on the transition from no-attention → attention, so a session
// emitting bells in a tight loop doesn't spam the OS notification
// center.
function onSessionBell(info) {
  const isActive = info.id === state.activeId;
  const windowFocused = document.hasFocus();
  if (isActive && windowFocused) return;
  const alreadyAttention = state.attention.has(info.id);
  if (alreadyAttention) {
    // Refresh to re-trigger CSS animation.
    state.attention.delete(info.id);
    state.terms.get(info.id)?.host.classList.remove('attention');
  }
  state.attention.add(info.id);
  state.terms.get(info.id)?.host.classList.add('attention');
  updateSidebarSelection();
  if (!alreadyAttention) fireBellNotification(info);
}

function clearAttention(sessionId) {
  if (state.attention.delete(sessionId)) {
    state.terms.get(sessionId)?.host.classList.remove('attention');
    updateSidebarSelection();
  }
}

// Whenever the window regains focus, clear the active session's
// attention state — the user is presumably looking at it. Also
// restore xterm focus: macOS fullscreen toggles, ⌘-tab returns, and
// menu actions can leave the window focused but no element inside it,
// so typing would land on the body and be lost.
window.addEventListener('focus', () => {
  if (state.activeId) clearAttention(state.activeId);
  refocusActiveTerm();
});

// fireBellNotification routes through Go because Wails' WKWebView on
// macOS doesn't implement the HTML5 Notification API. The Go side
// dispatches per-platform (NSUserNotification / notify-send / Windows
// toast). The session id is passed as the tag so the OS can dedupe
// repeated bells from the same session and the click handler knows
// which session to switch to.
function fireBellNotification(info) {
  const proj = state.projects.find((p) => p.id === (info.projectId ?? info.project_id));
  const projectName = proj?.name ?? '';
  const title = info.name || 'Session';
  const subtitle = projectName;
  const body = 'Waiting for input — click to switch.';
  Notify(title, subtitle, body, info.id).catch(() => {
    // Best-effort; the visual sidebar pulse covers the user even if
    // the OS notification fails (no notify-send installed, etc.).
  });
}

// onSessionDeath fires once when a session transitions Alive→dead.
// Shows the in-tile overlay, marks attention, and posts a desktop
// notification distinct from a normal bell.
function onSessionDeath(info) {
  state.dismissedDead.delete(info.id);
  const t = state.terms.get(info.id);
  if (t) {
    // Flip attached eagerly so a switch-back before pty:disconnect arrives
    // doesn't try to reuse the dying connection.
    t.attached = false;
    t.setDead(true, info.last_error || 'The process running in this session has exited.');
  }
  // Reuse the attention pulse path so the sidebar entry highlights.
  state.attention.add(info.id);
  state.terms.get(info.id)?.host.classList.add('attention');
  updateSidebarSelection();
  const proj = state.projects.find((p) => p.id === (info.projectId ?? info.project_id));
  Notify(info.name || 'Session', proj?.name ?? '', 'Session ended.', info.id).catch(() => {});
}

function applyFontSize() {
  for (const st of state.terms.values()) {
    st.term.options.fontSize = state.fontSize;
    st.refit();
  }
  localStorage.setItem('hive.fontSize', String(state.fontSize));
}

function bumpFontSize(delta) {
  const next = clampFont(state.fontSize + delta);
  if (next === state.fontSize) return;
  state.fontSize = next;
  applyFontSize();
  setStatus(`font ${state.fontSize}px`);
}

function resetFontSize() {
  state.fontSize = DEFAULT_FONT_SIZE;
  applyFontSize();
  setStatus(`font ${state.fontSize}px`);
}

const termsHost = document.getElementById('terms');
termsHost.classList.add('single');

const projectsUL = document.getElementById('projects');
const status = document.getElementById('status');

function setStatus(text, isError = false) {
  status.textContent = text;
  status.classList.toggle('error', isError);
}

// orderedSessions returns sessions sorted by (project order, session order)
// so navigation always matches what the user sees.
function orderedSessions() {
  const projOrder = new Map(state.projects.map((p, i) => [p.id, i]));
  return [...state.sessions].sort((a, b) => {
    const pa = projOrder.get(a.projectId ?? a.project_id ?? '') ?? 1e9;
    const pb = projOrder.get(b.projectId ?? b.project_id ?? '') ?? 1e9;
    if (pa !== pb) return pa - pb;
    return (a.order ?? 0) - (b.order ?? 0);
  });
}

function activeProjectId() {
  // currentProjectId is the user's explicit "I'm here" — set by
  // ⌘[/], project-header click, switchTo (synced to session's
  // project), and project events. Empty projects work because they
  // can be the current project even with no active session.
  if (state.currentProjectId) {
    return state.currentProjectId;
  }
  if (state.view === 'grid-project' && state.gridProjectId) {
    return state.gridProjectId;
  }
  if (state.activeId) {
    const s = state.sessions.find((x) => x.id === state.activeId);
    const pid = s?.projectId ?? s?.project_id;
    if (pid) return pid;
  }
  return state.projects[0]?.id ?? '';
}

// ---------- sidebar render ----------

function renderSidebar() {
  projectsUL.innerHTML = '';
  const activePID = activeProjectId();
  for (const p of state.projects) {
    projectsUL.appendChild(renderProject(p, activePID));
  }
}

// updateSidebarSelection toggles the .selected / .active /
// .attention classes on existing DOM nodes without rebuilding them.
// Selection-only or attention-only changes call this instead of
// renderSidebar so consecutive clicks on a session-item still match
// up as a dblclick pair (the rebuild between clicks was eating the
// dblclick because the LI was a different node by the second click).
function updateSidebarSelection() {
  const activePID = activeProjectId();
  for (const el of projectsUL.querySelectorAll('.project')) {
    el.classList.toggle('active', el.dataset.pid === activePID);
  }
  for (const el of projectsUL.querySelectorAll('.session-item')) {
    const sid = el.dataset.sid;
    el.classList.toggle('selected', sid === state.activeId);
    el.classList.toggle('attention', state.attention.has(sid));
  }
}

function renderProject(p, activePID) {
  const li = document.createElement('li');
  li.className = 'project';
  li.dataset.pid = p.id;
  if (state.collapsed.has(p.id)) li.classList.add('collapsed');
  if (p.id === activePID) li.classList.add('active');
  li.style.setProperty('--project-color', p.color || '#888');
  li.draggable = true;

  const header = document.createElement('div');
  header.className = 'project-header';

  const caret = document.createElement('span');
  caret.className = 'caret';
  caret.textContent = '▾';
  caret.addEventListener('click', (e) => {
    e.stopPropagation();
    if (state.collapsed.has(p.id)) state.collapsed.delete(p.id);
    else state.collapsed.add(p.id);
    renderSidebar();
  });

  const colorEl = document.createElement('span');
  colorEl.className = 'project-color';

  const name = document.createElement('span');
  name.className = 'project-name';
  name.textContent = p.name;
  name.title = p.cwd ? `${p.name} — ${p.cwd}` : p.name;

  const actions = document.createElement('span');
  actions.className = 'project-actions';

  const newBtn = document.createElement('button');
  newBtn.textContent = '+';
  newBtn.title = 'New session in this project';
  newBtn.addEventListener('click', (e) => {
    e.stopPropagation();
    openLauncher(p.id);
  });

  const editBtn = document.createElement('button');
  editBtn.textContent = '✎';
  editBtn.title = 'Edit project';
  editBtn.addEventListener('click', (e) => {
    e.stopPropagation();
    openProjectEditor(p);
  });

  const delBtn = document.createElement('button');
  delBtn.textContent = '✕';
  delBtn.title = 'Delete project';
  delBtn.addEventListener('click', (e) => {
    e.stopPropagation();
    confirmAndDeleteProject(p);
  });

  actions.append(newBtn, editBtn, delBtn);

  header.append(caret, colorEl, name, actions);
  header.addEventListener('click', (e) => {
    // Only fire when clicking the row background, color block, or name —
    // not on buttons / caret / inline inputs. Each of those stops
    // propagation in its own handler so we shouldn't see them here,
    // but be defensive.
    if (e.target.closest('.project-actions') || e.target === caret) return;
    switchToProject(p.id);
  });
  header.addEventListener('dblclick', (e) => {
    if (e.target === name || e.target === header) beginRenameProject(p, name);
  });
  li.appendChild(header);

  const ul = document.createElement('ul');
  ul.className = 'project-sessions';
  const sessions = state.sessions
    .filter((s) => (s.projectId ?? s.project_id) === p.id)
    .sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
  for (const s of sessions) {
    ul.appendChild(renderSession(s, p.color || '#888'));
  }
  li.appendChild(ul);

  // ---- Drag-to-reorder for projects ----
  // dragstart bubbles, so a session-item drag fires here too after
  // its own handler runs. We must not preventDefault in that case
  // (it would cancel the session drag). For drags that originate on
  // the project chrome (action buttons, rename input) we DO want to
  // abort, since the li itself is the closest draggable.
  li.addEventListener('dragstart', (e) => {
    if (e.target.closest('.session-item')) {
      // Bubbled from an inner session drag — leave it alone.
      return;
    }
    if (e.target.closest('.project-actions') ||
        e.target.closest('.project-name-input')) {
      e.preventDefault();
      return;
    }
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/x-hive-project', p.id);
    li.classList.add('dragging');
  });
  li.addEventListener('dragend', () => {
    li.classList.remove('dragging');
    document.querySelectorAll('.project.drop-above, .project.drop-below')
      .forEach((el) => el.classList.remove('drop-above', 'drop-below'));
  });
  li.addEventListener('dragover', (e) => {
    if (!e.dataTransfer.types.includes('text/x-hive-project')) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';
    // Use the header's bounds (not the whole li): with sessions
    // expanded, the li is tall, the cursor is almost always above
    // its midpoint, and the indicator would land far from the
    // cursor. Anchoring both the hit-test and the visual to the
    // header keeps them in sync.
    const r = header.getBoundingClientRect();
    const above = (e.clientY - r.top) < r.height / 2;
    li.classList.toggle('drop-above', above);
    li.classList.toggle('drop-below', !above);
  });
  li.addEventListener('dragleave', (e) => {
    // Only clear when leaving the li entirely; dragover into a child
    // re-fires and re-asserts the right class.
    if (!li.contains(e.relatedTarget)) {
      li.classList.remove('drop-above', 'drop-below');
    }
  });
  li.addEventListener('drop', (e) => {
    if (!e.dataTransfer.types.includes('text/x-hive-project')) return;
    e.preventDefault();
    const pid = e.dataTransfer.getData('text/x-hive-project');
    li.classList.remove('drop-above', 'drop-below');
    if (!pid || pid === p.id) return;
    const r = header.getBoundingClientRect();
    const above = (e.clientY - r.top) < r.height / 2;
    reorderDroppedProject(pid, p.id, above);
  });
  return li;
}

// reorderDroppedProject converts an above/below drop into the new
// Order index expected by UpdateProject. The daemon's moveProjectLocked
// removes the dragged project then inserts at newOrder, so we
// compensate when the source sits before the target.
function reorderDroppedProject(draggedID, targetID, above) {
  const ordered = [...state.projects].sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
  const targetIdx = ordered.findIndex((p) => p.id === targetID);
  const draggedIdx = ordered.findIndex((p) => p.id === draggedID);
  if (targetIdx < 0 || draggedIdx < 0) return;
  let newOrder = above ? targetIdx : targetIdx + 1;
  if (draggedIdx < newOrder) newOrder -= 1;
  if (newOrder === draggedIdx) return;
  UpdateProject(draggedID, '', '', '', newOrder);
}

function renderSession(s, projectColor) {
  const li = document.createElement('li');
  li.className = 'session-item';
  if (s.id === state.activeId) li.classList.add('selected');
  if (!s.alive) li.classList.add('dead');
  if (state.attention.has(s.id)) li.classList.add('attention');
  li.style.setProperty('--session-color', s.color || '#888');
  li.style.setProperty('--project-color', projectColor || '#888');
  li.dataset.sid = s.id;
  li.dataset.pid = s.projectId ?? s.project_id ?? '';
  li.draggable = true;

  const dot = document.createElement('span');
  dot.className = 'dot';

  const name = document.createElement('span');
  name.className = 'name';
  name.textContent = s.name;

  // Worktree glyph: shown when the session is backed by a git
  // worktree. Tooltip = branch name.
  const wtBranch = s.worktreeBranch ?? s.worktree_branch;
  let glyph = null;
  if (wtBranch) {
    glyph = document.createElement('span');
    glyph.className = 'worktree-glyph';
    glyph.textContent = '⎇';
    glyph.title = `Worktree: ${wtBranch}`;
  }

  const swatch = document.createElement('span');
  swatch.className = 'swatch';
  const colorInput = document.createElement('input');
  colorInput.type = 'color';
  colorInput.value = s.color || '#888888';
  colorInput.addEventListener('input', (e) => {
    UpdateSession(s.id, '', e.target.value, -1);
  });
  swatch.appendChild(colorInput);

  if (glyph) {
    li.append(dot, name, glyph, swatch);
  } else {
    li.append(dot, name, swatch);
  }
  li.addEventListener('click', (e) => {
    if (e.target === colorInput || e.target === swatch) return;
    switchTo(s.id);
  });
  li.addEventListener('dblclick', () => beginRenameSession(s, li, name));

  // ---- Drag-to-reorder ----
  // Same-project drops only; cross-project moves are not supported
  // yet (would require also updating project_id on the wire).
  li.addEventListener('dragstart', (e) => {
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/x-hive-session', s.id);
    li.classList.add('dragging');
  });
  li.addEventListener('dragend', () => {
    li.classList.remove('dragging');
    document.querySelectorAll('.session-item.drop-above, .session-item.drop-below')
      .forEach((el) => el.classList.remove('drop-above', 'drop-below'));
  });
  li.addEventListener('dragover', (e) => {
    if (!e.dataTransfer.types.includes('text/x-hive-session')) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';
    const r = li.getBoundingClientRect();
    const above = (e.clientY - r.top) < r.height / 2;
    li.classList.toggle('drop-above', above);
    li.classList.toggle('drop-below', !above);
  });
  li.addEventListener('dragleave', () => {
    li.classList.remove('drop-above', 'drop-below');
  });
  li.addEventListener('drop', (e) => {
    e.preventDefault();
    const sid = e.dataTransfer.getData('text/x-hive-session');
    li.classList.remove('drop-above', 'drop-below');
    if (!sid || sid === s.id) return;
    const dragged = state.sessions.find((x) => x.id === sid);
    if (!dragged) return;
    const draggedPID = dragged.projectId ?? dragged.project_id ?? '';
    const targetPID = s.projectId ?? s.project_id ?? '';
    if (draggedPID !== targetPID) return; // cross-project: not supported yet
    const r = li.getBoundingClientRect();
    const above = (e.clientY - r.top) < r.height / 2;
    reorderDroppedSession(sid, s.id, above);
  });
  return li;
}

// reorderDroppedSession converts a drop position ("above" or "below"
// the target row) into a global Order argument for UpdateSession.
// The daemon's moveLocked treats the argument as a global index into
// r.order; we pick the global Order of whichever neighbor sits at
// the project-relative drop slot (after pretending the dragged
// session is gone).
function reorderDroppedSession(draggedID, targetID, above) {
  const target = state.sessions.find((s) => s.id === targetID);
  if (!target) return;
  const projID = target.projectId ?? target.project_id ?? '';
  const projSessions = state.sessions
    .filter((s) => (s.projectId ?? s.project_id ?? '') === projID)
    .sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
  const targetIdx = projSessions.findIndex((s) => s.id === targetID);
  if (targetIdx < 0) return;
  let projIdx = above ? targetIdx : targetIdx + 1;
  const pretend = projSessions.filter((s) => s.id !== draggedID);
  if (pretend.length === 0) return;
  if (projIdx > pretend.length) projIdx = pretend.length;

  // Find the global index in r.order that we want the dragged session
  // to land at. We approximate using global Order values: pretend[i]
  // currently has some Order value, and moveLocked accepts a global
  // index. Easiest: walk the global ordered list of all sessions and
  // count to the slot we want.
  const globalOrdered = [...state.sessions].sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
  let globalTargetIdx;
  if (projIdx >= pretend.length) {
    // Drop after the last neighbor: land just past it.
    const last = pretend[pretend.length - 1];
    globalTargetIdx = globalOrdered.findIndex((x) => x.id === last.id) + 1;
  } else {
    const neighbor = pretend[projIdx];
    globalTargetIdx = globalOrdered.findIndex((x) => x.id === neighbor.id);
  }
  if (globalTargetIdx < 0) return;
  // moveLocked is "remove from current pos, then insert at newOrder"
  // — so if dragged is currently *before* the target index, the
  // index shifts by 1 after removal. Compensate.
  const draggedGlobalIdx = globalOrdered.findIndex((x) => x.id === draggedID);
  if (draggedGlobalIdx >= 0 && draggedGlobalIdx < globalTargetIdx) {
    globalTargetIdx -= 1;
  }
  UpdateSession(draggedID, '', '', globalTargetIdx);
}

function beginRenameSession(sess, li, nameEl) {
  const input = document.createElement('input');
  input.type = 'text';
  input.className = 'name-input';
  input.value = sess.name;
  nameEl.replaceWith(input);
  input.focus();
  input.select();
  const finish = (commit) => {
    if (commit && input.value.trim() && input.value !== sess.name) {
      UpdateSession(sess.id, input.value.trim(), '', -1);
    } else {
      renderSidebar();
    }
    refocusActiveTerm();
  };
  input.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') finish(true);
    else if (e.key === 'Escape') finish(false);
  });
  input.addEventListener('blur', () => finish(true));
}

function beginRenameProject(proj, nameEl) {
  const input = document.createElement('input');
  input.type = 'text';
  input.className = 'project-name-input';
  input.value = proj.name;
  nameEl.replaceWith(input);
  input.focus();
  input.select();
  const finish = (commit) => {
    if (commit && input.value.trim() && input.value !== proj.name) {
      UpdateProject(proj.id, input.value.trim(), '', '', -1);
    } else {
      renderSidebar();
    }
    refocusActiveTerm();
  };
  input.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') finish(true);
    else if (e.key === 'Escape') finish(false);
  });
  input.addEventListener('blur', () => finish(true));
}

function ensureTerm(info) {
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

function showSingle(id) {
  termsHost.classList.add('single');
  termsHost.classList.remove('grid');
  // Hide everything except the active tile.
  for (const [sid, st] of state.terms) {
    if (sid === id) st.show();
    else st.hide();
    st.host.classList.remove('in-grid', 'active');
  }
  const st = id ? state.terms.get(id) : null;
  if (st) st.ensureAttached();
}

function switchTo(id) {
  if (id === state.activeId && state.view === 'single') {
    focusActiveTerm();
    return;
  }
  setActive(id);
  let info = null;
  if (id) {
    info = state.sessions.find((s) => s.id === id);
    if (info) ensureTerm(info);
  }
  // Retarget the grid scope if the new session belongs to a different
  // project than the one currently shown in grid-project mode.
  if (state.view === 'grid-project' && info) {
    const pid = info.projectId ?? info.project_id;
    if (pid && pid !== state.gridProjectId) state.gridProjectId = pid;
  }
  if (state.view === 'single') showSingle(id);
  else renderGrid();
  updateSidebarSelection();
  setStatus(info ? info.name : '');
  updateAppTitle();
  // setActive() called focusActiveTerm() before ensureTerm() existed
  // for a brand-new session — re-focus now that the SessionTerm is
  // created and visible. Without this, typing after creating a
  // session lands in whichever terminal had focus before.
  if (id) focusActiveTerm();
}

// updateAppTitle composes "Hive — <session> — <termTitle>" and pushes
// it to both document.title and the native window title bar. The
// termTitle slot is whatever the running TUI most recently set via
// the OSC 0/2 escape sequence; empty if the program never set one.
//
// Throttled with a trailing-edge timer: programs like fish prompts
// or progress encoders can fire OSC 0/2 dozens of times per second,
// and each WindowSetTitle is a Wails IPC round-trip. 100ms keeps the
// title visibly responsive without flooding the bridge.
let _appTitleTimer = null;
function updateAppTitle() {
  if (_appTitleTimer) return;
  _appTitleTimer = setTimeout(() => {
    _appTitleTimer = null;
    const id = state.activeId;
    const info = id ? state.sessions.find((s) => s.id === id) : null;
    const parts = ['Hive'];
    if (info?.name) parts.push(info.name);
    const t = id ? state.terms.get(id) : null;
    if (t?.termTitle && t.termTitle !== info?.name) parts.push(t.termTitle);
    const title = parts.join(' — ');
    document.title = title;
    try { WindowSetTitle(title); } catch (_) { /* runtime not ready */ }
  }, 100);
}

// switchToProject activates a project: in grid-project mode it
// retargets the grid, and in any mode it makes the project's first
// session the active one. Empty projects are still selectable —
// currentProjectId is set so ⌘N targets them correctly.
function switchToProject(pid) {
  if (!pid) return;
  state.currentProjectId = pid;
  if (state.view === 'grid-project') state.gridProjectId = pid;
  const sessions = state.sessions
    .filter((s) => (s.projectId ?? s.project_id) === pid)
    .sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
  if (sessions[0]) switchTo(sessions[0].id);
  else {
    state.activeId = null;
    if (state.view === 'single') showSingle(null);
    else renderGrid();
    updateSidebarSelection();
  }
}

// gridLayout caches the (rows, cols) chosen for the current scope plus
// the per-tile placement so the keyboard navigation logic doesn't have
// to recompute. assignments[i] = { row, col, rowSpan } — tiles above
// last-row empty cells extend downward to fill the grid (matches
// current Hive's behavior). cellMap[row*cols + col] = session index.
let gridLayout = { rows: 1, cols: 1, sessions: [], assignments: [], cellMap: [] };

// computeGridDims picks (rows, cols) that fills the container without
// scrolling, biasing tile aspect toward typical terminal proportions
// (~1.6 wide-to-tall). Reorders sessions row-major so that arrow
// navigation feels predictable.
//
// Small-n special cases match user expectation rather than the
// aspect-ratio optimizer, which can pick stacked layouts on tall
// windows when side-by-side is what people mean by "two terminals":
//   n=1 → 1x1
//   n=2 → 1x2 (always side-by-side)
function computeGridDims(n, w, h) {
  if (n <= 0) return { rows: 1, cols: 1 };
  if (n === 1) return { rows: 1, cols: 1 };
  if (n === 2) return { rows: 1, cols: 2 };

  const targetAspect = 1.6;
  let best = { rows: 1, cols: n, score: Infinity };
  for (let cols = 1; cols <= n; cols++) {
    const rows = Math.ceil(n / cols);
    const tileW = w / cols;
    const tileH = h / rows;
    if (tileW <= 0 || tileH <= 0) continue;
    const aspect = tileW / tileH;
    // log distance from target, plus a small penalty for empty cells
    // in the last row to prefer balanced grids.
    const empty = rows * cols - n;
    const score = Math.abs(Math.log(aspect / targetAspect)) + empty * 0.05;
    if (score < best.score) best = { rows, cols, score };
  }
  return best;
}

// renderGrid lays out every tile that should be visible in the
// current grid scope. Tiles for other sessions are hidden but kept
// alive (so their xterm scrollback persists across mode switches).
function renderGrid() {
  termsHost.classList.remove('single');
  termsHost.classList.add('grid');
  const gridSessions = gridScopeSessions();
  const gridIDs = new Set(gridSessions.map((s) => s.id));
  const n = gridSessions.length;

  // Ensure every grid session has a SessionTerm and is attached.
  // Move tiles into the desired DOM order (row-major) so that flexbox
  // / CSS grid honors the navigation order without us having to set
  // grid-row/column explicitly.
  for (const info of gridSessions) {
    const st = ensureTerm(info);
    st.host.classList.add('in-grid');
    st.host.classList.toggle('active', info.id === state.activeId);
    st.host.classList.toggle('attention', state.attention.has(info.id));
    st.ensureAttached();
    termsHost.appendChild(st.host); // re-order to keep DOM == nav order
  }
  // Hide / unmark tiles outside the scope.
  for (const [sid, st] of state.terms) {
    if (!gridIDs.has(sid)) {
      st.host.classList.remove('in-grid', 'active');
      st.host.style.gridRow = '';
      st.host.style.gridColumn = '';
    }
  }

  // Pick (rows, cols) that fills the container.
  const w = termsHost.clientWidth || 800;
  const h = termsHost.clientHeight || 600;
  const { rows, cols } = computeGridDims(n, w, h);

  // Compute placement: each tile occupies one cell; tiles directly
  // above empty cells in the last row extend downward to fill the
  // gap. Last-row gaps are at row-major indices [n .. rows*cols-1].
  const assignments = new Array(n);
  for (let i = 0; i < n; i++) {
    assignments[i] = { row: Math.floor(i / cols), col: i % cols, rowSpan: 1 };
  }
  for (let e = n; e < rows * cols; e++) {
    const aboveIdx = e - cols;
    if (aboveIdx >= 0 && aboveIdx < n) {
      assignments[aboveIdx].rowSpan += 1;
    }
  }

  termsHost.style.gridTemplateColumns = `repeat(${cols}, 1fr)`;
  termsHost.style.gridTemplateRows = `repeat(${rows}, 1fr)`;

  // Apply each tile's row span. CSS grid 1-based; row indices are
  // implicit row-major, so we only need to span when rowSpan > 1.
  for (let i = 0; i < n; i++) {
    const a = assignments[i];
    const st = state.terms.get(gridSessions[i].id);
    if (!st) continue;
    if (a.rowSpan > 1) {
      st.host.style.gridRow = `span ${a.rowSpan}`;
    } else {
      st.host.style.gridRow = '';
    }
    st.host.style.gridColumn = '';
  }

  // Build cellMap so spatial nav knows which tile owns each grid cell
  // (including the cells absorbed by row-spans).
  const cellMap = new Array(rows * cols).fill(null);
  for (let i = 0; i < n; i++) {
    const a = assignments[i];
    for (let dr = 0; dr < a.rowSpan; dr++) {
      cellMap[(a.row + dr) * cols + a.col] = i;
    }
  }

  gridLayout = { rows, cols, sessions: gridSessions, assignments, cellMap };

  // Refit each visible tile after the layout settles.
  requestAnimationFrame(() => {
    for (const info of gridSessions) {
      state.terms.get(info.id)?.refit();
    }
  });
}

// setActive centralizes "the focused session changed" so every code
// path (click, arrow nav, project switch, switchTo) clears the bell
// indicator the same way and syncs the current project to whatever
// project the new session belongs to.
function setActive(id) {
  if (id) {
    state.attention.delete(id);
    state.terms.get(id)?.host.classList.remove('attention');
    const s = state.sessions.find((x) => x.id === id);
    const pid = s?.projectId ?? s?.project_id;
    if (pid) state.currentProjectId = pid;
  }
  state.activeId = id;
  // Schedule focus after the next paint so any DOM reorder / visibility
  // change from renderGrid / showSingle has settled. xterm.focus()
  // moves focus to its hidden textarea; that fires .onFocus, which
  // adds .term-focused to the host (the source of truth for the
  // visual focus border).
  if (id) focusActiveTerm();
}

// focusActiveTerm focuses the xterm of state.activeId. Use this
// (rather than calling term.focus() directly) at every site that
// might leave the terminal without keyboard focus — switching modes,
// closing dialogs, returning from OS fullscreen, etc.
function focusActiveTerm() {
  const st = state.activeId && state.terms.get(state.activeId);
  if (!st) return;
  // A single click on a tile schedules this rAF; a dblclick then
  // opens the inline rename input. Without checking activeElement
  // when the rAF fires we'd snatch focus back from the rename input
  // and the user couldn't type the new name. Same logic protects
  // launcher/project-editor inputs from being stolen out from under.
  requestAnimationFrame(() => {
    const ae = document.activeElement;
    if (
      ae &&
      (ae.tagName === 'INPUT' || ae.tagName === 'TEXTAREA' || ae.isContentEditable) &&
      !ae.classList.contains('xterm-helper-textarea')
    ) {
      return;
    }
    st.term.focus();
  });
}

// refocusActiveTerm is the "the user just dismissed something — put
// keystrokes back on the active session" version. Skips when the user
// is interacting with a real input (rename, project editor, etc.) or
// when a modal is open.
function refocusActiveTerm() {
  if (!launcherEl.classList.contains('hidden')) return;
  if (!editorEl.classList.contains('hidden')) return;
  const ae = document.activeElement;
  if (ae && (ae.tagName === 'INPUT' || ae.tagName === 'TEXTAREA' || ae.isContentEditable)) {
    if (!ae.classList.contains('xterm-helper-textarea')) return;
  }
  focusActiveTerm();
}

// gridSpatialMove moves the active tile in the given direction.
// Uses cellMap to honor row-spanned tiles: e.g. with 3 sessions in a
// 2x2 grid the bottom-right cell is absorbed by tile 1, so pressing
// "right" from tile 2 lands on tile 1 instead of doing nothing.
function gridSpatialMove(dCol, dRow) {
  const { rows, cols, sessions, cellMap, assignments } = gridLayout;
  if (sessions.length === 0) return;
  const idx = sessions.findIndex((s) => s.id === state.activeId);
  if (idx < 0) {
    setActive(sessions[0].id);
    renderGrid();
    updateSidebarSelection();
    return;
  }
  const a = assignments[idx];
  // For downward moves, start from the tile's bottom edge (last row of
  // its span); for the other directions the primary cell is correct.
  let r = a.row;
  let c = a.col;
  if (dRow > 0) r = a.row + a.rowSpan - 1;
  // Step in the requested direction, skipping cells that resolve to
  // the current tile (row-span absorption) or empty cells.
  let nr = r + dRow;
  let nc = c + dCol;
  while (nr >= 0 && nr < rows && nc >= 0 && nc < cols) {
    const target = cellMap[nr * cols + nc];
    if (target != null && target !== idx) {
      setActive(sessions[target].id);
      renderGrid();
      updateSidebarSelection();
      setStatus(sessions[target].name);
      return;
    }
    nr += dRow;
    nc += dCol;
  }
}

function shiftActiveProject(delta) {
  if (state.projects.length === 0) return;
  const cur = activeProjectId();
  const i = state.projects.findIndex((p) => p.id === cur);
  if (i < 0) return;
  const next = state.projects[(i + delta + state.projects.length) % state.projects.length];
  state.currentProjectId = next.id;
  if (state.view === 'grid-project') state.gridProjectId = next.id;

  const sessions = state.sessions
    .filter((s) => (s.projectId ?? s.project_id) === next.id)
    .sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
  if (sessions[0]) {
    setActive(sessions[0].id);
  } else {
    // Empty project — keep the project selected but drop the active
    // session so the user can ⌘N into it. activeProjectId() now
    // returns the empty project because currentProjectId is set.
    state.activeId = null;
  }
  if (state.view === 'single') showSingle(state.activeId);
  else renderGrid();
  updateSidebarSelection();
  setStatus(`${next.name}${sessions.length === 0 ? ' (empty)' : ''}`);
}

// gridScopeSessions returns the list of sessions that should be tiled
// in the current grid view.
function gridScopeSessions() {
  if (state.view === 'grid-all') return orderedSessions();
  if (state.view === 'grid-project') {
    const pid = state.gridProjectId || activeProjectId();
    return state.sessions
      .filter((s) => (s.projectId ?? s.project_id) === pid)
      .sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
  }
  return [];
}

function setView(view) {
  state.view = view;
  if (view === 'grid-project') {
    state.gridProjectId = activeProjectId();
  }
  if (view === 'single') {
    showSingle(state.activeId);
  } else {
    renderGrid();
  }
  updateSidebarSelection();
  // Toggling grid/fullscreen via the menu blurs the xterm; restore
  // focus so the user can keep typing into the active session.
  focusActiveTerm();
  const ord = orderedSessions();
  const active = ord.find((s) => s.id === state.activeId);
  setStatus(`${view}${active ? ' • ' + active.name : ''}`);
}


// ---------- daemon events ----------

EventsOn('project:list', (jsonStr) => {
  const { projects } = JSON.parse(jsonStr);
  state.projects = projects || [];
  if (!state.currentProjectId && state.projects[0]) {
    state.currentProjectId = state.projects[0].id;
  }
  renderSidebar();
});

EventsOn('project:event', (jsonStr) => {
  const ev = JSON.parse(jsonStr);
  const i = state.projects.findIndex((p) => p.id === ev.project.id);
  if (ev.kind === 'added') {
    if (i < 0) state.projects.push(ev.project);
    // First-ever project: make it current.
    if (!state.currentProjectId) state.currentProjectId = ev.project.id;
  } else if (ev.kind === 'removed') {
    if (i >= 0) state.projects.splice(i, 1);
    state.collapsed.delete(ev.project.id);
    if (state.currentProjectId === ev.project.id) {
      state.currentProjectId = state.projects[0]?.id ?? null;
    }
  } else if (ev.kind === 'updated') {
    if (i >= 0) state.projects[i] = ev.project;
    // Refresh tile-header project color for every session belonging
    // to this project so grid/single-mode title bars reflect rename
    // and recolor in real time.
    for (const s of state.sessions) {
      const pid = s.projectId ?? s.project_id;
      if (pid !== ev.project.id) continue;
      const st = state.terms.get(s.id);
      if (st) st.setProject(ev.project.name, ev.project.color);
    }
  }
  state.projects.sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
  renderSidebar();
});

// processAliveTransition compares incoming Alive against the last
// known value for this session and fires the death/revive side
// effects on the boundary. First sight of a session (no prior entry)
// just records the value without firing anything.
function processAliveTransition(info) {
  const prev = state.aliveById.get(info.id);
  state.aliveById.set(info.id, !!info.alive);
  if (prev === true && info.alive === false) {
    onSessionDeath(info);
  } else if (prev === undefined && info.alive === false) {
    // Session was born dead (e.g. agent binary not found).
    onSessionDeath(info);
  } else if (prev === false && info.alive === true) {
    state.dismissedDead.delete(info.id);
    const t = state.terms.get(info.id);
    if (t) {
      // Wipe stale frame from the previous (dead) shell so the revived
      // session's prompt lands on a clean screen instead of stacking on
      // the old cursor position.
      try { t.term.reset(); } catch {}
      t.attached = false;
      t.setDead(false);
    }
  }
}

EventsOn('session:list', (jsonStr) => {
  const { sessions } = JSON.parse(jsonStr);
  state.sessions = sessions || [];
  for (const s of state.sessions) processAliveTransition(s);
  renderSidebar();
  if (!state.activeId && state.sessions.length > 0) {
    switchTo(orderedSessions()[0].id);
  }
});

EventsOn('session:event', (jsonStr) => {
  const ev = JSON.parse(jsonStr);
  const i = state.sessions.findIndex((s) => s.id === ev.session.id);
  if (ev.kind === 'added' || ev.kind === 'updated') {
    processAliveTransition(ev.session);
  }
  if (ev.kind === 'added') {
    if (i < 0) state.sessions.push(ev.session);
    renderSidebar();
    switchTo(ev.session.id);
    return;
  }
  if (ev.kind === 'removed') {
    state.aliveById.delete(ev.session.id);
    state.dismissedDead.delete(ev.session.id);
    let nextId = null;
    if (state.activeId === ev.session.id) {
      const ord = orderedSessions();
      const idx = ord.findIndex((s) => s.id === ev.session.id);
      const nb = idx > 0 ? ord[idx - 1] : ord[idx + 1];
      nextId = nb?.id ?? null;
    }
    if (i >= 0) state.sessions.splice(i, 1);
    const t = state.terms.get(ev.session.id);
    if (t) {
      t.destroy();
      state.terms.delete(ev.session.id);
    }
    if (state.activeId === ev.session.id) {
      state.activeId = null;
      if (nextId) switchTo(nextId);
    }
  } else if (ev.kind === 'updated') {
    if (i >= 0) state.sessions[i] = ev.session;
    // Push the new name/color/worktree branch into the cached
    // SessionTerm so the grid tile-header refreshes immediately.
    // Without this, renames look broken in grid mode — the sidebar
    // updates but the tile keeps showing the old name.
    const st = state.terms.get(ev.session.id);
    if (st) {
      st.setInfo(ev.session);
      const pid = ev.session.projectId ?? ev.session.project_id;
      const proj = state.projects.find((p) => p.id === pid);
      st.setProject(proj?.name ?? '', proj?.color ?? '');
    }
    if (state.activeId === ev.session.id) updateAppTitle();
  }
  renderSidebar();
});

EventsOn('pty:data', (id, b64) => {
  state.terms.get(id)?.writeData(b64);
});

EventsOn('pty:event', (id, jsonStr) => {
  try {
    const ev = JSON.parse(jsonStr);
    const st = state.terms.get(id);
    if (st && ev.kind === 'scrollback_replay_done') {
      st.phase = 'live';
      // After scrollback replay, snap the viewport to the latest
      // line so the user sees the cursor / newest output rather than
      // landing somewhere mid-history.
      st.term.scrollToBottom();
      // Defensive refit: if measurement at attach time was off (layout
      // not yet settled, font metrics not loaded, etc.) the PTY app may
      // be drawing at the wrong size. A second refit after replay sends
      // the now-correct size and triggers SIGWINCH so the app repaints.
      st.refit();
    }
  } catch { /* ignore */ }
});

EventsOn('pty:disconnect', (id) => {
  const st = state.terms.get(id);
  if (st) st.attached = false;
});

EventsOn('pty:error', (id, jsonStr) => {
  const st = state.terms.get(id);
  if (st) {
    try {
      const e = JSON.parse(jsonStr);
      st.term.write(`\r\n\x1b[31m[hived: ${e.code}: ${e.message}]\x1b[0m\r\n`);
    } catch {}
  }
});

EventsOn('control:disconnect', () => {
  // During a user-initiated RestartDaemon we knowingly close the
  // control conn; the banner already says "Restarting hived…". Don't
  // also flash an alarming red status line in that window.
  if (daemonRestarting) return;
  setStatus('control disconnected', true);
});

// Stale-daemon banner. The Go side compares its own buildinfo.BuildID
// to the value advertised in WELCOME and emits "daemon:stale" on every
// connect with severity "match" / "mismatch" / "unknown". Mismatch is
// symmetric (the daemon could be older OR newer than the GUI — bisect,
// stash, reverse-checkout all flip the direction), so the copy is
// deliberately direction-neutral.
//
// Dismissal is keyed on the specific daemonBuild that was dismissed,
// so a *different* mismatched build later will still surface. A "match"
// reconnect clears the dismissal flag too.
const daemonBannerEl = document.getElementById('daemon-banner');
const daemonBannerText = document.getElementById('daemon-banner-text');
const daemonBannerRestart = document.getElementById('daemon-banner-restart');
const daemonBannerDismiss = document.getElementById('daemon-banner-dismiss');
let daemonBannerDismissedFor = null;
let daemonRestarting = false;

function showDaemonBanner(text) {
  daemonBannerText.textContent = text;
  daemonBannerEl.classList.remove('hidden');
}
function hideDaemonBanner() {
  daemonBannerEl.classList.add('hidden');
}
daemonBannerDismiss.addEventListener('click', () => {
  // Dismissals are per-daemon-build: re-show if a different build
  // appears later. We stash the build we last saw mismatched (if any).
  daemonBannerDismissedFor = daemonBannerEl.dataset.daemonBuild || '';
  hideDaemonBanner();
});
daemonBannerRestart.addEventListener('click', async () => {
  // Restart kills hived AND relaunches Hive itself, so every running
  // session ends. Warn first.
  const ok = await Confirm(
    'Restart Hive?',
    'This will close Hive, terminate every running shell and agent, ' +
    'and reopen Hive with a fresh daemon. Save your work first.\n\n' +
    'Continue?',
  );
  if (!ok) return;
  daemonBannerRestart.disabled = true;
  daemonRestarting = true;
  showDaemonBanner('Restarting Hive…');
  try {
    await RestartDaemon();
    // RestartDaemon quits this process on success; control returns
    // here only on failure paths.
  } catch (err) {
    setStatus(`restart failed: ${err}`, true);
    showDaemonBanner(`Restart failed: ${err}`);
  } finally {
    daemonBannerRestart.disabled = false;
    daemonRestarting = false;
  }
});

EventsOn('daemon:stale', (ev) => {
  if (!ev) return;
  daemonBannerEl.dataset.daemonBuild = ev.daemonBuild || '';
  if (ev.severity === 'match') {
    daemonBannerDismissedFor = null; // reset so future mismatch can re-show
    hideDaemonBanner();
    return;
  }
  // Same build the user already dismissed: stay hidden.
  if (daemonBannerDismissedFor === (ev.daemonBuild || '')) return;
  if (ev.severity === 'mismatch') {
    showDaemonBanner(
      `hived build (${ev.daemonBuild}) doesn't match this GUI (${ev.guiBuild}). ` +
      `Restart Hive to apply changes.`,
    );
  } else {
    showDaemonBanner(
      `Could not verify daemon build (gui=${ev.guiBuild || '?'}, daemon=${ev.daemonBuild || '?'}). ` +
      `If something looks wrong, restart Hive.`,
    );
  }
});

// User clicked a notification toast. Route to that session in the
// current view (single keeps single, grid keeps grid) without toggling
// modes. switchTo handles the view-aware repaint.
EventsOn('bell-click', (sessionId) => {
  if (!sessionId) return;
  const info = state.sessions.find((s) => s.id === sessionId);
  if (!info) return;
  switchTo(sessionId);
  clearAttention(sessionId);
});

EventsOn('control:error', async (jsonStr) => {
  let e;
  try { e = JSON.parse(jsonStr); } catch { setStatus('hived error', true); return; }
  // Worktree-dirty kill: confirm with the user. The daemon already
  // refused to kill, so we can safely retry with force=true if the
  // user accepts.
  if (e.code === 'worktree_dirty' && e.session_id) {
    const sess = state.sessions.find((s) => s.id === e.session_id);
    const branch = sess?.worktreeBranch ?? sess?.worktree_branch ?? 'this worktree';
    const ok = await Confirm(
      'Discard uncommitted changes?',
      `${sess?.name ?? 'Session'} has uncommitted changes in ${branch}.\n\n` +
      `Discard them and remove the worktree?`,
    );
    if (!ok) return;
    // Confirm() is async + modal; the session may have been removed
    // (or its worktree resolved) while the dialog was open. Re-check
    // before issuing a second kill that would just produce a confusing
    // "no_such_session" control error.
    if (!state.sessions.find((s) => s.id === e.session_id)) return;
    KillSession(e.session_id, true).catch((err) => {
      setStatus(`force kill failed: ${err}`, true);
    });
    return;
  }
  setStatus(`${e.code}: ${e.message}`, true);
  console.warn('hived control error:', e);
});

// ---------- agent launcher ----------

const launcherEl = document.getElementById('launcher');
const launcherState = {
  items: [],
  selected: 0,
  projectId: null,
  // useWorktree is sticky across launcher opens, persisted in
  // localStorage. ⌃⌘N opens the launcher with this forced to true
  // for the duration of that opening.
  useWorktree: localStorage.getItem('hive.worktree') === '1',
};

function loadAgentUsage() {
  try { return JSON.parse(localStorage.getItem('hive.agentUsage') || '{}') || {}; }
  catch { return {}; }
}
function bumpAgentUsage(id) {
  if (!id) return;
  const u = loadAgentUsage();
  u[id] = (u[id] || 0) + 1;
  try { localStorage.setItem('hive.agentUsage', JSON.stringify(u)); } catch {}
}

function highlightLauncherSelection() {
  launcherState.items.forEach((it, i) => {
    it.el.classList.toggle('selected', i === launcherState.selected);
    if (i === launcherState.selected) it.el.scrollIntoView({ block: 'nearest' });
  });
}

function moveLauncherSelection(delta) {
  const n = launcherState.items.length;
  if (n === 0) return;
  launcherState.selected = (launcherState.selected + delta + n) % n;
  highlightLauncherSelection();
}

function activateLauncherSelection() {
  const it = launcherState.items[launcherState.selected];
  if (!it) return;
  bumpAgentUsage(it.agent.id);
  CreateSession(
    it.agent.id,
    launcherState.projectId || activeProjectId(),
    '', '',
    0, 0,
    !!launcherState.useWorktree,
  );
  closeLauncher();
}

function openLauncher(projectId, opts) {
  launcherState.projectId = projectId || activeProjectId();
  // Re-read the sticky pref each open so a one-shot forceWorktree from a
  // previous opening doesn't leak into the next regular open. forceWorktree
  // overrides for this opening only and is intentionally not persisted.
  launcherState.useWorktree =
    opts && typeof opts.forceWorktree === 'boolean'
      ? opts.forceWorktree
      : localStorage.getItem('hive.worktree') === '1';
  ListAgents()
    .then((agents) => {
      launcherEl.innerHTML = '';
      launcherState.items = [];
      // Anchor next to the resolved project's + button so the user
      // can see which project the new session lands in. Falls back
      // to the global new-project button if the project's row isn't
      // currently in the DOM (e.g. its header is offscreen).
      const anchorEl =
        document.querySelector(`.project[data-pid="${launcherState.projectId}"] .project-actions button`) ??
        document.getElementById('new-project-btn');
      const r = anchorEl.getBoundingClientRect();
      launcherEl.style.left = `${r.left}px`;
      launcherEl.style.top = `${r.bottom + 4}px`;

      // Worktree toggle row at the top of the menu. Disabled (and
      // visually muted) when the active project's cwd isn't a git
      // repo. The IsGitRepo probe is async; we render the row
      // immediately as enabled and disable it once the probe
      // completes — almost always before the user reaches for the
      // checkbox.
      const proj = state.projects.find((p) => p.id === launcherState.projectId);
      const projCwd = proj?.cwd ?? '';
      const wtRow = document.createElement('label');
      wtRow.className = 'launcher-worktree';
      const wtBox = document.createElement('input');
      wtBox.type = 'checkbox';
      wtBox.checked = !!launcherState.useWorktree;
      const wtLabel = document.createElement('span');
      wtLabel.textContent = 'Create in git worktree';
      wtRow.append(wtBox, wtLabel);
      wtBox.addEventListener('change', (e) => {
        launcherState.useWorktree = e.target.checked;
        localStorage.setItem('hive.worktree', e.target.checked ? '1' : '0');
      });
      launcherEl.appendChild(wtRow);
      if (projCwd) {
        IsGitRepo(projCwd).then((ok) => {
          if (!ok) {
            wtRow.classList.add('disabled');
            wtBox.disabled = true;
            wtBox.checked = false;
            launcherState.useWorktree = false;
            wtLabel.textContent = 'Worktree (project is not a git repo)';
          }
        }).catch(() => {});
      }
      // Detection (exec.LookPath on the daemon side) is best-effort:
      // it can miss agents installed as shell aliases, functions, or
      // PATH that's only set up by an interactive rc file. So we list
      // every agent as launchable and let the user try; the daemon
      // runs the command via the user's interactive shell, and any
      // real failure surfaces as "command not found" inside the
      // session's terminal. The "install" hint stays visible as
      // advisory text for the truly-not-installed case.
      // Sort agents by recent usage (most-used first), ties preserve
      // the agent package's display order. Usage is persisted in
      // localStorage and incremented on activation.
      const usage = loadAgentUsage();
      const ordered = agents
        .map((a, i) => ({ a, i }))
        .sort((x, y) => {
          const ux = usage[x.a.id] || 0, uy = usage[y.a.id] || 0;
          if (ux !== uy) return uy - ux;
          return x.i - y.i;
        })
        .map((e) => e.a);
      ordered.forEach((a, idx) => {
        const item = document.createElement('div');
        item.className = 'launcher-item' + (a.available ? '' : ' uninstalled');
        item.style.setProperty('--agent-color', a.color);
        const num = document.createElement('span');
        num.className = 'agent-num';
        // Number keys 1–9 select that row directly; 10+ rows show no
        // number (no digit shortcut).
        num.textContent = idx < 9 ? String(idx + 1) : '';
        const dot = document.createElement('span');
        dot.className = 'agent-dot';
        const name = document.createElement('span');
        name.className = 'agent-name';
        name.textContent = a.name;
        item.append(num, dot, name);
        if (!a.available && a.installCmd && a.installCmd.length) {
          const tag = document.createElement('span');
          tag.className = 'install-tag';
          tag.title = a.installCmd.join(' ');
          tag.textContent = 'install?';
          item.appendChild(tag);
        }
        item.addEventListener('click', () => {
          bumpAgentUsage(a.id);
          CreateSession(
            a.id,
            launcherState.projectId,
            '', '',
            0, 0,
            !!launcherState.useWorktree,
          );
          closeLauncher();
        });
        item.addEventListener('mouseenter', () => {
          launcherState.selected = idx;
          highlightLauncherSelection();
        });
        launcherEl.appendChild(item);
        launcherState.items.push({ agent: a, el: item });
      });
      launcherState.selected = 0;
      highlightLauncherSelection();
      launcherEl.classList.remove('hidden');
    })
    .catch(() => {});
}

function closeLauncher() {
  launcherEl.classList.add('hidden');
  launcherState.items = [];
  refocusActiveTerm();
}

document.addEventListener('click', (e) => {
  const inAction = e.target.closest('.project-actions');
  if (!launcherEl.contains(e.target) && !inAction) closeLauncher();
});

// ---------- project editor (new + edit) ----------

const editorEl = document.getElementById('project-editor');
const editorTitle = document.getElementById('project-editor-title');
const editorName = document.getElementById('project-editor-name');
const editorCwd = document.getElementById('project-editor-cwd');
const editorColor = document.getElementById('project-editor-color');
const editorState = { editing: null }; // null = create; else project object

function openProjectEditor(project) {
  editorState.editing = project || null;
  editorTitle.textContent = project ? 'Edit project' : 'New project';
  editorName.value = project?.name ?? '';
  editorColor.value = project?.color || '#f59e0b';
  if (project) {
    editorCwd.value = project.cwd ?? '';
  } else {
    LaunchDir().then((d) => { editorCwd.value = d || ''; }).catch(() => {});
    editorCwd.value = '';
  }
  editorEl.classList.remove('hidden');
  setTimeout(() => editorName.focus(), 0);
}

function closeProjectEditor() {
  editorEl.classList.add('hidden');
  editorState.editing = null;
  refocusActiveTerm();
}

function saveProjectEditor() {
  const name = editorName.value.trim();
  const cwd = editorCwd.value.trim();
  const color = editorColor.value;
  if (!name) return;
  if (editorState.editing) {
    UpdateProject(editorState.editing.id, name, color, cwd, -1);
  } else {
    CreateProject(name, color, cwd);
  }
  closeProjectEditor();
}

document.getElementById('project-editor-cancel').addEventListener('click', closeProjectEditor);
document.getElementById('project-editor-save').addEventListener('click', saveProjectEditor);
document.getElementById('project-editor-browse').addEventListener('click', async () => {
  try {
    const picked = await PickDirectory(editorCwd.value || '');
    if (picked) editorCwd.value = picked;
  } catch (err) {
    // Silently ignore (user cancelled, or platform refused).
  }
});
editorEl.addEventListener('keydown', (e) => {
  if (e.key === 'Enter' && (e.target === editorName || e.target === editorCwd)) {
    e.preventDefault();
    saveProjectEditor();
  } else if (e.key === 'Escape') {
    closeProjectEditor();
  }
});
document.getElementById('new-project-btn').addEventListener('click', () => openProjectEditor(null));

// ---------- keyboard ----------

window.addEventListener('keydown', (e) => {
  if (!launcherEl.classList.contains('hidden')) {
    const handle = (fn) => { e.preventDefault(); e.stopPropagation(); fn(); };
    if (e.key === 'ArrowDown' || (e.key === 'Tab' && !e.shiftKey)) return handle(() => moveLauncherSelection(+1));
    if (e.key === 'ArrowUp'   || (e.key === 'Tab' && e.shiftKey))  return handle(() => moveLauncherSelection(-1));
    if (e.key === 'Enter')   return handle(activateLauncherSelection);
    if (e.key === 'Escape')  return handle(closeLauncher);
    if ((e.metaKey || e.ctrlKey) && (e.key === 'n' || e.key === 'N')) return handle(closeLauncher);
    // Digit shortcut: 1–9 picks the corresponding row. Skipped when
    // a modifier is held so things like ⌘1 (browser tab switch) and
    // ⌘+ aren't swallowed.
    if (!e.metaKey && !e.ctrlKey && !e.altKey && /^[1-9]$/.test(e.key)) {
      const i = parseInt(e.key, 10) - 1;
      if (i < launcherState.items.length) {
        return handle(() => {
          launcherState.selected = i;
          activateLauncherSelection();
        });
      }
    }
  }
  if (!editorEl.classList.contains('hidden')) {
    return; // editor's own listener handles keys
  }
  const _palette = document.getElementById('command-palette');
  if (_palette && !_palette.classList.contains('hidden')) {
    return; // palette's own listener handles keys
  }

  // Dead-session overlay: route Enter/Escape to the active session's
  // overlay if it's shown. In grid mode the user can still click any
  // tile's buttons directly; this just handles the focused tile.
  if (state.activeId) {
    const t = state.terms.get(state.activeId);
    if (t?.deadOverlayShown) {
      if (e.key === 'Enter') { e.preventDefault(); e.stopPropagation(); t._closeDead(); return; }
      if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); t._dismissDead(); return; }
    }
  }

  const meta = e.metaKey || e.ctrlKey;
  if (!meta) return;
  const swallow = () => { e.preventDefault(); e.stopPropagation(); };

  if (e.key === '=' || e.key === '+') {
    swallow();
    bumpFontSize(+1);
    return;
  }
  if (e.key === '-' || e.key === '_') {
    swallow();
    bumpFontSize(-1);
    return;
  }
  if (e.key === '0') {
    swallow();
    resetFontSize();
    return;
  }

  if ((e.key === 'k' || e.key === 'K') && e.shiftKey && e.metaKey && !e.ctrlKey) {
    swallow();
    openCommandPalette();
    return;
  }
  if ((e.key === 'p' || e.key === 'P') && e.shiftKey) {
    swallow();
    openProjectEditor(null);
  } else if (e.key === 't' || e.key === 'T') {
    swallow();
    if (e.shiftKey) openLauncher(undefined, { forceWorktree: true });
    else openLauncher();
  } else if (e.key === 'Backspace' && e.shiftKey) {
    swallow();
    deleteActiveProject();
  } else if (e.key === 's' || e.key === 'S') {
    swallow();
    const app = document.getElementById('app');
    app.classList.toggle('sidebar-hidden');
    // Re-fit visible terminals after the layout transition.
    setTimeout(() => {
      if (state.view === 'single') {
        state.terms.get(state.activeId)?.refit();
      } else {
        for (const info of gridScopeSessions()) state.terms.get(info.id)?.refit();
      }
    }, 150);
  } else if (e.key === 'g' || e.key === 'G') {
    swallow();
    if (e.shiftKey) {
      setView(state.view === 'grid-all' ? 'single' : 'grid-all');
    } else {
      setView(state.view === 'grid-project' ? 'single' : 'grid-project');
    }
  } else if (e.key === 'Enter') {
    // ⌘Enter mirrors ⌘G: in a grid mode it maximizes the active
    // tile back to single mode; in single mode it expands to a
    // per-project grid for context.
    swallow();
    if (state.view === 'single') setView('grid-project');
    else setView('single');
  } else if (e.key === 'n' || e.key === 'N') {
    swallow();
    if (e.shiftKey) {
      OpenNewWindow().catch((err) => {
        setStatus(`window failed: ${err}`, true);
      });
    } else {
      // ⌘N — new project. (⌥⌘N is reserved by macOS Spotlight.)
      openProjectEditor(null);
    }
  } else if (e.key === 'w' || e.key === 'W') {
    swallow();
    if (e.shiftKey) {
      CloseWindow();
    } else if (state.activeId) {
      // force=false: lets the daemon refuse with worktree_dirty if
      // the worktree has uncommitted changes; the control:error
      // handler then shows a confirm dialog and retries with force.
      KillSession(state.activeId, false);
    }
  } else if (/^[1-9]$/.test(e.key)) {
    const idx = parseInt(e.key, 10) - 1;
    const ord = orderedSessions();
    if (idx < ord.length) {
      swallow();
      switchTo(ord[idx].id);
    }
  } else if (e.key === 'ArrowLeft') {
    swallow();
    if (state.view !== 'single') gridSpatialMove(-1, 0);
    else moveActiveSession(-1, e.shiftKey);
  } else if (e.key === 'ArrowRight') {
    swallow();
    if (state.view !== 'single') gridSpatialMove(+1, 0);
    else moveActiveSession(+1, e.shiftKey);
  } else if (e.key === 'ArrowUp') {
    swallow();
    if (state.view !== 'single') gridSpatialMove(0, -1);
    else moveActiveSession(-1, e.shiftKey);
  } else if (e.key === 'ArrowDown') {
    swallow();
    if (state.view !== 'single') gridSpatialMove(0, +1);
    else moveActiveSession(+1, e.shiftKey);
  } else if (e.key === '[') {
    swallow();
    shiftActiveProject(-1);
  } else if (e.key === ']') {
    swallow();
    shiftActiveProject(+1);
  }
}, true);

// ---------- menu actions ----------
//
// Native menu items emit `menu:<action>` events from cmd/hivegui/menu.go.
// They dispatch to the same handlers as the keyboard listener above so the
// menu and keyboard stay in lockstep — when you add a shortcut, add it
// here AND in menu.go.

function toggleSidebar() {
  const app = document.getElementById('app');
  app.classList.toggle('sidebar-hidden');
  setTimeout(() => {
    if (state.view === 'single') {
      state.terms.get(state.activeId)?.refit();
    } else {
      for (const info of gridScopeSessions()) state.terms.get(info.id)?.refit();
    }
  }, 150);
}

function toggleProjectGrid() {
  setView(state.view === 'grid-project' ? 'single' : 'grid-project');
}

function toggleAllGrid() {
  setView(state.view === 'grid-all' ? 'single' : 'grid-all');
}

function navSession(delta) {
  if (state.view !== 'single') {
    gridSpatialMove(delta > 0 ? +1 : -1, 0);
  } else {
    moveActiveSession(delta, false);
  }
}

function reorderActive(delta) {
  if (state.view === 'single') moveActiveSession(delta, true);
  else gridSpatialMove(delta > 0 ? +1 : -1, 0);
}

function switchToNthSession(n) {
  const ord = orderedSessions();
  if (n - 1 < ord.length) switchTo(ord[n - 1].id);
}

const menuActions = {
  'menu:new-session': () => openLauncher(),
  'menu:new-session-worktree': () => openLauncher(undefined, { forceWorktree: true }),
  'menu:new-project': () => openProjectEditor(null),
  'menu:delete-project': () => deleteActiveProject(),
  'menu:command-palette': () => openCommandPalette(),
  'menu:close-session': () => { if (state.activeId) KillSession(state.activeId, false); },
  'menu:zoom-in': () => bumpFontSize(+1),
  'menu:zoom-out': () => bumpFontSize(-1),
  'menu:zoom-reset': () => resetFontSize(),
  'menu:toggle-sidebar': toggleSidebar,
  'menu:toggle-project-grid': toggleProjectGrid,
  'menu:toggle-all-grid': toggleAllGrid,
  'menu:next-session': () => navSession(+1),
  'menu:prev-session': () => navSession(-1),
  'menu:move-session-forward': () => reorderActive(+1),
  'menu:move-session-backward': () => reorderActive(-1),
  'menu:next-project': () => shiftActiveProject(+1),
  'menu:prev-project': () => shiftActiveProject(-1),
};
for (const [name, fn] of Object.entries(menuActions)) {
  EventsOn(name, fn);
}
for (let i = 1; i <= 9; i++) {
  EventsOn(`menu:switch-${i}`, () => switchToNthSession(i));
}

// ---------- delete project ----------

// confirmAndDeleteProject is the single confirm + KillProject path
// shared by the sidebar ✕ button and the ⇧⌘⌫ shortcut. Kept as one
// function so the prompt text and killSessions logic can't drift.
async function confirmAndDeleteProject(proj) {
  if (!proj) return;
  const sessions = state.sessions.filter(
    (s) => (s.projectId ?? s.project_id) === proj.id,
  );
  const msg = sessions.length
    ? `Delete project "${proj.name}" and kill ${sessions.length} session${sessions.length === 1 ? '' : 's'}?`
    : `Delete project "${proj.name}"?`;
  const ok = await Confirm('Delete project', msg);
  if (!ok) return;
  KillProject(proj.id, sessions.length > 0).catch((err) => {
    setStatus(`delete failed: ${err}`, true);
  });
}

function deleteActiveProject() {
  const pid = activeProjectId();
  confirmAndDeleteProject(state.projects.find((p) => p.id === pid));
}

// ---------- command palette ----------

const paletteCommands = [
  { id: 'new-project',          name: 'New Project…',                shortcut: '⌘N',     run: () => openProjectEditor(null) },
  { id: 'new-session',          name: 'New Session',                 shortcut: '⌘T',     run: () => openLauncher() },
  { id: 'new-session-worktree', name: 'New Session in Worktree',     shortcut: '⇧⌘T',    run: () => openLauncher(undefined, { forceWorktree: true }) },
  { id: 'delete-project',       name: 'Delete Active Project…',      shortcut: '⇧⌘⌫',    run: () => deleteActiveProject() },
  { id: 'close-session',        name: 'Close Session',               shortcut: '⌘W',     run: () => { if (state.activeId) KillSession(state.activeId, false); } },
  { id: 'new-window',           name: 'New Window',                  shortcut: '⇧⌘N',    run: () => OpenNewWindow().catch((err) => setStatus(`window failed: ${err}`, true)) },
  { id: 'close-window',         name: 'Close Window',                shortcut: '⇧⌘W',    run: () => CloseWindow() },
  { id: 'toggle-sidebar',       name: 'Toggle Sidebar',              shortcut: '⌘S',     run: toggleSidebar },
  { id: 'toggle-project-grid',  name: 'Toggle Project Grid',         shortcut: '⌘G',     run: toggleProjectGrid },
  { id: 'toggle-all-grid',      name: 'Toggle All Sessions Grid',    shortcut: '⇧⌘G',    run: toggleAllGrid },
  { id: 'zoom-in',              name: 'Zoom In',                     shortcut: '⌘=',     run: () => bumpFontSize(+1) },
  { id: 'zoom-out',             name: 'Zoom Out',                    shortcut: '⌘-',     run: () => bumpFontSize(-1) },
  { id: 'zoom-reset',           name: 'Actual Size',                 shortcut: '⌘0',     run: () => resetFontSize() },
  { id: 'next-session',         name: 'Next Session',                shortcut: '⌘↓',     run: () => navSession(+1) },
  { id: 'prev-session',         name: 'Previous Session',            shortcut: '⌘↑',     run: () => navSession(-1) },
  { id: 'move-forward',         name: 'Move Session Forward',        shortcut: '⇧⌘↓',    run: () => reorderActive(+1) },
  { id: 'move-backward',        name: 'Move Session Backward',       shortcut: '⇧⌘↑',    run: () => reorderActive(-1) },
  { id: 'next-project',         name: 'Next Project',                shortcut: '⌘]',     run: () => shiftActiveProject(+1) },
  { id: 'prev-project',         name: 'Previous Project',            shortcut: '⌘[',     run: () => shiftActiveProject(-1) },
];

const paletteEl = document.getElementById('command-palette');
const paletteInput = document.getElementById('command-palette-input');
const paletteList = document.getElementById('command-palette-list');
const paletteState = { items: [], selected: 0 };

function renderPalette() {
  const q = paletteInput.value.trim().toLowerCase();
  paletteList.innerHTML = '';
  paletteState.items = paletteCommands.filter((c) => {
    if (!q) return true;
    return c.name.toLowerCase().includes(q) || c.shortcut.toLowerCase().includes(q);
  });
  if (paletteState.selected >= paletteState.items.length) {
    paletteState.selected = 0;
  }
  paletteState.items.forEach((c, i) => {
    const row = document.createElement('div');
    row.className = 'palette-item' + (i === paletteState.selected ? ' selected' : '');
    const name = document.createElement('span');
    name.className = 'palette-name';
    name.textContent = c.name;
    const sc = document.createElement('span');
    sc.className = 'palette-shortcut';
    sc.textContent = c.shortcut;
    row.append(name, sc);
    row.addEventListener('mouseenter', () => {
      paletteState.selected = i;
      for (const el of paletteList.children) el.classList.remove('selected');
      row.classList.add('selected');
    });
    row.addEventListener('click', () => activatePalette(i));
    paletteList.appendChild(row);
  });
}

function openCommandPalette() {
  paletteInput.value = '';
  paletteState.selected = 0;
  renderPalette();
  paletteEl.classList.remove('hidden');
  paletteInput.focus();
}

function closeCommandPalette() {
  // Blur first: focusActiveTerm() bails when activeElement is an INPUT,
  // and hiding the palette via CSS doesn't move focus off paletteInput.
  paletteInput.blur();
  paletteEl.classList.add('hidden');
  focusActiveTerm();
}

function activatePalette(i) {
  const c = paletteState.items[i];
  if (!c) return;
  closeCommandPalette();
  // Defer so the palette is fully closed before the action runs
  // (some actions open another modal that owns focus).
  setTimeout(() => c.run(), 0);
}

paletteInput.addEventListener('input', renderPalette);
paletteEl.addEventListener('keydown', (e) => {
  if (e.key === 'Escape') {
    e.preventDefault(); e.stopPropagation();
    closeCommandPalette();
  } else if (e.key === 'ArrowDown' || (e.key === 'Tab' && !e.shiftKey)) {
    e.preventDefault(); e.stopPropagation();
    if (paletteState.items.length === 0) return;
    paletteState.selected = (paletteState.selected + 1) % paletteState.items.length;
    renderPalette();
  } else if (e.key === 'ArrowUp' || (e.key === 'Tab' && e.shiftKey)) {
    e.preventDefault(); e.stopPropagation();
    if (paletteState.items.length === 0) return;
    paletteState.selected = (paletteState.selected - 1 + paletteState.items.length) % paletteState.items.length;
    renderPalette();
  } else if (e.key === 'Enter') {
    e.preventDefault(); e.stopPropagation();
    activatePalette(paletteState.selected);
  }
});
document.addEventListener('mousedown', (e) => {
  if (paletteEl.classList.contains('hidden')) return;
  if (!paletteEl.contains(e.target)) closeCommandPalette();
});

// moveActiveSession walks the (project_order, session_order) list.
// reorder=true moves the session within its project only.
function moveActiveSession(delta, reorder) {
  const ord = orderedSessions();
  const n = ord.length;
  if (n === 0) return;
  const idx = ord.findIndex((s) => s.id === state.activeId);
  if (idx < 0) {
    switchTo(ord[0].id);
    return;
  }
  if (reorder) {
    const cur = state.sessions.find((s) => s.id === state.activeId);
    const sib = state.sessions
      .filter((s) => (s.projectId ?? s.project_id) === (cur.projectId ?? cur.project_id))
      .sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
    const sIdx = sib.findIndex((s) => s.id === state.activeId);
    const next = (sIdx + delta + sib.length) % sib.length;
    UpdateSession(state.activeId, '', '', next);
    return;
  }
  const next = (idx + delta + n) % n;
  switchTo(ord[next].id);
}

// ---------- resize ----------

let resizeTimer = null;
window.addEventListener('resize', () => {
  if (resizeTimer) clearTimeout(resizeTimer);
  resizeTimer = setTimeout(() => {
    if (state.view === 'single') {
      const t = state.activeId && state.terms.get(state.activeId);
      if (t) t.refit();
      return;
    }
    // Grid mode: re-pick (rows, cols) for the new container shape.
    // Just refitting each tile keeps the old layout, which looks
    // wrong after a landscape↔portrait resize until the user
    // switches sessions and accidentally triggers a re-render.
    renderGrid();
  }, 100);
});

// ---------- sidebar resize ----------
//
// Drag the right edge of the sidebar to resize. Width persists across
// reloads. Constrained to a sane min/max so the resizer can't be lost
// off-screen or eat the whole window.
(function setupSidebarResize() {
  const MIN = 140, MAX = 480;
  const app = document.getElementById('app');
  const handle = document.getElementById('sidebar-resizer');
  if (!app || !handle) return;
  const saved = parseInt(localStorage.getItem('hive.sidebarWidth') || '', 10);
  if (Number.isFinite(saved)) {
    app.style.setProperty('--sidebar-width', `${Math.max(MIN, Math.min(MAX, saved))}px`);
  }
  // #app spans the viewport, so pointer clientX maps directly to sidebar width.
  let dragging = false;
  function endDrag() {
    if (!dragging) return;
    dragging = false;
    document.body.classList.remove('resizing-sidebar');
    handle.classList.remove('dragging');
    const px = app.style.getPropertyValue('--sidebar-width');
    const w = parseInt(px, 10);
    if (Number.isFinite(w)) localStorage.setItem('hive.sidebarWidth', String(w));
    // Main pane width changed — reflow terminals.
    if (state.view === 'single') {
      const t = state.activeId && state.terms.get(state.activeId);
      if (t) t.refit();
    } else {
      renderGrid();
    }
  }
  handle.addEventListener('pointerdown', (e) => {
    e.preventDefault();
    dragging = true;
    document.body.classList.add('resizing-sidebar');
    handle.classList.add('dragging');
    // Capture so we keep getting moves/ups even if the cursor leaves the window.
    handle.setPointerCapture(e.pointerId);
  });
  handle.addEventListener('pointermove', (e) => {
    if (!dragging) return;
    const w = Math.max(MIN, Math.min(MAX, e.clientX));
    app.style.setProperty('--sidebar-width', `${w}px`);
  });
  handle.addEventListener('pointerup', endDrag);
  handle.addEventListener('pointercancel', endDrag);
  // Belt-and-braces: if focus leaves the window mid-drag, end the drag so a
  // stray mousemove on return doesn't snap the sidebar to the cursor.
  window.addEventListener('blur', endDrag);

  // Keyboard a11y: when the resizer has focus, arrow keys adjust width
  // (Shift = larger step). Persist + refit on each change.
  function nudge(delta) {
    const cur = parseInt(getComputedStyle(app).getPropertyValue('--sidebar-width'), 10);
    const base = Number.isFinite(cur) ? cur : 200;
    const w = Math.max(MIN, Math.min(MAX, base + delta));
    app.style.setProperty('--sidebar-width', `${w}px`);
    localStorage.setItem('hive.sidebarWidth', String(w));
    if (state.view === 'single') {
      const t = state.activeId && state.terms.get(state.activeId);
      if (t) t.refit();
    } else {
      renderGrid();
    }
  }
  handle.addEventListener('keydown', (e) => {
    const step = e.shiftKey ? 50 : 10;
    if (e.key === 'ArrowLeft')       { e.preventDefault(); nudge(-step); }
    else if (e.key === 'ArrowRight') { e.preventDefault(); nudge(+step); }
    else if (e.key === 'Home')       { e.preventDefault(); nudge(-MAX); }
    else if (e.key === 'End')        { e.preventDefault(); nudge(+MAX); }
  });
})();

// ---------- bootstrap ----------

(async () => {
  setStatus('connecting…');
  try {
    await ConnectControl();
    setStatus('connected');
  } catch (err) {
    setStatus(`connect failed: ${err}`, true);
  }
})();
