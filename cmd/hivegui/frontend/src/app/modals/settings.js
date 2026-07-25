// ---------- settings (custom agents) ----------
//
// Follows the project-editor pattern: module-scope element refs, an
// open/close pair, and focus callbacks injected via init.
//
// The agent list is edited as a local draft and written in one
// SaveCustomAgents call. Go owns validation and ID assignment — new
// rows are saved with an empty id and come back slugged. IDs are
// deliberately not editable: registry entries persist only the agent
// id, so changing one would break revive for every session already
// created with that agent.

import { ListCustomAgents, SaveCustomAgents } from '../../bridge.js';
import { registerModal } from './registry.js';

let deps = {
  setFocusedTile: () => {},
  refocusActiveTerm: () => {},
};

export const settingsEl = document.getElementById('settings');
const listEl = document.getElementById('settings-agents-list');
const errorEl = document.getElementById('settings-error');

const DEFAULT_COLOR = '#64748b';

// draft is the in-progress edit; discarded on cancel.
let draft = [];

// loading distinguishes "still fetching" from "genuinely empty" so the
// empty state never renders over a list that is about to arrive.
let loading = false;

// loadFailed blocks Save after a failed read. Go cannot tell a
// deliberate "delete every agent" from a draft that is empty only
// because agents.json would not parse, so the refusal has to live
// here, where that distinction is still known.
let loadFailed = false;

// openToken invalidates an in-flight load when the modal is closed and
// reopened before it resolves, so a stale response cannot overwrite a
// newer draft.
let openToken = 0;

/** Splits a command line into argv on whitespace.
 *
 * ponytail: whitespace split, no quote handling. There is no
 * shell-word splitter in this repo and `claude --model haiku` is the
 * shape people actually type. agents.json stores a real array, so an
 * argument containing spaces stays hand-editable in the file. Upgrade
 * path if this bites: a real tokenizer here and in Go's validator.
 */
export function splitCommand(line) {
  return String(line || '')
    .trim()
    .split(/\s+/)
    .filter(Boolean);
}

function showError(msg) {
  errorEl.textContent = msg;
  errorEl.classList.toggle('hidden', !msg);
}

function render() {
  listEl.replaceChildren();
  if (loading) {
    const busy = document.createElement('p');
    busy.className = 'settings-hint';
    busy.textContent = 'Loading…';
    listEl.append(busy);
    return;
  }
  if (draft.length === 0) {
    const empty = document.createElement('p');
    empty.className = 'settings-hint';
    empty.textContent = 'No custom agents yet.';
    listEl.append(empty);
    return;
  }

  draft.forEach((a, i) => {
    const row = document.createElement('div');
    row.className = 'settings-agent-row';

    const color = document.createElement('input');
    color.type = 'color';
    color.value = a.color || DEFAULT_COLOR;
    color.setAttribute('aria-label', 'Agent color');
    color.addEventListener('input', () => {
      draft[i].color = color.value;
    });

    const name = document.createElement('input');
    name.type = 'text';
    name.className = 'settings-agent-name';
    name.placeholder = 'Name (e.g. Claude Lite)';
    name.autocomplete = 'off';
    name.value = a.name || '';
    name.setAttribute('aria-label', 'Agent name');
    name.addEventListener('input', () => {
      draft[i].name = name.value;
    });

    const cmd = document.createElement('input');
    cmd.type = 'text';
    cmd.className = 'settings-agent-cmd';
    cmd.placeholder = 'Command (e.g. claude --model haiku)';
    cmd.autocomplete = 'off';
    cmd.value = (a.cmd || []).join(' ');
    cmd.setAttribute('aria-label', 'Agent command');
    cmd.addEventListener('input', () => {
      draft[i].cmd = splitCommand(cmd.value);
    });

    const del = document.createElement('button');
    del.type = 'button';
    del.className = 'settings-agent-delete';
    del.textContent = '×';
    del.title = 'Delete agent';
    del.setAttribute('aria-label', `Delete ${a.name || 'agent'}`);
    del.addEventListener('click', () => {
      draft.splice(i, 1);
      showError('');
      render();
      // render() destroyed the button that had focus, dropping it to
      // <body> — from there the Tab trap has no boundary to wrap and
      // the next Tab walks behind the backdrop. Put focus back on the
      // row that took this one's place, or on "+ Add agent".
      const dels = listEl.querySelectorAll('.settings-agent-delete');
      const next = dels[Math.min(i, dels.length - 1)];
      (next || document.getElementById('settings-agent-add'))?.focus();
    });

    row.append(color, name, cmd, del);
    listEl.append(row);
  });
}

