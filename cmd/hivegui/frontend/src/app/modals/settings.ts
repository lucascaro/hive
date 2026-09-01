// ---------- settings (appearance + custom agents + updates) ----------
//
// The panel is built here rather than declared in index.html: it is the
// only way the dialog and field primitives can own the markup, and the
// Appearance section's controls are data-driven off PRESETS anyway.
// The ids the keyboard pipeline and the e2e specs key off are set
// explicitly and are part of this module's contract.
//
// The agent list is edited as a local draft and written in one
// SaveCustomAgents call. Go owns validation and ID assignment — new
// rows are saved with an empty id and come back slugged. IDs are
// deliberately not editable: registry entries persist only the agent
// id, so changing one would break revive for every session already
// created with that agent.
//
// Appearance is NOT part of that draft. It is a local preference with
// no round-trip and nothing to validate, so it applies and persists on
// change — you cannot choose a theme you are not allowed to look at.
// Cancel therefore does not revert it, and the section says so.

import {
  EventsOn,
  GetUpdateSettings,
  ListCustomAgents,
  PickDirectory,
  SaveCustomAgents,
  SaveUpdateSettings,
  SourceRepoStatusFor,
  StartUpdate,
  UpdateStatus,
} from '../../bridge.js';
import { isMac } from '../../lib/platform.js';
import {
  CHANNEL_LATEST,
  CHANNEL_RELEASE,
  updateButtonState,
} from '../../lib/update-state.js';
import {
  applyOverrides,
  applyTheme,
  PRESETS,
  readOverrides,
  readTheme,
  sanitizeOverrides,
  THEME_KEY,
  type ThemeName,
  writeOverrides,
} from '../../theme/theme.js';
import { button } from '../../ui/button.js';
import { dialog } from '../../ui/dialog.js';
import {
  colorInput,
  errorSlot,
  field,
  selectInput,
  textareaInput,
  textInput,
} from '../../ui/field.js';
import { iconButton } from '../../ui/icon-button.js';
import { applyUpdateAndRestart } from '../banners.js';
import { applyXtermTheme } from '../session-term.js';
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

const DEFAULT_COLOR = '#64748b';

function hintPara(text: string): HTMLParagraphElement {
  const p = document.createElement('p');
  p.className = 'settings-hint';
  p.textContent = text;
  return p;
}

function heading(text: string): HTMLElement {
  const h = document.createElement('h4');
  h.textContent = text;
  return h;
}

// ---------- appearance ----------

const themeError = errorSlot('settings-overrides-error');

const themeSelect = selectInput({
  id: 'settings-theme',
  ariaLabel: 'Theme',
  options: PRESETS.map((p) => ({ value: p.id, label: p.label })),
  onChange: (value) => selectPreset(value as ThemeName),
});

const overridesInput = textareaInput({
  id: 'settings-overrides',
  ariaLabel: 'Custom tokens',
  rows: 4,
  placeholder: '--accent: #7aa2f7;',
  onInput: (value) => applyUserOverrides(value),
});

// One place stamps the theme, so the three things that must happen
// together cannot drift apart: the attribute, the terminals (xterm
// caches its palette; a CSS change alone leaves every open session on
// the old colours), and the store.
function selectPreset(name: ThemeName): void {
  applyTheme(name);
  applyXtermTheme();
  try {
    localStorage.setItem(THEME_KEY, name);
  } catch {
    // Denied storage: applied for this session, not remembered.
  }
}

// applyUserOverrides runs on every keystroke, which is what makes the
// box usable — but it must never leave a half-typed line showing as an
// error while the user is still typing it. Rejected lines are reported;
// accepted ones apply. Typing "--acc" reports one rejected line and
// changes nothing, which is the honest answer.
function applyUserOverrides(raw: string): void {
  const { css, rejected } = sanitizeOverrides(raw);
  applyOverrides(css);
  applyXtermTheme();
  writeOverrides(css);
  themeError.show(
    rejected.length === 0
      ? ''
      : `Ignored ${rejected.length} line(s) — only "--token: value;" declarations are allowed: ${rejected.join(' / ')}`,
  );
}

function appearanceSection(): Node[] {
  return [
    heading('Appearance'),
    hintPara(
      'Applies as you change it, and is remembered. Cancel does not undo it.',
    ),
    field('Theme', themeSelect),
    hintPara(
      'Override any design token, one declaration per line. Fonts must already be installed on this machine.',
    ),
    field('Custom tokens', overridesInput),
    themeError.el,
  ];
}

// ---------- custom agents ----------

