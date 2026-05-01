import '@xterm/xterm/css/xterm.css';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';

import {
  ConnectControl, OpenSession, CloseAttach,
  WriteStdin, ResizeSession,
  CreateSession, KillSession, UpdateSession, ListAgents,
} from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';

// ---------- session terminal ----------

class SessionTerm {
  constructor(info) {
    this.info = info; // { id, name, color, order, alive }
    this.host = document.createElement('div');
    this.host.className = 'term-host';
    this.host.dataset.sid = info.id;
    document.getElementById('terms').appendChild(this.host);

    this.term = new Terminal({
      fontFamily: 'Menlo, "DejaVu Sans Mono", monospace',
      fontSize: 14,
      cursorBlink: true,
      scrollback: 5000,
      theme: { background: '#000000' },
    });
    this.fit = new FitAddon();
    this.term.loadAddon(this.fit);
    this.term.open(this.host);
    this.attached = false;
    this.phase = 'replay';

    this.term.onData((data) => {
      const bytes = new TextEncoder().encode(data);
      let bin = '';
      for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
      WriteStdin(this.info.id, btoa(bin));
    });
  }

  show() {
    this.host.classList.add('visible');
    this.fit.fit();
    if (this.attached) {
      ResizeSession(this.info.id, this.term.cols, this.term.rows);
    }
    this.term.focus();
  }

  hide() {
    this.host.classList.remove('visible');
  }

  async ensureAttached() {
    if (this.attached) return;
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
}

const decoder = new TextDecoder('utf-8', { fatal: false });

// ---------- sidebar / app state ----------

const state = {
  sessions: [],          // SessionInfo[] in display order
  terms: new Map(),      // id -> SessionTerm
  activeId: null,
};

const sidebarUL = document.getElementById('sessions');
const status = document.getElementById('status');

function setStatus(text, isError = false) {
  status.textContent = text;
  status.classList.toggle('error', isError);
}

function renderSidebar() {
  sidebarUL.innerHTML = '';
  state.sessions.sort((a, b) => a.order - b.order);
  for (const s of state.sessions) {
    const li = document.createElement('li');
    li.className = 'session-item';
    if (s.id === state.activeId) li.classList.add('selected');
    if (!s.alive) li.classList.add('dead');
    li.style.setProperty('--session-color', s.color || '#888');
    li.dataset.sid = s.id;

    const dot = document.createElement('span');
    dot.className = 'dot';

    const name = document.createElement('span');
    name.className = 'name';
    name.textContent = s.name;

    const swatch = document.createElement('span');
    swatch.className = 'swatch';
    const colorInput = document.createElement('input');
    colorInput.type = 'color';
    colorInput.value = s.color || '#888888';
    colorInput.addEventListener('input', (e) => {
      UpdateSession(s.id, '', e.target.value, -1);
    });
    swatch.appendChild(colorInput);

    li.append(dot, name, swatch);
    li.addEventListener('click', (e) => {
      if (e.target === colorInput || e.target === swatch) return;
      switchTo(s.id);
    });
    li.addEventListener('dblclick', () => beginRename(s, li, name));
    sidebarUL.appendChild(li);
  }
}

function beginRename(sess, li, nameEl) {
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
      renderSidebar(); // restore
    }
  };
  input.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') finish(true);
    else if (e.key === 'Escape') finish(false);
  });
  input.addEventListener('blur', () => finish(true));
}

function switchTo(id) {
  if (id === state.activeId) return;
  if (state.activeId) {
    state.terms.get(state.activeId)?.hide();
  }
  state.activeId = id;
  let st = state.terms.get(id);
  if (!st) {
    const info = state.sessions.find((s) => s.id === id);
    if (!info) return;
    st = new SessionTerm(info);
    state.terms.set(id, st);
  }
  st.show();
  st.ensureAttached();
  renderSidebar();
  const info = state.sessions.find((s) => s.id === id);
  setStatus(info ? `${info.name}` : '');
}

// ---------- daemon events ----------

EventsOn('session:list', (jsonStr) => {
  const { sessions } = JSON.parse(jsonStr);
  state.sessions = sessions || [];
  renderSidebar();
  // Auto-attach to the first session on first load.
  if (!state.activeId && state.sessions.length > 0) {
    switchTo(state.sessions[0].id);
  }
});

