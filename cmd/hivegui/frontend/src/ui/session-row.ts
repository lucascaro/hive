// Session row — docs/design-docs/ui/components.md › sessionRow.
//
// 40px, two lines: name over window title. Grid is
// [state 14px] [text 1fr] [meta auto]; the meta column (key hint,
// worktree, agent code) is swapped for the action buttons on hover or
// keyboard focus — see patterns.md › Hover-revealed actions.
//
// This module owns markup and per-part callbacks only. Drag-reorder,
// inline rename and double-click live on the returned <li> and are wired
// by app/sidebar.ts, which owns that behaviour.
import { icon, stateIcon, updateStateIcon } from './icon.js';
import { iconButton } from './icon-button.js';
import { kbd } from './kbd.js';
import type { SessionState } from '../lib/session-state.js';
import { displayTitle } from '../lib/term-title.js';
import type { SessionInfo } from '../app/state.js';

export interface SessionRowState {
  state: SessionState;
  selected: boolean;
  minimized: boolean;
  index: number | null;
}

export interface SessionRowOpts extends SessionRowState {
  session: SessionInfo;
  onSelect: () => void;
  onMinimize: () => void;
  onRestore: () => void;
  onRestart: () => void;
  onKill: () => void;
  onWorktrees: () => void;
  onColor: (hex: string) => void;
}

// Line 2 when the program has published no window title. One channel per
// fact (README principle 2): the row says what the session is doing, and
// when it is doing nothing it says why. Never both title and state words.
function subtitleFor(s: SessionInfo, state: SessionState): string {
  const t = displayTitle(s.title, s.name);
  if (t) return t;
  if (state === 'starting') return 'Starting…';
  if (state === 'exited') return 'Exited';
  if (state === 'error') {
    const err = (s.last_error ?? s.lastError ?? '').trim();
    return err ? `Exited — ${err}` : 'Exited';
  }
  return '';
}

// Agent short code: two letters, mono, in the meta column. `cl`, `co`,
// `ge`, `sh` fall out of "first two letters" for the built-ins, so there
// is no table to keep in sync with settings' user-defined agents.
function agentCode(agent?: string): string {
  return (agent ?? '').trim().slice(0, 2).toLowerCase();
}

