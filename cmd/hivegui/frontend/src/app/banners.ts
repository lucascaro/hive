// ---------- daemon-stale + update banners ----------
//
// Moved verbatim from main.js. initBanners() performs the listener
// and EventsOn registrations plus the boot-time update poll — the
// module has no side effects on import, matching events.ts's
// wireDaemonEvents pattern.
// isDaemonRestarting() is read by the control:disconnect handler in
// events.ts so a user-initiated restart doesn't flash a red status.

import {
  EventsOn,
  Confirm,
  RestartDaemon,
  CheckForUpdate,
  StartUpdate,
  ApplyUpdateAndRestart,
  OpenURL,
} from '../bridge.js';
import { flashStatus, reportFailure } from './dom.js';
import { banner, type Banner } from '../ui/banner.js';
import { isMac } from '../lib/platform.js';
import { updateButtonState } from '../lib/update-state.js';
// Type-only, so the generated module is erased before Vite resolves it.
import type { main } from '../../wailsjs/go/models';
import type { DaemonStaleEvent } from './version-footer.js';

export function isDaemonRestarting() {
  return daemonRestarting;
}

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
// Built lazily by initBanners(): the module is imported by jsdom tests
// that never open a banner, and a mount on import would inject markup
// into scaffolds that don't expect it.
let daemonBanner: Banner | null = null;
let updateBanner: Banner | null = null;
let daemonBannerDismissedFor: string | null = null;
let daemonRestarting = false;

function showDaemonBanner(text: string) {
  daemonBanner?.setText(text);
  daemonBanner?.show();
}
function hideDaemonBanner() {
  daemonBanner?.hide();
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
  // Hoisted once: the menu and palette can reach restartHive before
  // initBanners() has mounted anything, and `finally` must not throw.
  const restartBtn = daemonBanner?.action('restart');
  if (restartBtn) restartBtn.disabled = true;
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
    if (restartBtn) restartBtn.disabled = false;
    daemonRestarting = false;
  }
}

function wireDaemonBanner() {
  EventsOn('daemon:stale', (ev: DaemonStaleEvent | null) => {
    if (!ev) return;
    if (daemonBanner)
      daemonBanner.el.dataset.daemonBuild = ev.daemonBuild || '';
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
}

// applyUpdateAndRestart is the ONE way either surface applies a staged
// update. Both the banner and the Settings modal call it.
//
// It exists because ApplyUpdateAndRestart ends in RestartDaemon, which
// is exactly as destructive as the Restart Hive action beside it: every
// running shell and agent dies. AGENTS.md requires destructive actions
// to go through the confirm overlay, and the first cut of this feature
// wired both buttons straight to the binding instead.
//
// It also claims `daemonRestarting`, which is what stops events.ts from
// flashing a red "disconnected" status while the daemon we deliberately
// killed is coming back. A restart path that skips that flag looks like
// a crash to the user.
//
// versionLabel names what is about to be installed ("2.5.0", or a commit
// on the latest channel); empty is tolerated so a caller that has lost
// track of it still gets a truthful, if vaguer, dialog.
export async function applyUpdateAndRestart(versionLabel = '') {
  // Same re-entrancy shape as restartHive: claimed before the first
  // await, because the confirm dialog is itself a window in which a
  // second click on the other surface would slip past.
  if (daemonRestarting) return;
  daemonRestarting = true;
  try {
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
  const el = updateBanner?.action('action');
  if (!el) return;
  const btn = updateButtonState(info, isMac);
  if (!btn.label) {
    el.hidden = true;
    return;
  }
  el.hidden = false;
  const label = el.querySelector('.hv-button__label');
  if (label) label.textContent = btn.label;
  el.disabled = btn.disabled;
  el.dataset.action = btn.action;
  // Kept on the button, not on the banner: banner.dataset.version is the
  // per-version dismiss key, and showUpdateBanner deletes it on every
  // show — so by the time the button says Restart it would be gone.
  el.dataset.version = info?.latest || '';
}

function showUpdateBanner(
  text: string,
  { downloadUrl = '', showDownload = true, autoHideMs = 0 } = {},
) {
  if (!updateBanner) return;
  updateBanner.setText(text);
  // A banner with no trusted URL still tells the user an update exists;
  // it just doesn't offer a one-click Download for an untrusted target.
  updateBanner.action('download').hidden = !(showDownload && downloadUrl);
  updateBanner.el.dataset.url = downloadUrl;
  // Clear the per-version dismissal key on every show — only the
  // "available" branch sets it back. Without this, dismissing a
  // transient banner ("up to date", "checking…") would write a
  // stale version into localStorage.
  delete updateBanner.el.dataset.version;
  updateBanner.show();
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
  updateBanner?.hide();
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
    showUpdateBanner(text, { downloadUrl: info.url });
    if (updateBanner) updateBanner.el.dataset.version = info.latest;
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
function onUpdateAction() {
  const el = updateBanner?.action('action');
  if (!el) return;
  if (el.dataset.action === 'restart') {
    // Confirm + guard live in the shared wrapper; never call the
    // binding directly from a click handler.
    void applyUpdateAndRestart(el.dataset.version || '');
    return;
  }
  if (el.dataset.action === 'start') {
    el.disabled = true;
    // StartUpdate's refusals return synchronously without emitting an
    // update:progress event, so nothing would re-render the button —
    // re-enable it here or the click dead-ends it permanently.
    StartUpdate().catch((err) => {
      el.disabled = false;
      reportFailure('start update')(err);
    });
  }
}

function mountBanners() {
  if (daemonBanner) return;
  daemonBanner = banner({
    kind: 'error',
    id: 'daemon-banner',
    actions: [{ id: 'restart', label: 'Restart Hive', onClick: restartHive }],
    onDismiss: () => {
      // Dismissals are per-daemon-build: a *different* mismatched build
      // later still surfaces.
      daemonBannerDismissedFor = daemonBanner?.el.dataset.daemonBuild || '';
      daemonBanner?.hide();
    },
  });
  daemonBanner.el.dataset.slot = 'daemon';
  updateBanner = banner({
    kind: 'info',
    id: 'update-banner',
    actions: [
      { id: 'action', label: 'Update', onClick: onUpdateAction },
      {
        id: 'download',
        label: 'Download',
        onClick: () => {
          const url = updateBanner?.el.dataset.url;
          if (url) OpenURL(url).catch(reportFailure('open link'));
        },
      },
    ],
    onDismiss: () => {
      const v = updateBanner?.el.dataset.version || '';
      if (v) {
        try {
          localStorage.setItem(UPDATE_DISMISS_KEY, v);
        } catch {}
      }
      updateBanner?.hide();
    },
  });
  updateBanner.el.dataset.slot = 'update';
  // Hidden until renderUpdateAction says otherwise — same as the old
  // markup's display:none default for a banner with nothing to act on.
  updateBanner.action('action').hidden = true;
  // Prepended so the two banners are grid rows 1 and 2, above the
  // sidebar+terms row, exactly where the markup used to sit.
  const app = document.getElementById('app');
  app?.prepend(daemonBanner.el, updateBanner.el);
}

export function initBanners() {
  mountBanners();
  wireDaemonBanner();
  wireUpdateBanner();
}
