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

import {
  ListCustomAgents,
  SaveCustomAgents,
  GetUpdateSettings,
  SaveUpdateSettings,
  SourceRepoStatusFor,
  UpdateStatus,
  StartUpdate,
  PickDirectory,
  EventsOn,
} from '../../bridge.js';
import { applyUpdateAndRestart } from '../banners.js';
import { registerModal } from './registry.js';
import { pageEl } from '../el.js';
import { icon } from '../../ui/icon.js';
import { iconButton } from '../../ui/icon-button.js';
import { isMac } from '../../lib/platform.js';
import {
  updateButtonState,
  CHANNEL_LATEST,
  CHANNEL_RELEASE,
} from '../../lib/update-state.js';
// Type-only, so the generated module is erased before Vite resolves it.
import type { main } from '../../../wailsjs/go/models';

// Narrow on purpose: this modal needs exactly two callbacks off the
// focus pipeline, so it names those two rather than the whole module.
export interface SettingsDeps {
  setFocusedTile: (id: string | null) => void;
  refocusActiveTerm: () => void;
}

let deps: SettingsDeps = {
  setFocusedTile: () => {},
  refocusActiveTerm: () => {},
};

export const settingsEl = pageEl('settings');
const listEl = pageEl('settings-agents-list');
const errorEl = pageEl('settings-error');
const channelEl = pageEl<HTMLSelectElement>('settings-update-channel');
const sourceRepoEl = pageEl<HTMLInputElement>('settings-source-repo');
const sourceRepoRow = pageEl('settings-source-repo-row');
const sourceRepoBrowse = pageEl<HTMLButtonElement>(
  'settings-source-repo-browse',
);
const sourceRepoHint = pageEl('settings-source-repo-hint');
const updateActionEl = pageEl<HTMLButtonElement>('settings-update-action');
const updateStatusEl = pageEl('settings-update-status');

const DEFAULT_COLOR = '#64748b';

// draft is the in-progress edit; discarded on cancel. Rows are plain
// objects, not main.CustomAgent instances — the generated class is a
// data shape and Go re-slugs the ids on save.
let draft: main.CustomAgent[] = [];

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
 *
 * Takes `string | null` because a test asserts splitCommand(null) is [];
 * the String(line || '') guard is part of the contract, not defensive
 * padding around a narrower one.
 */
export function splitCommand(line: string | null): string[] {
  return String(line || '')
    .trim()
    .split(/\s+/)
    .filter(Boolean);
}

function showError(msg: string) {
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

    const del = iconButton({
      icon: 'x',
      label: `Delete ${a.name || 'agent'}`,
      className: 'settings-agent-delete',
      onClick: () => {
        draft.splice(i, 1);
        showError('');
        render();
        // render() destroyed the button that had focus, dropping it to
        // <body> — from there the Tab trap has no boundary to wrap and
        // the next Tab walks behind the backdrop. Put focus back on the
        // row that took this one's place, or on "+ Add agent".
        const dels = listEl.querySelectorAll<HTMLElement>(
          '.settings-agent-delete',
        );
        const next =
          dels[Math.min(i, dels.length - 1)] ??
          document.getElementById('settings-agent-add');
        next?.focus();
      },
    });

    row.append(color, name, cmd, del);
    listEl.append(row);
  });
}

