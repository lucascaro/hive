// ---------- settings (appearance + custom agents + updates) ----------
//
// React port of src/app/modals/settings.ts. The ids the keyboard
// pipeline and the e2e specs key off are set explicitly here and are
// part of this component's contract.
//
// The agent list is edited as a local draft and written in one
// SaveCustomAgents call. Go owns validation and ID assignment — new rows
// are saved with an empty id and come back slugged. IDs are deliberately
// not editable: registry entries persist only the agent id, so changing
// one would break revive for every session already created with it.
//
// Appearance is NOT part of that draft. It is a local preference with no
// round-trip and nothing to validate, so it applies and persists on
// change — you cannot choose a theme you are not allowed to look at.
// Cancel therefore does not revert it, and the section says so.
//
// Per-open state resets by construction: the body is mounted only while
// the modal is open, so the staleness tokens the imperative version
// needed (openToken) are now the unmount guard on each request.

import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from 'react';
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
  writeOverrides,
  type ThemeName,
} from '../../theme/theme.js';
import { applyUpdateAndRestart } from '../../app/banners.js';
import { applyXtermTheme } from '../../app/session-term.js';
import { closeSettings, splitCommand } from '../../app/modals/settings.js';
import { useAppStore } from '../../store/store.js';
import { Button } from '../Button.js';
import { IconButton } from '../IconButton.js';
import { ModalShell } from './ModalShell.js';
// Type-only, so the generated module is erased before Vite resolves it.
import type { main } from '../../../wailsjs/go/models';

const DEFAULT_COLOR = '#64748b';

// Applying overrides is not free: a style invalidation, then a
// getComputedStyle and a palette rebuild on EVERY live terminal, then a
// synchronous localStorage write. applyXtermTheme's own comment says
// "not per frame"; a keystroke is the per-frame case, so the work trails
// the typing by one short pause instead of riding every character.
const OVERRIDES_DEBOUNCE_MS = 150;
// SourceRepoStatusFor stats its way up a directory tree, so firing it per
// keystroke is both wasteful and unordered.
const SOURCE_REPO_DEBOUNCE_MS = 250;

export function Settings({ root }: { root: HTMLElement | null }): ReactNode {
  const entry = useAppStore((s) => s.modals.find((m) => m.id === 'settings'));

  // #settings sits outside React's tree, so its open/closed class is
  // applied here. The `hidden` class is load-bearing: registry.ts's
  // anyModalOpen(), keyboard.ts's per-modal gate and every e2e
  // visibility assertion read it.
  useLayoutEffect(() => {
    root?.classList.toggle('hidden', !entry);
  }, [root, entry]);

  if (!entry || !root) return null;
  // Remounted per opening, which is what resets the draft, the load
  // flags and the two debounces — the openToken the imperative version
  // carried is now this key plus a per-request unmount guard.
  return <SettingsDialog key={entry.seq} root={root} />;
}

