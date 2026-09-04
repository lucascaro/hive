// ---------- settings (custom agents + appearance + updates) ----------
//
// The body is three tabbed panels. All three stay mounted and the
// inactive ones are hidden: that is what keeps the agent draft, the
// theme state, the update:progress subscription and the source-repo
// debounce alive across a switch, and `display: none` takes their
// controls out of the tab order for free. The error slot deliberately
// sits OUTSIDE the panels — see #settings-error below.
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
  MenuBarLoginItemStatus,
  PickDirectory,
  SetMenuBarLoginItem,
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
import { Tabs } from '../Tabs.js';
import { IconButton } from '../IconButton.js';
import { ModalShell } from './ModalShell.js';
// Type-only, so the generated module is erased before Vite resolves it.
import type { main } from '../../../wailsjs/go/models';

const DEFAULT_COLOR = '#64748b';

type TabId = 'agents' | 'appearance' | 'menubar' | 'updates';
// Menu bar is macOS-only and needs a login item the build can actually
// register, so its tab is absent — not disabled — everywhere else, which
// is what the section it replaced did with the same guard.
function tabsFor(showMenuBar: boolean): { id: TabId; label: string }[] {
  return [
    { id: 'agents', label: 'Agents' },
    { id: 'appearance', label: 'Appearance' },
    ...(showMenuBar ? [{ id: 'menubar' as TabId, label: 'Menu bar' }] : []),
    { id: 'updates', label: 'Updates' },
  ];
}

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
  // Covers the gap between the click and the first progress event only.
  // Every progress event clears it: staging succeeding turns the button
  // into "Restart" (and an error into "Retry"), and a latch that outlived
  // the event would leave the user staring at a greyed-out button with no
  // way to apply the update but closing and reopening Settings. The
  // imperative version got this for free — renderUpdateAction reassigned
  // .disabled on every event.
  const [updateBusy, setUpdateBusy] = useState(false);
  // Always opens on Agents: it is the section people open Settings to
  // edit, and the one that grows. Deliberately not persisted.
  const [tab, setTab] = useState<TabId>('agents');

  // Menu-bar login item. Status is re-read on open rather than tracked
  // while the modal is closed: the user can add or remove the login
  // item from System Settings, so anything cached here would be a
  // guess.
  const [menuBarStatus, setMenuBarStatus] = useState('unsupported');
  const [menuBarBusy, setMenuBarBusy] = useState(false);
  const [menuBarError, setMenuBarError] = useState('');

  useEffect(() => {
    let live = true;
    MenuBarLoginItemStatus()
      .then((s) => {
        if (live) setMenuBarStatus(s);
      })
      .catch(() => {
        if (live) setMenuBarStatus('unsupported');
      });
    return () => {
      live = false;
    };
  }, []);

  async function toggleMenuBarLoginItem() {
    const enable = menuBarStatus !== 'enabled';
    setMenuBarBusy(true);
    setMenuBarError('');
    try {
      await SetMenuBarLoginItem(enable);
      setMenuBarStatus(await MenuBarLoginItemStatus());
    } catch (err) {
      // Verbatim, not flattened into "could not enable": on the builds
      // Hive currently ships this fails with a code-signing error, and
      // that is the only thing telling the user it is not their fault
      // and not fixable from here.
      setMenuBarError(String(err));
    } finally {
      setMenuBarBusy(false);
    }
  }

  const menuBarHint = menuBarError
    ? `Could not change the login item: ${menuBarError}`
    : menuBarStatus === 'enabled'
      ? 'macOS starts the Hive menu bar at login.'
      : menuBarStatus === 'requires-approval'
        ? 'Waiting for approval in System Settings ▸ General ▸ Login Items.'
        : 'Off by default. The menu bar already appears whenever the daemon ' +
          'or a window starts; this adds it at login before either has run.';

  // Staging runs in Go and outlives this modal, so the button follows
  // the same progress events the banner does rather than any local
  // state. The state it starts from is re-read on open (UpdateStatus
  // below) rather than tracked while the dialog is closed.
  useEffect(
    () =>
      EventsOn('update:progress', (info: main.UpdateInfo | null) => {
        setUpdateInfo(info);
        setUpdateBusy(false);
      }),
    [],
  );

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

  // MenuBarLoginItemStatus resolves after mount, so the strip gains its
  // fourth tab once the answer arrives. It only ever gains one: the
  // status is read once and never re-read, so a tab cannot leave the
  // strip while it is the selected one, and `tab` always names a tab
  // that is there.
  const showMenuBar = isMac && menuBarStatus !== 'unsupported';
  const tabs = tabsFor(showMenuBar);

  function runUpdate() {
    if (updateBtn.action === 'restart' || updateBtn.action === 'reload') {
      // Shared with the banner: confirm overlay + re-entrancy guard +
      // the daemon-restart flag live there, and applying is exactly as
      // destructive from here as it is from the banner.
      void applyUpdateAndRestart(
        updateInfo?.latest || '',
        updateBtn.action === 'reload' ? 'gui' : 'full',
      );
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

  // Enter confirms the dialog — AGENTS.md lists dialog confirm/cancel
  // (Enter / Esc) as a hard-coded, non-rebindable binding, and the
  // footer hint says so. Two exclusions:
  //   * the Appearance textarea, where Enter is a newline (turning
  //     "next line of overrides" into "save and close" is the bug this
  //     started as),
  //   * buttons, where Enter is the button's own activation — Cancel
  //     would otherwise close AND save on the same keystroke.
  //
  // Pre-Phase-3 this fired only from `input[type=text]`, which is why
  // the hint was inaccurate the moment it was added: Enter did nothing
  // from the selects or the panel itself.
  //
  // On the root element, not on a wrapper: the body's children are the
  // tab strip, one panel per tab and the error slot, so there is no
  // single element covering the fields to hang a React handler on.
  // Re-attached each render so it closes over the current draft.
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (e.key !== 'Enter') return;
      const target = e.target as HTMLElement | null;
      if (
        target instanceof HTMLTextAreaElement ||
        target instanceof HTMLButtonElement
      ) {
        return;
      }
      e.preventDefault();
      saveSettings();
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
      <Tabs
        id="settings"
        label="Settings sections"
        tabs={tabs}
        active={tab}
        onChange={setTab}
      />
      <Panel tab="agents" active={tab}>
        {/* No <h4>: the selected tab is this section's heading, and the
            panel is aria-labelledby it. */}
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
      </Panel>

      <Panel tab="appearance" active={tab}>
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
      </Panel>

      {showMenuBar ? (
        <Panel tab="menubar" active={tab}>
          {/* The menu bar is macOS-only and starts itself whenever hived or
            a window does, so this tab is about ONE thing: whether launchd
            should start it at login too. Unsigned builds cannot register
            a login item, and the error says so rather than the toggle
            springing silently back. The tab itself is absent wherever the
            section would have been (see TABS below), so this panel only
            ever renders on a Mac that supports the login item. */}
          <p className="settings-hint">
            The Hive menu bar shows the daemon's version and sessions, and keeps
            working when every window is closed. It starts on its own whenever
            Hive does.
          </p>
          <div className="settings-field">
            <Button
              id="settings-menubar-login-item"
              label={
                menuBarStatus === 'enabled'
                  ? 'Stop starting at login'
                  : 'Start at login'
              }
              disabled={menuBarBusy}
              onClick={toggleMenuBarLoginItem}
            />
          </div>
          <p id="settings-menubar-hint" className="settings-hint">
            {menuBarHint}
          </p>
        </Panel>
      ) : null}

      <Panel tab="updates" active={tab}>
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
      </Panel>

      {/* Outside the panels on purpose. This is the slot for EVERY error
          the dialog raises, and half of them come from the Updates
          section (a rejected SaveUpdateSettings, a failed PickDirectory —
          test/dom/settings-updates.test.tsx). Inside the agents panel it
          would render invisibly whenever another tab is active. Being
          outside the scrolling region is also why it no longer needs a
          scrollIntoView to be seen. */}
      <p
        id="settings-error"
        role="alert"
        className={
          error
            ? 'hv-field-error settings-error'
            : 'hv-field-error settings-error hidden'
        }
      >
        {error}
      </p>
    </ModalShell>
  );
}

// A hidden panel is `display: none` (settings.css), which is what keeps
// its controls out of the Tab order while its React state stays alive.
// `role="tabpanel"` is not tabbable itself — every panel here contains
// its own focusable controls, so the pattern's "make the panel tabbable
// when it has none" case does not apply.
function Panel({
  tab,
  active,
  children,
}: {
  tab: TabId;
  active: TabId;
  children: ReactNode;
}): ReactNode {
  return (
    <section
      id={`settings-panel-${tab}`}
      className={tab === active ? 'settings-panel' : 'settings-panel hidden'}
      role="tabpanel"
      aria-labelledby={`settings-tab-${tab}`}
      hidden={tab !== active}
    >
      {children}
    </section>
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
