// ---------- daemon-stale + update banners ----------
//
// Policy only. Since Phase 2 of the React rewrite the markup lives in
// components/Banner.tsx and the per-slot data in the store; this module
// decides WHEN each banner is up, what it says, and what its actions
// do. The dismiss handlers and the update action's click handler are
// exported for components/Banners.tsx, which declares the static half
// (kind, element id, action ids and labels).
//
// initBanners() performs the listener and EventsOn registrations plus
// the boot-time update poll — the module has no side effects on import,
// matching events.ts's wireDaemonEvents pattern.
// isDaemonRestarting() is read by the control:disconnect handler in
// events.ts so a user-initiated restart doesn't flash a red status.

import {
  EventsOn,
  Confirm,
  RestartDaemon,
  RequestReloadAllGUIs,
  CheckForUpdate,
  StartUpdate,
  ApplyUpdateAndRestart,
  OpenURL,
} from '../bridge.js';
import { flashStatus, reportFailure } from './dom.js';
import { appStore, hideBanner, setBanner } from '../store/store.js';
import { isMac } from '../lib/platform.js';
import { updateButtonState } from '../lib/update-state.js';
import { iconButton } from '../ui/icon-button.js';
// Type-only, so the generated module is erased before Vite resolves it.
import type { main } from '../../wailsjs/go/models';
import type { DaemonStaleEvent } from './version-footer.js';

export function isDaemonRestarting() {
  return daemonRestarting;
}

// Stale-daemon banner. The Go side emits "daemon:stale" on every
// connect with severity "match" / "reloadable" / "mismatch" /
// "unknown". Build IDs say whether anything changed; the daemon
// CONTRACT says what it costs to apply — equal contracts mean a GUI
// reload is enough and every session survives, anything else means the
// daemon has to restart and they all end.
//
// Both the reloadable and mismatch cases are symmetric (the daemon
// could be older OR newer than the GUI — bisect, stash,
// reverse-checkout all flip the direction), so the copy is deliberately
// direction-neutral about which side moved.
//
// Dismissal is keyed on the specific daemonBuild that was dismissed,
// so a *different* mismatched build later will still surface. A "match"
// reconnect clears the dismissal flag too.
let daemonBannerDismissedFor: string | null = null;
let daemonRestarting = false;

function showDaemonBanner(text: string) {
  setBanner('daemon', { text, visible: true });
}
function hideDaemonBanner() {
  hideBanner('daemon');
}

/** Dismiss handler for the daemon slot; wired in components/Banners.tsx. */
export function dismissDaemonBanner() {
  // Dismissals are per-daemon-build: a *different* mismatched build
  // later still surfaces.
  daemonBannerDismissedFor =
    appStore.getState().banners.daemon.data?.daemonBuild ?? '';
  hideDaemonBanner();
}
// restartHive confirms, then asks Go to replace the daemon and
// relaunch. Exported (like manualUpdateCheck below) because the
// daemon-stale banner used to be the *only* way to reach it: with
// matching builds the banner never appears, so there was no way to
// restart Hive at all. File ▸ Restart Hive… and the command palette
// call this directly.
export async function restartHive() {
  // Re-entrancy guard. The banner button disables itself, but the
  // menu item and palette entry bypass that — and the probe window
  // is seconds long, plenty of time to invoke it twice and reach
  // spawnNewGUI twice.
  if (daemonRestarting) return;
  // Claimed before the first await: the confirm dialog is itself a
  // window during which a second invocation would otherwise slip
  // past the check above.
  daemonRestarting = true;
  setRestartDisabled(true);
  try {
    // Restart kills hived AND relaunches Hive itself, so every
    // running session ends. Warn first.
    const ok = await Confirm(
      'Restart Hive?',
      'This will close Hive, terminate every running shell and agent, ' +
        'and reopen Hive with a fresh daemon. Save your work first.\n\n' +
        'Continue?',
    );
    if (!ok) return;
    showDaemonBanner('Restarting Hive…');
    try {
      await RestartDaemon();
      // RestartDaemon quits this process on success; control returns
      // here only on failure paths — including the daemon refusing to
      // die, which now surfaces here instead of silently relaunching
      // into the old daemon.
    } catch (err) {
      flashStatus(`restart failed: ${err}`, true);
      showDaemonBanner(`Restart failed: ${err}`);
    }
  } finally {
    setRestartDisabled(false);
    daemonRestarting = false;
  }
}

function setRestartDisabled(disabled: boolean) {
  setBanner('daemon', { actions: { restart: { disabled } } });
}