const listEl = document.createElement('div');
listEl.id = 'settings-agents-list';

const agentsError = errorSlot('settings-error');
// The e2e and DOM specs assert on `.settings-error`; the primitive's
// own class carries the styling.
agentsError.el.classList.add('settings-error');

const addBtn = button({
  label: 'Add agent',
  icon: 'plus',
  onClick: () => addAgentRow(),
});
addBtn.id = 'settings-agent-add';

function agentsSection(): Node[] {
  return [
    heading('Custom agents'),
    hintPara(
      'Define your own tools — a command and its arguments. They appear in the new-session menu alongside the built-ins.',
    ),
    listEl,
    addBtn,
    agentsError.el,
  ];
}

// ---------- updates ----------

const channelEl = selectInput({
  id: 'settings-update-channel',
  ariaLabel: 'Update channel',
  options: [
    { value: CHANNEL_RELEASE, label: 'Release — tagged versions' },
    { value: CHANNEL_LATEST, label: 'Latest — tip of your checkout' },
  ],
});

const sourceRepoEl = textInput({
  id: 'settings-source-repo',
  ariaLabel: 'Source repo',
  placeholder: 'Detected automatically',
});
sourceRepoEl.setAttribute('aria-describedby', 'settings-source-repo-hint');

const sourceRepoBrowse = button({
  label: 'Browse…',
  onClick: () => browseSourceRepo(),
});
sourceRepoBrowse.id = 'settings-source-repo-browse';

// The row is a container, not a field: it holds the labelled input and
// the Browse button on one line, and is hidden wholesale on the release
// channel (the e2e spec toggles on #settings-source-repo-row).
const sourceRepoRow = document.createElement('div');
sourceRepoRow.id = 'settings-source-repo-row';
sourceRepoRow.className = 'settings-field';
sourceRepoRow.append(field('Source repo', sourceRepoEl), sourceRepoBrowse);

const sourceRepoHint = hintPara('');
sourceRepoHint.id = 'settings-source-repo-hint';

const updateActionEl = button({ label: 'Update', onClick: () => runUpdate() });
updateActionEl.id = 'settings-update-action';
const updateActionLabel = updateActionEl.querySelector(
  '.hv-button__label',
) as HTMLElement;

const updateStatusEl = document.createElement('span');
updateStatusEl.id = 'settings-update-status';
updateStatusEl.className = 'settings-hint';
updateStatusEl.setAttribute('role', 'status');

// Updates sits outside the scrolling part of the body, at its natural
// height: a dozen custom agents must never push the channel picker
// below the fold (test/e2e/settings.spec.ts).
function updatesSection(): HTMLElement {
  const actionRow = document.createElement('div');
  actionRow.className = 'settings-field';
  actionRow.append(updateActionEl, updateStatusEl);
  const section = document.createElement('section');
  section.id = 'settings-updates';
  section.append(
    heading('Updates'),
    hintPara(
      'Hive checks for updates in the background. Nothing is downloaded or built until you press Update.',
    ),
    field('Channel', channelEl),
    sourceRepoRow,
    sourceRepoHint,
    actionRow,
  );
  return section;
}

// Everything above Updates scrolls together.
function scrollSection(): HTMLElement {
  const el = document.createElement('div');
  el.id = 'settings-scroll';
  el.append(...appearanceSection(), ...agentsSection());
  return el;
}

// ---------- the dialog ----------

const saveBtn = button({
  label: 'Save',
  kind: 'primary',
  onClick: () => saveSettings(),
});
saveBtn.id = 'settings-save';
const cancelBtn = button({ label: 'Cancel', onClick: () => closeSettings() });
cancelBtn.id = 'settings-cancel';

const dlg = dialog({
  id: 'settings',
  title: 'Settings',
  size: 'md',
  body: [scrollSection(), updatesSection()],
  actions: [cancelBtn, saveBtn],
  onClose: () => closeSettings(),
});
// keyboard.ts and the tests import this; it must stay the root element.
export const settingsEl = dlg.el;
// The primitive's panel and close button keep the ids the e2e specs use.
dlg.panel.id = 'settings-panel';
dlg.el.querySelector('.hv-dialog__close')?.setAttribute('id', 'settings-close');

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
  agentsError.show(msg);
}

function addAgentRow() {
  draft.push({ id: '', name: '', cmd: [], color: DEFAULT_COLOR });
  showError('');
  render();
  listEl
    .querySelector<HTMLElement>(
      '.settings-agent-row:last-child .settings-agent-name',
    )
    ?.focus();
}