EventsOn('session:event', (jsonStr) => {
  const ev = JSON.parse(jsonStr);
  const i = state.sessions.findIndex((s) => s.id === ev.session.id);
  if (ev.kind === 'added') {
    if (i < 0) state.sessions.push(ev.session);
    // Always focus a freshly-added session; in single-client Phase 3
    // every "added" event corresponds to an action this user just took.
    renderSidebar();
    switchTo(ev.session.id);
    return;
  }
  if (ev.kind === 'removed') {
    // If the removed session was active, focus the one immediately
    // before it in sidebar order (or after, if it was first).
    let nextId = null;
    if (state.activeId === ev.session.id && i >= 0 && state.sessions.length > 1) {
      const prevIdx = i > 0 ? i - 1 : i + 1;
      nextId = state.sessions[prevIdx]?.id ?? null;
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
    if (st && ev.kind === 'scrollback_replay_done') st.phase = 'live';
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
  setStatus('control disconnected', true);
});

// ---------- keyboard ----------

window.addEventListener('keydown', (e) => {
  // Launcher captures keys while open; check it first.
  if (!launcherEl.classList.contains('hidden')) {
    if (e.key === 'ArrowDown' || (e.key === 'Tab' && !e.shiftKey)) {
      e.preventDefault();
      moveLauncherSelection(+1);
      return;
    }
    if (e.key === 'ArrowUp' || (e.key === 'Tab' && e.shiftKey)) {
      e.preventDefault();
      moveLauncherSelection(-1);
      return;
    }
    if (e.key === 'Enter') {
      e.preventDefault();
      activateLauncherSelection();
      return;
    }
    if (e.key === 'Escape') {
      e.preventDefault();
      closeLauncher();
      return;
    }
  }

  const meta = e.metaKey || e.ctrlKey;
  if (!meta) return;
  if (e.key === 'n' || e.key === 'N') {
    e.preventDefault();
    openLauncher();
  } else if (e.key === 'w' || e.key === 'W') {
    e.preventDefault();
    if (state.activeId) KillSession(state.activeId);
  } else if (/^[1-9]$/.test(e.key)) {
    const idx = parseInt(e.key, 10) - 1;
    if (idx < state.sessions.length) {
      e.preventDefault();
      switchTo(state.sessions[idx].id);
    }
  }
});

// ---------- resize ----------

let resizeTimer = null;
window.addEventListener('resize', () => {
  if (resizeTimer) clearTimeout(resizeTimer);
  resizeTimer = setTimeout(() => {
    const t = state.activeId && state.terms.get(state.activeId);
    if (t) {
      t.fit.fit();
      ResizeSession(t.info.id, t.term.cols, t.term.rows);
    }
  }, 50);
});

// ---------- bootstrap ----------

// ---------- agent launcher menu ----------

const launcherEl = document.getElementById('launcher');
const launcherState = { items: [], selected: 0 }; // items[].agent, items[].el

function highlightLauncherSelection() {
  launcherState.items.forEach((it, i) => {
    it.el.classList.toggle('selected', i === launcherState.selected);
    if (i === launcherState.selected) {
      it.el.scrollIntoView({ block: 'nearest' });
    }
  });
}

function moveLauncherSelection(delta) {
  const n = launcherState.items.length;
  if (n === 0) return;
  let i = launcherState.selected;
  for (let step = 0; step < n; step++) {
    i = (i + delta + n) % n;
    if (launcherState.items[i].agent.available) break;
  }
  launcherState.selected = i;
  highlightLauncherSelection();
}

function activateLauncherSelection() {
  const it = launcherState.items[launcherState.selected];
  if (!it || !it.agent.available) return;
  CreateSession(it.agent.id, '', '', 0, 0);
  closeLauncher();
}

function openLauncher() {
  ListAgents()
    .then((agents) => {
      launcherEl.innerHTML = '';
      launcherState.items = [];
      const newBtn = document.getElementById('new-btn');
      const r = newBtn.getBoundingClientRect();
      launcherEl.style.left = `${r.left}px`;
      launcherEl.style.top = `${r.bottom + 4}px`;
      let firstAvailable = -1;
      agents.forEach((a, idx) => {
        const item = document.createElement('div');
        item.className = 'launcher-item' + (a.available ? '' : ' unavailable');
        item.style.setProperty('--agent-color', a.color);
        const dot = document.createElement('span');
        dot.className = 'agent-dot';
        const name = document.createElement('span');
        name.className = 'agent-name';
        name.textContent = a.name;
        item.append(dot, name);
        if (!a.available && a.installCmd && a.installCmd.length) {
          const tag = document.createElement('span');
          tag.className = 'install-tag';
          tag.title = a.installCmd.join(' ');
          tag.textContent = 'install';
          item.appendChild(tag);
        }
        if (a.available) {
          if (firstAvailable < 0) firstAvailable = idx;
          item.addEventListener('click', () => {
            CreateSession(a.id, '', '', 0, 0);
            closeLauncher();
          });
          item.addEventListener('mouseenter', () => {
            launcherState.selected = idx;
            highlightLauncherSelection();
          });
        }
        launcherEl.appendChild(item);
        launcherState.items.push({ agent: a, el: item });
      });
      launcherState.selected = firstAvailable >= 0 ? firstAvailable : 0;
      highlightLauncherSelection();
      launcherEl.classList.remove('hidden');
    })
    .catch(() => {});
}

function closeLauncher() {
  launcherEl.classList.add('hidden');
  launcherState.items = [];
}

document.getElementById('new-btn').addEventListener('click', (e) => {
  e.stopPropagation();
  if (launcherEl.classList.contains('hidden')) openLauncher();
  else closeLauncher();
});

document.addEventListener('click', (e) => {
  if (!launcherEl.contains(e.target) && e.target.id !== 'new-btn') closeLauncher();
});
window.addEventListener('keydown', (e) => {
  if (e.key === 'Escape' && !launcherEl.classList.contains('hidden')) closeLauncher();
});

(async () => {
  setStatus('connecting…');
  try {
    await ConnectControl();
    setStatus('connected');
  } catch (err) {
    setStatus(`connect failed: ${err}`, true);
  }
})();