// reloadGui relaunches every GUI window and leaves the daemon — and
// every running shell and agent — alone.
//
// Deliberately NOT behind a confirm dialog, unlike restartHive. AGENTS.md
// requires the confirm for destructive actions, and this one destroys
// nothing: the sessions keep running, the windows come back. Putting a
// "are you sure?" in front of it would train the user to click through
// the dialog that DOES matter.
//
// It asks Go to broadcast rather than reloading this window, because
// each window is its own process — reloading only this one would leave
// the others running the old binary against the same daemon.
export async function reloadGui() {
  if (daemonRestarting) return;
  daemonRestarting = true;
  try {
    showDaemonBanner('Reloading Hive…');
    await RequestReloadAllGUIs();
    // Every window, this one included, is now relaunching. Control
    // returns here only if the request never reached the daemon.
  } catch (err) {
    flashStatus(`reload failed: ${err}`, true);
    showDaemonBanner(`Reload failed: ${err}`);
  } finally {
    daemonRestarting = false;
  }
}

function wireDaemonBanner() {
  EventsOn('daemon:stale', (ev: DaemonStaleEvent | null) => {
    if (!ev) return;
    setBanner('daemon', { data: { daemonBuild: ev.daemonBuild || '' } });
    // 'reloadable' is silent, like 'match'. Equal contracts means the
    // two are compatible, so a differing daemon BUILD is both harmless
    // and unactionable: reloading the GUI cannot change which build
    // hived is, so a banner offering it would reappear immediately
    // after a successful reload and never clear. The sidebar footer
    // reports the two builds instead — the right surface for a fact
    // the user cannot act on.
    if (ev.severity === 'match' || ev.severity === 'reloadable') {
      daemonBannerDismissedFor = null; // reset so future mismatch can re-show
      hideDaemonBanner();
      return;
    }
    // Same build the user already dismissed: stay hidden.
    if (daemonBannerDismissedFor === (ev.daemonBuild || '')) return;
    if (ev.severity === 'mismatch') {
      showDaemonBanner(
        `hived build (${ev.daemonBuild}) doesn't match this GUI (${ev.guiBuild}) ` +
          `and the daemon itself changed. Restarting Hive ends every running session.`,
      );
    } else {
      showDaemonBanner(
        `Could not verify daemon build (gui=${ev.guiBuild || '?'}, daemon=${ev.daemonBuild || '?'}). ` +
          `If something looks wrong, restart Hive.`,
      );
    }
  });
}

// applyUpdateAndRestart is the ONE way either surface applies a staged
// update. Both the banner and the Settings modal call it.
//
// It exists because ApplyUpdateAndRestart can end in RestartDaemon,
// which is exactly as destructive as the Restart Daemon action beside
// it: every running shell and agent dies. AGENTS.md requires
// destructive actions to go through the confirm overlay, and the first
// cut of this feature wired both buttons straight to the binding
// instead.
//
// `kind` says which it will be, and the dialog follows it. A
// GUI-only update destroys nothing, so it is applied without a
// confirm — putting a warning in front of a harmless action is how
// users learn to click through the one that matters. Go decides which
// it is (from the staged daemon's contract) and the reducer carries
// the answer here; anything other than 'gui' takes the destructive
// path, so a missing value is safe.
//
// It also claims `daemonRestarting`, which is what stops events.ts from
// flashing a red "disconnected" status while the daemon we deliberately
// killed is coming back. A restart path that skips that flag looks like
// a crash to the user.
//
// versionLabel names what is about to be installed ("2.5.0", or a commit
// on the latest channel); empty is tolerated so a caller that has lost
// track of it still gets a truthful, if vaguer, dialog.
export async function applyUpdateAndRestart(versionLabel = '', kind = '') {
  // Same re-entrancy shape as restartHive: claimed before the first
  // await, because the confirm dialog is itself a window in which a
  // second click on the other surface would slip past.
  if (daemonRestarting) return;
  daemonRestarting = true;
  try {
    if (kind !== 'gui') {
      const title = versionLabel
        ? `Install ${versionLabel} and restart Hive?`
        : 'Install the update and restart Hive?';
      const ok = await Confirm(
        title,
        'Hive will close, terminate every running shell and agent, and ' +
          'reopen on the new version. Save your work first.\n\n' +
          'Continue?',
      );
      if (!ok) return;
    }
    try {
      await ApplyUpdateAndRestart();
      // On success this process is already quitting; control reaching
      // here means the swap or the daemon teardown refused, and the
      // window the user is looking at still works.
    } catch (err) {
      flashStatus(`update failed: ${err}`, true);
    }
  } finally {
    daemonRestarting = false;
  }
}