function deleteAgentRow(i: number) {
  draft.splice(i, 1);
  showError('');
  render();
  // render() destroyed the button that had focus, dropping it to
  // <body> — from there the Tab trap has no boundary to wrap and the
  // next Tab walks behind the backdrop. Put focus back on the row that
  // took this one's place, or on "Add agent".
  const dels = listEl.querySelectorAll<HTMLElement>('.settings-agent-delete');
  (dels[Math.min(i, dels.length - 1)] ?? addBtn).focus();
}

function render() {
  listEl.replaceChildren();
  if (loading) {
    listEl.append(hintPara('Loading…'));
    return;
  }
  if (draft.length === 0) {
    listEl.append(hintPara('No custom agents yet.'));
    return;
  }

  draft.forEach((a, i) => {
    const row = document.createElement('div');
    row.className = 'settings-agent-row';

    const color = colorInput({
      value: a.color || DEFAULT_COLOR,
      ariaLabel: 'Agent color',
      onInput: (v) => {
        draft[i].color = v;
      },
    });
    const name = textInput({
      className: 'settings-agent-name',
      placeholder: 'Name (e.g. Claude Lite)',
      ariaLabel: 'Agent name',
      value: a.name || '',
      onInput: (v) => {
        draft[i].name = v;
      },
    });
    const cmd = textInput({
      className: 'settings-agent-cmd',
      placeholder: 'Command (e.g. claude --model haiku)',
      ariaLabel: 'Agent command',
      value: (a.cmd || []).join(' '),
      onInput: (v) => {
        draft[i].cmd = splitCommand(v);
      },
    });
    const del = iconButton({
      icon: 'x',
      label: `Delete ${a.name || 'agent'}`,
      className: 'settings-agent-delete',
      onClick: () => deleteAgentRow(i),
    });

    row.append(color.el, name, cmd, del);
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
  if (dlg.isOpen()) return;
  // Seeded from the store, not from the last open: the theme can change
  // elsewhere (the `system` preset follows the OS).
  themeSelect.value = readTheme();
  overridesInput.value = readOverrides().replace(/\n\s*/g, '\n');
  themeError.clear();
  showError('');
  draft = [];
  loading = true;
  loadFailed = false;
  const token = ++openToken;
  setEditingEnabled(false);
  render();
  dlg.show();
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

function browseSourceRepo() {
  PickDirectory(updateDraft.sourceRepo)
    .then((dir) => {
      if (!dir) return; // cancelled
      updateDraft.sourceRepo = dir;
      sourceRepoEl.value = dir;
      renderSourceRepoRow();
    })
    .catch((err) => showError(String(err?.message || err)));
}

function runUpdate() {
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
}

function renderUpdateAction(info: main.UpdateInfo | null) {
  const btn = updateButtonState(info, isMac);
  updateStatusEl.textContent = btn.status;
  updateActionEl.style.display = btn.label ? '' : 'none';
  updateActionLabel.textContent = btn.label;
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
  dlg.hide();
  agentsError.clear();
  draft = [];
  updateDraft = { channel: CHANNEL_RELEASE, sourceRepo: '' };
  deps.refocusActiveTerm();
}

// setEditingEnabled gates the controls that can mutate agents.json.
// They stay disabled while a load is in flight or after one failed.
function setEditingEnabled(on: boolean) {
  saveBtn.disabled = !on;
  addBtn.disabled = !on;
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
  document.getElementById('app')?.append(settingsEl);
  channelEl.addEventListener('change', () => {
    updateDraft.channel =
      channelEl.value === CHANNEL_LATEST ? CHANNEL_LATEST : CHANNEL_RELEASE;
    renderSourceRepoRow();
  });
  sourceRepoEl.addEventListener('input', () => {
    updateDraft.sourceRepo = sourceRepoEl.value;
    renderSourceRepoRow();
  });
  // Staging runs in Go and outlives this modal, so the button follows
  // the same progress events the banner does rather than any local state.
  EventsOn('update:progress', (info: main.UpdateInfo | null) =>
    renderUpdateAction(info),
  );
  // Enter in a text field saves, as before. Escape and the backdrop are
  // the dialog primitive's. The Appearance textarea is excluded by the
  // type check: Enter there is a newline, and this listener would
  // otherwise turn "next line of overrides" into "save and close".
  settingsEl.addEventListener('keydown', (e) => {
    if (
      e.key === 'Enter' &&
      e.target instanceof HTMLInputElement &&
      e.target.type === 'text'
    ) {
      e.preventDefault();
      saveSettings();
    }
  });
}