export function openSettings() {
  // Re-entry must not discard an in-progress draft. This is reachable
  // on macOS: the native File ▸ Settings… accelerator consumes ⌘,
  // before the webview's keydown listener sees it (same precedence
  // that makes the '?' branch in keyboard.ts dead on darwin, per
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

  loadUpdateSection(token);

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

// ---------- updates section ----------
//
// The channel and source-repo inputs join the modal's draft/save cycle
// (Cancel discards them). The Update button deliberately does NOT: it
// starts real work in Go, and losing a running download because the
// user closed the dialog would be surprising. Its state is read back
// from Go rather than tracked here, which is also what keeps it in
// agreement with the banner.

// updateDraft mirrors the two saved fields while the modal is open.
let updateDraft = { channel: CHANNEL_RELEASE, sourceRepo: '' };

// SourceRepoStatusFor stats its way up a directory tree, so firing it
// per keystroke is both wasteful and unordered — a slow answer for
// "/Use" could land after the fast one for "/Users/me/hive" and
// overwrite it with a stale error. Debounce, and stamp each request so
// only the newest reply is allowed to render.
let sourceRepoProbe: ReturnType<typeof setTimeout> | null = null;
let sourceRepoToken = 0;
const SOURCE_REPO_DEBOUNCE_MS = 250;

function renderSourceRepoRow() {
  const isLatest = updateDraft.channel === CHANNEL_LATEST;
  sourceRepoRow.classList.toggle('hidden', !isLatest);
  if (sourceRepoProbe) {
    clearTimeout(sourceRepoProbe);
    sourceRepoProbe = null;
  }
  const token = ++sourceRepoToken;
  if (!isLatest) {
    sourceRepoHint.textContent = '';
    return;
  }
  const path = updateDraft.sourceRepo;
  sourceRepoProbe = setTimeout(() => {
    sourceRepoProbe = null;
    SourceRepoStatusFor(path)
      .then((st) => {
        if (token !== sourceRepoToken || !st) return;
        if (st.error) {
          sourceRepoHint.textContent = st.error;
          return;
        }
        sourceRepoHint.textContent = st.detected
          ? `Detected ${st.path}`
          : `Using ${st.path}`;
      })
      .catch((err) => {
        if (token !== sourceRepoToken) return;
        sourceRepoHint.textContent = String(err?.message || err);
      });
  }, SOURCE_REPO_DEBOUNCE_MS);
}

function renderUpdateAction(info: main.UpdateInfo | null) {
  const btn = updateButtonState(info, isMac);
  updateStatusEl.textContent = btn.status;
  updateActionEl.style.display = btn.label ? '' : 'none';
  updateActionEl.textContent = btn.label;
  updateActionEl.disabled = btn.disabled;
  updateActionEl.dataset.action = btn.action;
  updateActionEl.dataset.version = info?.latest || '';
}

function refreshUpdateAction() {
  UpdateStatus()
    .then(renderUpdateAction)
    .catch(() => renderUpdateAction(null));
}

function loadUpdateSection(token: number) {
  GetUpdateSettings()
    .then((s) => {
      // Same staleness guard the agent list uses: closed and reopened
      // before this resolved means a newer draft is on screen, and this
      // response would overwrite it.
      if (token !== openToken) return;
      updateDraft = {
        channel:
          s?.channel === CHANNEL_LATEST ? CHANNEL_LATEST : CHANNEL_RELEASE,
        sourceRepo: s?.source_repo || '',
      };
      channelEl.value = updateDraft.channel;
      sourceRepoEl.value = updateDraft.sourceRepo;
      renderSourceRepoRow();
    })
    // A corrupt update.json must not be silently replaced by defaults on
    // the next save, so surface it the way a bad agents.json is.
    .catch((err) => {
      if (token !== openToken) return;
      showError(String(err?.message || err));
    });
  refreshUpdateAction();
}

export function closeSettings() {
  openToken += 1; // invalidate any in-flight load
  loading = false;
  loadFailed = false;
  settingsEl.classList.add('hidden');
  showError('');
  draft = [];
  updateDraft = { channel: CHANNEL_RELEASE, sourceRepo: '' };
  deps.refocusActiveTerm();
}

// setEditingEnabled gates the controls that can mutate agents.json.
// They stay disabled while a load is in flight or after one failed.
function setEditingEnabled(on: boolean) {
  const save = pageEl<HTMLButtonElement>('settings-save');
  const add = pageEl<HTMLButtonElement>('settings-agent-add');
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
  // Both writes are validated Go-side and either can be rejected, so one
  // of them is going to be the "partial save" on failure. Agents go
  // first because that is the recoverable order: a rejected channel
  // leaves the modal open on the row that caused it, with the agent
  // edits already durable. The reverse strands a saved channel behind a
  // Cancel that no longer discards it.
  SaveCustomAgents(payload)
    .then(() =>
      SaveUpdateSettings({
        channel: updateDraft.channel,
        source_repo: updateDraft.sourceRepo,
      } as main.UpdateSettings),
    )
    .then(closeSettings)
    // Go returns one joined error naming every rejected entry; show it
    // verbatim rather than paraphrasing it into something vaguer.
    .catch((err) => showError(String(err?.message || err)));
}

export function initSettings(injected: SettingsDeps) {
  deps = injected;
  registerModal(settingsEl);
  const settingsCloseBtn = pageEl('settings-close');
  settingsCloseBtn.replaceChildren(icon('x'));
  settingsCloseBtn.addEventListener('click', closeSettings);
  pageEl('settings-cancel').addEventListener('click', closeSettings);
  pageEl('settings-save').addEventListener('click', saveSettings);
  channelEl.addEventListener('change', () => {
    updateDraft.channel =
      channelEl.value === CHANNEL_LATEST ? CHANNEL_LATEST : CHANNEL_RELEASE;
    renderSourceRepoRow();
  });
  sourceRepoEl.addEventListener('input', () => {
    updateDraft.sourceRepo = sourceRepoEl.value;
    renderSourceRepoRow();
  });
  sourceRepoBrowse.addEventListener('click', () => {
    PickDirectory(updateDraft.sourceRepo)
      .then((dir) => {
        if (!dir) return; // cancelled
        updateDraft.sourceRepo = dir;
        sourceRepoEl.value = dir;
        renderSourceRepoRow();
      })
      .catch((err) => showError(String(err?.message || err)));
  });
  updateActionEl.addEventListener('click', () => {
    const action = updateActionEl.dataset.action;
    if (action === 'restart') {
      // Shared with the banner: confirm overlay + re-entrancy guard +
      // the daemon-restart flag live there, and applying is exactly as
      // destructive from here as it is from the banner.
      void applyUpdateAndRestart(updateActionEl.dataset.version || '');
      return;
    }
    if (action === 'start') {
      updateActionEl.disabled = true;
      // A synchronous refusal from StartUpdate emits no update:progress
      // event, so re-enable the button here rather than leaving it dead.
      StartUpdate().catch((err) => {
        updateActionEl.disabled = false;
        showError(String(err?.message || err));
      });
    }
  });
  // Staging runs in Go and outlives this modal, so the button follows
  // the same progress events the banner does rather than any local state.
  EventsOn('update:progress', (info: main.UpdateInfo | null) =>
    renderUpdateAction(info),
  );
  pageEl('settings-agent-add').addEventListener('click', () => {
    draft.push({ id: '', name: '', cmd: [], color: DEFAULT_COLOR });
    showError('');
    render();
    listEl
      .querySelector<HTMLElement>(
        '.settings-agent-row:last-child .settings-agent-name',
      )
      ?.focus();
  });
  settingsEl.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
      // Consume it. This listener fires before the window handler in
      // keyboard.ts, which would otherwise see an already-hidden
      // dialog, fall past its settings gate, and spend the same
      // Escape on whatever is behind the modal.
      e.preventDefault();
      e.stopPropagation();
      closeSettings();
    } else if (
      e.key === 'Enter' &&
      e.target instanceof HTMLInputElement &&
      e.target.type === 'text'
    ) {
      e.preventDefault();
      saveSettings();
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