export function sessionRow(o: SessionRowOpts): HTMLLIElement {
  const s = o.session;
  const li = document.createElement('li');
  li.className = 'hv-session-row';
  li.dataset.sid = s.id;
  li.dataset.pid = s.projectId ?? s.project_id ?? '';
  li.draggable = true;
  if (s.color) li.style.setProperty('--session-color', s.color);

  // classList (an Element API, so it works on SVG); never `.className`,
  // which is a read-only SVGAnimatedString on an SVG element.
  const st = stateIcon(o.state);
  st.classList.add('hv-session-row__state');

  const text = document.createElement('span');
  text.className = 'hv-session-row__text';
  const name = document.createElement('span');
  name.className = 'hv-session-row__name';
  const sub = document.createElement('span');
  sub.className = 'hv-session-row__sub';
  text.append(name, sub);

  const meta = document.createElement('span');
  meta.className = 'hv-session-row__meta';

  const wtBranch = s.worktreeBranch ?? s.worktree_branch;
  if (wtBranch) {
    const wt = iconButton({
      icon: 'branch',
      label: `Worktree: ${wtBranch} — manage worktrees`,
      onClick: (e) => {
        e.stopPropagation();
        o.onWorktrees();
      },
    });
    wt.classList.add('hv-session-row__worktree');
    meta.append(wt);
  }

  const code = agentCode(s.agent);
  if (code) {
    const agent = document.createElement('span');
    agent.className = 'hv-session-row__agent';
    agent.textContent = code;
    meta.append(agent);
  }

  const actions = document.createElement('span');
  actions.className = 'hv-session-row__actions';

  const minBtn = iconButton({
    icon: o.minimized ? 'plus' : 'minus',
    label: `${o.minimized ? 'Restore' : 'Minimize'} ${s.name ?? 'session'}`,
    onClick: (e) => {
      e.stopPropagation();
      if (li.dataset.minimized === undefined) o.onMinimize();
      else o.onRestore();
    },
  });
  minBtn.dataset.action = 'minimize';

  const restartBtn = iconButton({
    icon: 'rotate',
    label: `Restart ${s.name ?? 'session'}`,
    onClick: (e) => {
      e.stopPropagation();
      o.onRestart();
    },
  });
  restartBtn.dataset.action = 'restart';

  const killBtn = iconButton({
    icon: 'x',
    label: `Kill ${s.name ?? 'session'}`,
    onClick: (e) => {
      e.stopPropagation();
      o.onKill();
    },
  });
  killBtn.dataset.action = 'kill';

  // rotate first, x second — patterns.md › Exited sessions. Restart is
  // only offered where it means something; a running session's restart is
  // the tile's job, not a one-click sidebar action.
  actions.append(minBtn);
  if (o.state === 'exited' || o.state === 'error') actions.append(restartBtn);
  actions.append(killBtn);

  // The colour picker keeps its native input (components.md › Form fields)
  // and sits outside the hover swap: it is data, not an action.
  const swatch = document.createElement('span');
  swatch.className = 'hv-session-row__swatch';
  const colorInput = document.createElement('input');
  colorInput.type = 'color';
  colorInput.value = s.color || '#888888';
  colorInput.setAttribute('aria-label', `Colour for ${s.name ?? 'session'}`);
  colorInput.addEventListener('input', () => o.onColor(colorInput.value));
  swatch.append(colorInput);

  li.append(st, text, meta, actions, swatch);

  li.addEventListener('click', (e) => {
    // The swatch opens the native picker; it must not also switch sessions.
    if (e.target === colorInput || e.target === swatch) return;
    o.onSelect();
  });

  applyState(li, s, o);
  return li;
}

// updateSessionRow patches an existing row. Exists for the same reason
// app/sidebar.ts's updateSidebarSelection does: a full rebuild replaces the
// <li> between two clicks and eats the dblclick pair that starts a rename.
export function updateSessionRow(
  el: HTMLLIElement,
  s: SessionInfo,
  next: SessionRowState,
): void {
  applyState(el, s, next);
}

function applyState(
  el: HTMLLIElement,
  s: SessionInfo,
  next: SessionRowState,
): void {
  el.dataset.state = next.state;
  if (next.selected) el.dataset.selected = '';
  else delete el.dataset.selected;
  if (next.minimized) el.dataset.minimized = '';
  else delete el.dataset.minimized;

  const name = el.querySelector<HTMLElement>('.hv-session-row__name');
  // Absent while an inline rename has swapped the label for its <input>.
  if (name) name.textContent = s.name ?? '';

  const sub = el.querySelector<HTMLElement>('.hv-session-row__sub');
  if (sub) {
    const t = subtitleFor(s, next.state);
    sub.textContent = t;
    sub.title = t;
  }

  const st = el.querySelector<SVGSVGElement>('.hv-session-row__state');
  if (st) updateStateIcon(st, next.state);

  const minBtn = el.querySelector<HTMLButtonElement>(
    '[data-action="minimize"]',
  );
  if (minBtn) {
    const label = `${next.minimized ? 'Restore' : 'Minimize'} ${s.name ?? 'session'}`;
    minBtn.setAttribute('aria-label', label);
    minBtn.title = label;
    minBtn.replaceChildren(icon(next.minimized ? 'plus' : 'minus'));
  }

  const meta = el.querySelector<HTMLElement>('.hv-session-row__meta');
  const existing = el.querySelector('.hv-kbd');
  if (existing) existing.remove();
  if (meta && next.index !== null) meta.prepend(kbd(`[${next.index}]`));
}