export function openSettings() {
  // Re-entry must not discard an in-progress draft. This is reachable
  // on macOS: the native File ▸ Settings… accelerator consumes ⌘,
  // before the webview's keydown listener sees it (same precedence
  // that makes the '?' branch in keyboard.js dead on darwin, per
  // menu_darwin.go), so pressing ⌘, with the modal already open
  // arrives here as menu:settings rather than as the toggle-to-close
  // in the keydown gate. Without this guard the `draft = []` below
  // silently wipes the user's unsaved edits.
  if (!settingsEl.classList.contains('hidden')) return;
  showError('');
  draft = [];
  loading = true;
  loadFailed = false;
  const token = ++openToken;
  setEditingEnabled(false);
  render();
  settingsEl.classList.remove('hidden');
  // Drop the active tile's visual focus and pull focus into the
  // dialog — same discipline as the help overlay. Without this, focus
  // stays on the terminal and keystrokes leak behind the backdrop.
  deps.setFocusedTile(null);
  document.getElementById('settings-close')?.focus();

  ListCustomAgents()
    .then((list) => {
      if (token !== openToken) return; // closed and reopened; stale
      loading = false;
      setEditingEnabled(true);
      draft = (list || []).map((a) => ({
        id: a.id || '',
        name: a.name || '',
        cmd: a.cmd || [],
        color: a.color || DEFAULT_COLOR,
      }));
      render();
    })
    .catch((err) => {
      if (token !== openToken) return;
      loading = false;
      // Editing stays disabled: the draft is empty only because the
      // read failed, and saving it would destroy agents.json.
      loadFailed = true;
      render();
      showError(
        `Could not read agents.json — fix or move the file, then reopen Settings. (${String(err?.message || err)})`,
      );
    });
}

export function closeSettings() {
  openToken += 1; // invalidate any in-flight load
  loading = false;
  loadFailed = false;
  settingsEl.classList.add('hidden');
  showError('');
  draft = [];
  deps.refocusActiveTerm();
}

// setEditingEnabled gates the controls that can mutate agents.json.
// They stay disabled while a load is in flight or after one failed.
function setEditingEnabled(on) {
  const save = document.getElementById('settings-save');
  const add = document.getElementById('settings-agent-add');
  if (save) save.disabled = !on;
  if (add) add.disabled = !on;
}

function saveSettings() {
  // A draft that is empty because the file would not parse must never
  // be written back over it.
  if (loading || loadFailed) return;
  // Drop fully-blank rows so an accidental "+ Add agent" doesn't
  // block the save with a validation error.
  const payload = draft.filter((a) => a.name.trim() || a.cmd.length);
  SaveCustomAgents(payload)
    .then(closeSettings)
    // Go returns one joined error naming every rejected entry; show it
    // verbatim rather than paraphrasing it into something vaguer.
    .catch((err) => showError(String(err?.message || err)));
}

export function initSettings(injected) {
  deps = injected;
  registerModal(settingsEl);
  document
    .getElementById('settings-close')
    .addEventListener('click', closeSettings);
  document
    .getElementById('settings-cancel')
    .addEventListener('click', closeSettings);
  document
    .getElementById('settings-save')
    .addEventListener('click', saveSettings);
  document
    .getElementById('settings-agent-add')
    .addEventListener('click', () => {
      draft.push({ id: '', name: '', cmd: [], color: DEFAULT_COLOR });
      showError('');
      render();
      listEl
        .querySelector('.settings-agent-row:last-child .settings-agent-name')
        ?.focus();
    });
  settingsEl.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
      // Consume it. This listener fires before the window handler in
      // keyboard.js, which would otherwise see an already-hidden
      // dialog, fall past its settings gate, and spend the same
      // Escape on whatever is behind the modal.
      e.preventDefault();
      e.stopPropagation();
      closeSettings();
    } else if (
      e.key === 'Enter' &&
      e.target.tagName === 'INPUT' &&
      e.target.type === 'text'
    ) {
      e.preventDefault();
      saveSettings();
    } else if (e.key === 'Tab') {
      // aria-modal promises focus stays inside the dialog. Unlike the
      // help overlay (one focusable element, so it just pins focus),
      // this is a form — Tab must still walk the fields, so only the
      // two boundaries wrap. Without this, Tab past Save lands on a
      // terminal behind the backdrop and keystrokes leak into it.
      const focusable = [
        ...settingsEl.querySelectorAll(
          'button, input, [tabindex]:not([tabindex="-1"])',
        ),
      ].filter((el) => !el.disabled && el.offsetParent !== null);
      if (focusable.length === 0) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    }
  });
  // Click on the backdrop (not the panel) closes. Both ends of the
  // gesture must land on the backdrop: a text-selection drag that
  // starts inside an input and releases outside the panel dispatches
  // its click on the nearest common ancestor — the backdrop — so
  // testing the click alone discards the whole draft mid-edit. Same
  // data-loss class as the openSettings re-entry guard above.
  let downOnBackdrop = false;
  settingsEl.addEventListener('mousedown', (e) => {
    downOnBackdrop = e.target === settingsEl;
  });
  settingsEl.addEventListener('click', (e) => {
    if (downOnBackdrop && e.target === settingsEl) closeSettings();
    downOnBackdrop = false;
  });
}
