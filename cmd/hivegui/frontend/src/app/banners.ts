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
import { pageEl } from './el.js';
import { icon } from '../ui/icon.js';
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
const daemonBannerEl = pageEl('daemon-banner');
const daemonBannerText = pageEl('daemon-banner-text');
const daemonBannerRestart = pageEl<HTMLButtonElement>('daemon-banner-restart');
const daemonBannerDismiss = pageEl('daemon-banner-dismiss');
let daemonBannerDismissedFor: string | null = null;
let daemonRestarting = false;

function showDaemonBanner(text: string) {
  daemonBannerText.textContent = text;
  daemonBannerEl.classList.remove('hidden');
}
function hideDaemonBanner() {
  daemonBannerEl.classList.add('hidden');
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
  daemonBannerRestart.disabled = true;
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
    daemonBannerRestart.disabled = false;
    daemonRestarting = false;
  }
}

function wireDaemonBanner() {
  daemonBannerDismiss.addEventListener('click', () => {
    // Dismissals are per-daemon-build: re-show if a different build
    // appears later. We stash the build we last saw mismatched (if any).
    daemonBannerDismissedFor = daemonBannerEl.dataset.daemonBuild || '';
    hideDaemonBanner();
  });
  daemonBannerRestart.addEventListener('click', restartHive);

  EventsOn('daemon:stale', (ev: DaemonStaleEvent | null) => {
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
const updateBannerEl = pageEl('update-banner');
const updateBannerText = pageEl('update-banner-text');
const updateBannerDownload = pageEl('update-banner-download');
const updateBannerAction = pageEl<HTMLButtonElement>('update-banner-action');
const updateBannerDismiss = pageEl('update-banner-dismiss');
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
    updateBannerAction.style.display = 'none';
    return;
  }
  updateBannerAction.style.display = '';
  updateBannerAction.textContent = btn.label;
  updateBannerAction.disabled = btn.disabled;
  updateBannerAction.dataset.action = btn.action;
  // Kept on the button, not on the banner: banner.dataset.version is the
  // per-version dismiss key, and showUpdateBanner deletes it on every
  // show — so by the time the button says Restart it would be gone.
  updateBannerAction.dataset.version = info?.latest || '';
}

function showUpdateBanner(
  text: string,
  { downloadUrl = '', showDownload = true, autoHideMs = 0 } = {},
) {
  updateBannerText.textContent = text;
  updateBannerDownload.style.display =
    showDownload && downloadUrl ? '' : 'none';
  updateBannerEl.dataset.url = downloadUrl;
  // Clear the per-version dismissal key on every show — only the
  // "available" branch sets it back. Without this, dismissing a
  // transient banner ("up to date", "checking…") would write a
  // stale version into localStorage.
  delete updateBannerEl.dataset.version;
  updateBannerEl.classList.remove('hidden');
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
  updateBannerEl.classList.add('hidden');
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
    updateBannerEl.dataset.version = info.latest;
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
  updateBannerAction.addEventListener('click', () => {
    const action = updateBannerAction.dataset.action;
    if (action === 'restart') {
      // Confirm + guard live in the shared wrapper; never call the
      // binding directly from a click handler.
      void applyUpdateAndRestart(updateBannerAction.dataset.version || '');
      return;
    }
    if (action === 'start') {
      updateBannerAction.disabled = true;
      // StartUpdate's refusals return synchronously without emitting an
      // update:progress event, so nothing would re-render the button —
      // re-enable it here or the click dead-ends it permanently.
      StartUpdate().catch((err) => {
        updateBannerAction.disabled = false;
        reportFailure('start update')(err);
      });
    }
  });
  updateBannerDownload.addEventListener('click', () => {
    const url = updateBannerEl.dataset.url;
    if (url) OpenURL(url).catch(reportFailure('open link'));
  });
  updateBannerDismiss.addEventListener('click', () => {
    const v = updateBannerEl.dataset.version || '';
    if (v) {
      try {
        localStorage.setItem(UPDATE_DISMISS_KEY, v);
      } catch {}
    }
    hideUpdateBanner();
  });

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

export function initBanners() {
  daemonBannerDismiss.replaceChildren(icon('x'));
  updateBannerDismiss.replaceChildren(icon('x'));
  wireDaemonBanner();
  wireUpdateBanner();
}