function SettingsDialog({ root }: { root: HTMLElement }): ReactNode {
  // draft is the in-progress edit; discarded on cancel (this component
  // unmounts). Rows are plain objects, not main.CustomAgent instances —
  // the generated class is a data shape and Go re-slugs the ids on save.
  const [draft, setDraft] = useState<main.CustomAgent[]>([]);
  // loading distinguishes "still fetching" from "genuinely empty" so the
  // empty state never renders over a list that is about to arrive.
  const [loading, setLoading] = useState(true);
  // loadFailed blocks Save after a failed read. Go cannot tell a
  // deliberate "delete every agent" from a draft that is empty only
  // because agents.json would not parse, so the refusal has to live here,
  // where that distinction is still known.
  const [loadFailed, setLoadFailed] = useState(false);
  const [error, setError] = useState('');
  const [theme, setTheme] = useState<ThemeName>(() => readTheme());
  const [overrides, setOverrides] = useState(() =>
    readOverrides().replace(/\n\s*/g, '\n'),
  );
  const [overridesError, setOverridesError] = useState('');
  // updateDraft mirrors the two saved update fields while the modal is
  // open; Cancel discards them like any other draft.
  const [channel, setChannel] = useState(CHANNEL_RELEASE);
  const [sourceRepo, setSourceRepo] = useState('');
  const [sourceRepoHint, setSourceRepoHint] = useState('');
  const [updateInfo, setUpdateInfo] = useState<main.UpdateInfo | null>(null);

  // Staging runs in Go and outlives this modal, so the button follows
  // the same progress events the banner does rather than any local
  // state. The state it starts from is re-read on open (UpdateStatus
  // below) rather than tracked while the dialog is closed.
  useEffect(
    () =>
      EventsOn('update:progress', (info: main.UpdateInfo | null) =>
        setUpdateInfo(info),
      ),
    [],
  );

  const errorRef = useRef<HTMLParagraphElement | null>(null);
  const listRef = useRef<HTMLDivElement | null>(null);
  // The row index whose delete button took the last click. render()
  // destroyed the button that had focus, dropping it to <body> — from
  // there the Tab trap has no boundary to wrap and the next Tab walks
  // behind the backdrop. Put focus back on the row that took this one's
  // place, or on "Add agent".
  const refocusDelete = useRef<number | null>(null);
  // Focus the newly added row's name box, once it exists.
  const focusLastName = useRef(false);

  const editingEnabled = !loading && !loadFailed;

  // ---------- the load, and the focus it lands with ----------

  // Mount-only: this effect IS the open, and re-running it would refetch
  // under an edited draft.
  // biome-ignore lint/correctness/useExhaustiveDependencies: see above
  useEffect(() => {
    let live = true;
    // Drop the active tile's visual focus and pull focus into the
    // dialog — same discipline as the help overlay. Without this, focus
    // stays on the terminal and keystrokes leak behind the backdrop.
    document.getElementById('settings-close')?.focus();

    ListCustomAgents()
      .then((list) => {
        if (!live) return; // closed and reopened; stale
        setLoading(false);
        setDraft(
          (list || []).map((a) => ({
            id: a.id || '',
            name: a.name || '',
            cmd: a.cmd || [],
            color: a.color || DEFAULT_COLOR,
          })) as main.CustomAgent[],
        );
      })
      .catch((err) => {
        if (!live) return;
        setLoading(false);
        // Editing stays disabled: the draft is empty only because the
        // read failed, and saving it would destroy agents.json.
        setLoadFailed(true);
        showError(
          `Could not read agents.json — fix or move the file, then reopen Settings. (${String(err?.message || err)})`,
        );
      });

    GetUpdateSettings()
      .then((s) => {
        if (!live) return;
        setChannel(
          s?.channel === CHANNEL_LATEST ? CHANNEL_LATEST : CHANNEL_RELEASE,
        );
        setSourceRepo(s?.source_repo || '');
      })
      // A corrupt update.json must not be silently replaced by defaults
      // on the next save, so surface it the way a bad agents.json is.
      .catch((err) => {
        if (!live) return;
        showError(String(err?.message || err));
      });

    // Read back from Go rather than tracked here, which is what keeps
    // the button in agreement with the banner.
    UpdateStatus()
      .then((info) => live && setUpdateInfo(info))
      .catch(() => live && setUpdateInfo(null));

    return () => {
      live = false;
    };
  }, []);

  // ---------- errors ----------

  function showError(msg: string) {
    setError(msg);
    // The slot lives in the scrolling region, so a save rejected after
    // the user scrolled down would otherwise land off-screen and read as
    // "the button did nothing". Optional call: jsdom has no layout.
    if (msg)
      queueMicrotask(() =>
        errorRef.current?.scrollIntoView?.({ block: 'nearest' }),
      );
  }

  // ---------- appearance ----------

  // One place stamps the theme, so the three things that must happen
  // together cannot drift apart: the attribute, the terminals (xterm
  // caches its palette; a CSS change alone leaves every open session on
  // the old colours), and the stored choice.
  function selectPreset(name: ThemeName) {
    setTheme(name);
    applyTheme(name);
    applyXtermTheme();
    try {
      localStorage.setItem(THEME_KEY, name);
    } catch {
      // Denied storage: applied for this session, not remembered.
    }
  }

  // Debounced, and cancelled on close by the cleanup: a pending timer
  // would otherwise fire after the dialog is gone with the text as it
  // was, and on a reopen inside the window write that back over what the
  // box now shows.
  const firstOverrides = useRef(true);
  useEffect(() => {
    if (firstOverrides.current) {
      // The mount value is what is already applied; re-applying it would
      // rewrite storage for a dialog the user only looked at.
      firstOverrides.current = false;
      return;
    }
    const t = setTimeout(() => {
      // Rejected lines are reported; accepted ones apply. Typing "--acc"
      // reports one rejected line and changes nothing, which is the
      // honest answer.
      const { css, rejected } = sanitizeOverrides(overrides);
      applyOverrides(css);
      applyXtermTheme();
      writeOverrides(css);
      setOverridesError(
        rejected.length === 0
          ? ''
          : `Ignored ${rejected.length} line(s) — only "--token: value;" declarations are allowed: ${rejected.join(' / ')}`,
      );
    }, OVERRIDES_DEBOUNCE_MS);
    return () => clearTimeout(t);
  }, [overrides]);

  // ---------- updates ----------

  const isLatest = channel === CHANNEL_LATEST;
  useEffect(() => {
    if (!isLatest) {
      setSourceRepoHint('');
      return;
    }
    let live = true;
    const t = setTimeout(() => {
      SourceRepoStatusFor(sourceRepo)
        .then((st) => {
          if (!live || !st) return;
          if (st.error) {
            setSourceRepoHint(st.error);
            return;
          }
          setSourceRepoHint(
            st.detected ? `Detected ${st.path}` : `Using ${st.path}`,
          );
        })
        .catch((err) => {
          if (!live) return;
          setSourceRepoHint(String(err?.message || err));
        });
    }, SOURCE_REPO_DEBOUNCE_MS);
    return () => {
      live = false;
      clearTimeout(t);
    };
  }, [isLatest, sourceRepo]);

  function browseSourceRepo() {
    PickDirectory(sourceRepo)
      .then((dir) => {
        if (!dir) return; // cancelled
        setSourceRepo(dir);
      })
      .catch((err) => showError(String(err?.message || err)));
  }

  const updateBtn = updateButtonState(updateInfo, isMac);
  const [updateBusy, setUpdateBusy] = useState(false);

  function runUpdate() {
    if (updateBtn.action === 'restart') {
      // Shared with the banner: confirm overlay + re-entrancy guard +
      // the daemon-restart flag live there, and applying is exactly as
      // destructive from here as it is from the banner.
      void applyUpdateAndRestart(updateInfo?.latest || '');
      return;
    }
    if (updateBtn.action === 'start') {
      setUpdateBusy(true);
      // A synchronous refusal from StartUpdate emits no update:progress
      // event, so re-enable the button here rather than leaving it dead.
      StartUpdate().catch((err) => {
        setUpdateBusy(false);
        showError(String(err?.message || err));
      });
    }
  }

  // ---------- agents ----------

  function addAgentRow() {
    setDraft((cur) => [
      ...cur,
      { id: '', name: '', cmd: [], color: DEFAULT_COLOR } as main.CustomAgent,
    ]);
    showError('');
    focusLastName.current = true;
  }

  function deleteAgentRow(i: number) {
    setDraft((cur) => cur.filter((_, idx) => idx !== i));
    showError('');
    refocusDelete.current = i;
  }

  function patchAgent(i: number, patch: Partial<main.CustomAgent>) {
    setDraft((cur) =>
      cur.map((a, idx) => (idx === i ? { ...a, ...patch } : a)),
    );
  }

  // The refs are read, never subscribed to: this effect exists to put
  // focus back after the list re-rendered, so `draft` is the whole
  // dependency.
  // biome-ignore lint/correctness/useExhaustiveDependencies: see above
  useEffect(() => {
    if (focusLastName.current) {
      focusLastName.current = false;
      listRef.current
        ?.querySelector<HTMLElement>(
          '.settings-agent-row:last-child .settings-agent-name',
        )
        ?.focus();
      return;
    }
    const i = refocusDelete.current;
    if (i == null) return;
    refocusDelete.current = null;
    const dels =
      listRef.current?.querySelectorAll<HTMLElement>(
        '.settings-agent-delete',
      ) ?? [];
    (
      dels[Math.min(i, dels.length - 1)] ??
      document.getElementById('settings-agent-add')
    )?.focus();
  }, [draft]);

  // ---------- save ----------

  function saveSettings() {
    // A draft that is empty because the file would not parse must never
    // be written back over it.
    if (loading || loadFailed) return;
    // Drop fully-blank rows so an accidental "+ Add agent" doesn't block
    // the save with a validation error.
    const payload = draft.filter((a) => a.name.trim() || a.cmd.length);
    // Both writes are validated Go-side and either can be rejected, so
    // one of them is going to be the "partial save" on failure. Agents go
    // first because that is the recoverable order: a rejected channel
    // leaves the modal open on the row that caused it, with the agent
    // edits already durable. The reverse strands a saved channel behind a
    // Cancel that no longer discards it.
    SaveCustomAgents(payload)
      .then(() =>
        SaveUpdateSettings({
          channel,
          source_repo: sourceRepo,
        } as main.UpdateSettings),
      )
      .then(closeSettings)
      // Go returns one joined error naming every rejected entry; show it
      // verbatim rather than paraphrasing it into something vaguer.
      .catch((err) => showError(String(err?.message || err)));
  }

  // Enter in a text field saves, as before. Escape and the backdrop are
  // the shell's. The Appearance textarea is excluded by the type check:
  // Enter there is a newline, and this would otherwise turn "next line of
  // overrides" into "save and close".
  //
  // On the root element, not on a wrapper: #settings-scroll and
  // #settings-updates must stay direct children of .hv-dialog__body
  // (settings.css pins Updates below the scrolling region), so this
  // subtree has no element of its own to hang a React handler on.
  // Re-attached each render so it closes over the current draft.
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (
        e.key === 'Enter' &&
        e.target instanceof HTMLInputElement &&
        e.target.type === 'text'
      ) {
        e.preventDefault();
        saveSettings();
      }
    }
    root.addEventListener('keydown', onKeyDown);
    return () => root.removeEventListener('keydown', onKeyDown);
  });

  return (
    <ModalShell
      id="settings"
      root={root}
      title="Settings"
      size="md"
      onClose={closeSettings}
      hints={[
        { keys: '[esc]', label: 'cancel' },
        { keys: '[enter]', label: 'save' },
      ]}
      actions={
        <>
          <Button id="settings-cancel" label="Cancel" onClick={closeSettings} />
          <Button
            id="settings-save"
            label="Save"
            kind="primary"
            disabled={!editingEnabled}
            onClick={saveSettings}
          />
        </>
      }
    >
      {/* Everything above Updates scrolls together. Agents first: it is
          the section people open Settings to edit, and it is the only one
          that grows — putting Appearance above it pushed the list
          off-screen on open for anyone with more than a couple of
          agents. */}
      <div id="settings-scroll">
        <h4>Custom agents</h4>
        <p className="settings-hint">
          Define your own tools — a command and its arguments. They appear in
          the new-session menu alongside the built-ins.
        </p>
        <div id="settings-agents-list" ref={listRef}>
          {loading ? (
            <p className="settings-hint">Loading…</p>
          ) : draft.length === 0 ? (
            <p className="settings-hint">No custom agents yet.</p>
          ) : (
            draft.map((a, i) => (
              // Index keys on purpose: a new row has no id at all until
              // Go assigns one on save, and the position IS the identity
              // here — every edit and the delete address a row by index.
              // biome-ignore lint/suspicious/noArrayIndexKey: see above
              <div className="settings-agent-row" key={i}>
                <span
                  className="hv-swatch"
                  style={{ ['--swatch' as string]: a.color || DEFAULT_COLOR }}
                >
                  <input
                    type="color"
                    aria-label="Agent color"
                    value={a.color || DEFAULT_COLOR}
                    onChange={(e) => patchAgent(i, { color: e.target.value })}
                  />
                </span>
                <input
                  type="text"
                  className="hv-input settings-agent-name"
                  autoComplete="off"
                  aria-label="Agent name"
                  placeholder="Name (e.g. Claude Lite)"
                  value={a.name || ''}
                  onChange={(e) => patchAgent(i, { name: e.target.value })}
                />
                <input
                  type="text"
                  className="hv-input settings-agent-cmd"
                  autoComplete="off"
                  aria-label="Agent command"
                  placeholder="Command (e.g. claude --model haiku)"
                  value={(a.cmd || []).join(' ')}
                  onChange={(e) =>
                    patchAgent(i, { cmd: splitCommand(e.target.value) })
                  }
                />
                <IconButton
                  icon="x"
                  label={`Delete ${a.name || 'agent'}`}
                  className="settings-agent-delete"
                  onClick={() => deleteAgentRow(i)}
                />
              </div>
            ))
          )}
        </div>
        <Button
          id="settings-agent-add"
          label="Add agent"
          icon="plus"
          disabled={!editingEnabled}
          onClick={addAgentRow}
        />
        <p
          id="settings-error"
          ref={errorRef}
          role="alert"
          className={
            error
              ? 'hv-field-error settings-error'
              : 'hv-field-error settings-error hidden'
          }
        >
          {error}
        </p>

        <h4>Appearance</h4>
        <p className="settings-hint">
          Applies as you change it, and is remembered. Cancel does not undo it.
        </p>
        <label className="hv-field">
          <span className="hv-field__label">Theme</span>
          <select
            id="settings-theme"
            className="hv-input"
            aria-label="Theme"
            value={theme}
            onChange={(e) => selectPreset(e.target.value as ThemeName)}
          >
            {groupPresets().map((g) =>
              g.group ? (
                <optgroup key={g.group} label={g.group}>
                  {g.options.map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.label}
                    </option>
                  ))}
                </optgroup>
              ) : (
                g.options.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.label}
                  </option>
                ))
              ),
            )}
          </select>
        </label>
        <p className="settings-hint">
          Override any design token, one declaration per line. Fonts must
          already be installed on this machine.
        </p>
        <label className="hv-field">
          <span className="hv-field__label">Custom tokens</span>
          <textarea
            id="settings-overrides"
            className="hv-input hv-input--mono"
            aria-label="Custom tokens"
            // Programmatic link to the slot this box writes its
            // rejections into: without it a screen reader never hears why
            // a line was ignored.
            aria-describedby="settings-overrides-error"
            rows={4}
            spellCheck={false}
            placeholder="--accent: #7aa2f7;"
            value={overrides}
            onChange={(e) => setOverrides(e.target.value)}
          />
        </label>
        <p
          id="settings-overrides-error"
          role="alert"
          className={
            overridesError ? 'hv-field-error' : 'hv-field-error hidden'
          }
        >
          {overridesError}
        </p>
      </div>

      {/* Updates sits outside the scrolling part of the body, at its
          natural height: a dozen custom agents must never push the
          channel picker below the fold (test/e2e/settings.spec.ts). */}
      <section id="settings-updates">
        <h4>Updates</h4>
        <p className="settings-hint">
          Hive checks for updates in the background. Nothing is downloaded or
          built until you press Update.
        </p>
        <label className="hv-field">
          <span className="hv-field__label">Channel</span>
          <select
            id="settings-update-channel"
            className="hv-input"
            aria-label="Update channel"
            value={channel}
            onChange={(e) =>
              setChannel(
                e.target.value === CHANNEL_LATEST
                  ? CHANNEL_LATEST
                  : CHANNEL_RELEASE,
              )
            }
          >
            <option value={CHANNEL_RELEASE}>Release — tagged versions</option>
            <option value={CHANNEL_LATEST}>
              Latest — tip of your checkout
            </option>
          </select>
        </label>
        {/* A container, not a field: it holds the labelled input and the
            Browse button on one line, and is hidden wholesale on the
            release channel. */}
        <div
          id="settings-source-repo-row"
          className={isLatest ? 'settings-field' : 'settings-field hidden'}
        >
          <label className="hv-field">
            <span className="hv-field__label">Source repo</span>
            <input
              id="settings-source-repo"
              type="text"
              className="hv-input"
              autoComplete="off"
              aria-label="Source repo"
              aria-describedby="settings-source-repo-hint"
              placeholder="Detected automatically"
              value={sourceRepo}
              onChange={(e) => setSourceRepo(e.target.value)}
            />
          </label>
          <Button
            id="settings-source-repo-browse"
            label="Browse…"
            onClick={browseSourceRepo}
          />
        </div>
        <p id="settings-source-repo-hint" className="settings-hint">
          {sourceRepoHint}
        </p>
        <div className="settings-field">
          <Button
            id="settings-update-action"
            label={updateBtn.label}
            hidden={!updateBtn.label}
            disabled={updateBtn.disabled || updateBusy}
            onClick={runUpdate}
            extra={{
              'data-action': updateBtn.action,
              'data-version': updateInfo?.latest || '',
            }}
          />
          <span
            id="settings-update-status"
            className="settings-hint"
            role="status"
          >
            {updateBtn.status}
          </span>
        </div>
      </section>
    </ModalShell>
  );
}

// Consecutive presets sharing a group land in one <optgroup>, per-run
// rather than per-name so the array order is the rendered order.
function groupPresets(): { group: string | null; options: typeof PRESETS }[] {
  const runs: { group: string | null; options: (typeof PRESETS)[number][] }[] =
    [];
  for (const p of PRESETS) {
    const last = runs[runs.length - 1];
    if (last && last.group === (p.group ?? null)) last.options.push(p);
    else runs.push({ group: p.group ?? null, options: [p] });
  }
  return runs;
}