// Update-available banner. Backend's startUpdateCheckLoop emits
// "update:available" on startup + every 6h when a newer GitHub
// release tag than buildinfo.Version() is found. The user can also
// trigger it manually via the "Check for Updates…" menu item, which
// calls CheckForUpdate() and surfaces *all* outcomes (including
// "you're up to date" and "skipped: dev build") so the click feels
// responsive. Dismissals are remembered per-version in localStorage
// so the 6h tick doesn't re-nag for a release the user has already
// seen.
const UPDATE_DISMISS_KEY = 'hive.updateDismissedFor';
let updateBannerAutoHideTimer: ReturnType<typeof setTimeout> | null = null;

// renderUpdateAction drives the Update / Updating… / Restart button from
// the shared reducer, so the banner and the Settings modal always agree
// about what the button should say. Hidden entirely when there is
// nothing to act on — including on non-macOS, where staging a build we
// could not install would be a dead end and the Download link is the
// real answer.
function renderUpdateAction(info: main.UpdateInfo | null) {
  const btn = updateButtonState(info, isMac);
  if (!btn.label) {
    setBanner('update', { actions: { action: { hidden: true } } });
    return;
  }
  setBanner('update', {
    actions: {
      action: {
        hidden: false,
        label: btn.label,
        disabled: btn.disabled,
        // Kept on the button, not on the banner: the banner's
        // data-version is the per-version dismiss key, and
        // showUpdateBanner drops it on every show — so by the time the
        // button says Restart it would be gone.
        data: { action: btn.action, version: info?.latest || '' },
      },
    },
  });
}

function showUpdateBanner(
  text: string,
  { downloadUrl = '', showDownload = true, autoHideMs = 0, version = '' } = {},
) {
  setBanner('update', {
    text,
    visible: true,
    // `data` is replaced wholesale, which is how the per-version
    // dismissal key gets cleared on every show — only the "available"
    // branch passes one back. Without that, dismissing a transient
    // banner ("up to date", "checking…") would write a stale version
    // into localStorage.
    data: version ? { url: downloadUrl, version } : { url: downloadUrl },
    // A banner with no trusted URL still tells the user an update
    // exists; it just doesn't offer a one-click Download for an
    // untrusted target.
    actions: { download: { hidden: !(showDownload && downloadUrl) } },
  });
  if (updateBannerAutoHideTimer) {
    clearTimeout(updateBannerAutoHideTimer);
    updateBannerAutoHideTimer = null;
  }
  if (autoHideMs > 0) {
    updateBannerAutoHideTimer = setTimeout(() => {
      hideUpdateBanner();
      updateBannerAutoHideTimer = null;
    }, autoHideMs);
  }
}
function hideUpdateBanner() {
  hideBanner('update');
}

/** Dismiss handler for the update slot; wired in components/Banners.tsx. */
export function dismissUpdateBanner() {
  const v = appStore.getState().banners.update.data?.version || '';
  if (v) {
    try {
      localStorage.setItem(UPDATE_DISMISS_KEY, v);
    } catch {}
  }
  hideUpdateBanner();
}

/** Download handler for the update slot; wired in components/Banners.tsx. */
export function openDownloadUrl() {
  const url = appStore.getState().banners.update.data?.url;
  if (url) OpenURL(url).catch(reportFailure('open link'));
}

// Transient (non-actionable) banners auto-hide so they don't linger
// after the user has registered the message. The "available" banner
// stays sticky — it has a Download button the user actually needs.
const UPDATE_TRANSIENT_MS = 4000;

function applyUpdateInfo(
  info: main.UpdateInfo | null,
  { manual = false }: { manual?: boolean } = {},
) {
  if (!info) return;
  renderUpdateAction(info);
  // Staging and its outcomes are always worth showing: the user asked
  // for this, and a failure that only lived in the Settings modal would
  // be invisible to anyone who closed it.
  if (
    info.stage === 'staging' ||
    info.stage === 'ready' ||
    info.stage === 'error'
  ) {
    const btn = updateButtonState(info, isMac);
    showUpdateBanner(btn.status, {
      downloadUrl: info.url || '',
      showDownload: info.stage !== 'ready',
    });
    return;
  }
  if (info.skipped) {
    if (manual) {
      // Go says *why* it skipped — an untagged build on the release
      // channel, a checkout with no upstream on the latest one. Those
      // are different problems with different fixes, so don't flatten
      // them back into one hardcoded sentence.
      showUpdateBanner(
        info.message
          ? `Update check skipped — ${info.message}.`
          : 'Update check skipped — this is a dev build.',
        { showDownload: false, autoHideMs: UPDATE_TRANSIENT_MS },
      );
    }
    return;
  }
  if (info.available) {
    let dismissed = '';
    try {
      dismissed = localStorage.getItem(UPDATE_DISMISS_KEY) || '';
    } catch {}
    if (!manual && dismissed === info.latest) return;
    // info.url is empty when the Go side rejected the release's
    // html_url for failing the github.com/<repo>/ prefix check
    // (defense-in-depth against a tampered or spoofed response).
    // Still tell the user an update exists; just don't expose a
    // one-click Download for an untrusted target.
    const trustedURL = !!info.url;
    const base = updateButtonState(info, isMac).status;
    const text = trustedURL ? base : `${base} Open releases page manually.`;
    showUpdateBanner(text, { downloadUrl: info.url, version: info.latest });
    return;
  }
  if (manual) {
    showUpdateBanner(`Hive ${info.current} is up to date.`, {
      showDownload: false,
      autoHideMs: UPDATE_TRANSIENT_MS,
    });
  }
}

function wireUpdateBanner() {
  EventsOn('update:available', (info: main.UpdateInfo | null) =>
    applyUpdateInfo(info),
  );
  // Staging progress. Go emits one of these per step, and on the
  // terminal ready/error transitions.
  EventsOn('update:progress', (info: main.UpdateInfo | null) =>
    applyUpdateInfo(info),
  );

  // Pull once on load. The Go side's periodic loop only fires every
  // 6h, so without this the user wouldn't see an "available" banner
  // until 6h after launch.
  // Intentionally silent: background boot poll. The manual menu path
  // below surfaces every outcome, including failures.
  CheckForUpdate()
    .then((info) => applyUpdateInfo(info))
    .catch(() => {});
}

// Guard against double-firing CheckForUpdate from the menu — clicking
// "Check for Updates…" repeatedly should not produce N parallel
// GitHub API calls.
let updateCheckInFlight = false;

export async function manualUpdateCheck() {
  if (updateCheckInFlight) return;
  updateCheckInFlight = true;
  showUpdateBanner('Checking for updates…', { showDownload: false });
  try {
    const info = await CheckForUpdate();
    applyUpdateInfo(info, { manual: true });
  } catch (err) {
    showUpdateBanner(`Update check failed: ${err}`, {
      showDownload: false,
      autoHideMs: UPDATE_TRANSIENT_MS,
    });
  } finally {
    updateCheckInFlight = false;
  }
}

// The update banner's primary action (Update / Updating… / Restart) is
// driven by renderUpdateAction from the shared reducer; the click
// handler dispatches on the data-action it wrote.
export function onUpdateAction() {
  const data = appStore.getState().banners.update.actions?.action?.data ?? {};
  if (data.action === 'restart' || data.action === 'reload') {
    // Confirm + guard live in the shared wrapper; never call the
    // binding directly from a click handler.
    void applyUpdateAndRestart(
      data.version || '',
      data.action === 'reload' ? 'gui' : 'full',
    );
    return;
  }
  if (data.action === 'start') {
    setBanner('update', { actions: { action: { disabled: true } } });
    // StartUpdate's refusals return synchronously without emitting an
    // update:progress event, so nothing would re-render the button —
    // re-enable it here or the click dead-ends it permanently.
    StartUpdate().catch((err) => {
      setBanner('update', { actions: { action: { disabled: false } } });
      reportFailure('start update')(err);
    });
  }
}

// wireCheckUpdatesButton puts a "Check for updates" control in the
// sidebar header, next to "New project". Until now the only manual
// trigger was the macOS app menu's "Check for Updates…" item, which is
// invisible on every other platform and undiscoverable on that one.
//
// Built here rather than in index.html because components.md forbids
// hand-rolled icon-only buttons: iconButton() supplies the markup, the
// aria-label/title pair and the .hv-icon-btn styling, so this adds no
// CSS of its own.
//
// Null-guarded on purpose: initBanners() is called by DOM tests that
// mount a scaffold with no sidebar header at all (update-banner and
// restart-hive), and a missing header must not throw there. The
// already-wired check keeps a second initBanners() from appending a
// duplicate.
const CHECK_UPDATES_ID = 'check-updates-btn';

function wireCheckUpdatesButton() {
  if (document.getElementById(CHECK_UPDATES_ID)) return;
  const newProjectBtn = document.getElementById('new-project-btn');
  if (!newProjectBtn?.parentElement) return;
  const btn = iconButton({
    icon: 'download',
    label: 'Check for updates',
    size: 22,
    onClick: () => void manualUpdateCheck(),
  });
  btn.id = CHECK_UPDATES_ID;
  newProjectBtn.after(btn);
}

export function initBanners() {
  wireDaemonBanner();
  wireUpdateBanner();
  wireCheckUpdatesButton();
}
