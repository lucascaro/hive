// ---------- daemon-stale + update banners ----------
//
// Moved verbatim from main.js. initBanners() performs the listener
// and EventsOn registrations plus the boot-time update poll — the
// module has no side effects on import, matching events.js's
// wireDaemonEvents pattern.
// isDaemonRestarting() is read by the control:disconnect handler in
// events.js so a user-initiated restart doesn't flash a red status.

import {
  EventsOn, Confirm, RestartDaemon, CheckForUpdate, OpenURL,
} from '../bridge.js';
import { flashStatus, reportFailure } from './dom.js';

export function isDaemonRestarting() { return daemonRestarting; }

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
const daemonBannerEl = document.getElementById('daemon-banner');
const daemonBannerText = document.getElementById('daemon-banner-text');
const daemonBannerRestart = document.getElementById('daemon-banner-restart');
const daemonBannerDismiss = document.getElementById('daemon-banner-dismiss');
let daemonBannerDismissedFor = null;
let daemonRestarting = false;

function showDaemonBanner(text) {
  daemonBannerText.textContent = text;
  daemonBannerEl.classList.remove('hidden');
}
function hideDaemonBanner() {
  daemonBannerEl.classList.add('hidden');
}
function wireDaemonBanner() {
  daemonBannerDismiss.addEventListener('click', () => {
    // Dismissals are per-daemon-build: re-show if a different build
    // appears later. We stash the build we last saw mismatched (if any).
    daemonBannerDismissedFor = daemonBannerEl.dataset.daemonBuild || '';
    hideDaemonBanner();
  });
  daemonBannerRestart.addEventListener('click', async () => {
    // Restart kills hived AND relaunches Hive itself, so every running
    // session ends. Warn first.
    const ok = await Confirm(
      'Restart Hive?',
      'This will close Hive, terminate every running shell and agent, ' +
      'and reopen Hive with a fresh daemon. Save your work first.\n\n' +
      'Continue?',
    );
    if (!ok) return;
    daemonBannerRestart.disabled = true;
    daemonRestarting = true;
    showDaemonBanner('Restarting Hive…');
    try {
      await RestartDaemon();
      // RestartDaemon quits this process on success; control returns
      // here only on failure paths.
    } catch (err) {
      flashStatus(`restart failed: ${err}`, true);
      showDaemonBanner(`Restart failed: ${err}`);
    } finally {
      daemonBannerRestart.disabled = false;
      daemonRestarting = false;
    }
  });

  EventsOn('daemon:stale', (ev) => {
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

// Update-available banner. Backend's startUpdateCheckLoop emits
// "update:available" on startup + every 6h when a newer GitHub
// release tag than buildinfo.Version() is found. The user can also
// trigger it manually via the "Check for Updates…" menu item, which
// calls CheckForUpdate() and surfaces *all* outcomes (including
// "you're up to date" and "skipped: dev build") so the click feels
// responsive. Dismissals are remembered per-version in localStorage
// so the 6h tick doesn't re-nag for a release the user has already
// seen.
const updateBannerEl = document.getElementById('update-banner');
const updateBannerText = document.getElementById('update-banner-text');
const updateBannerDownload = document.getElementById('update-banner-download');
const updateBannerDismiss = document.getElementById('update-banner-dismiss');
const UPDATE_DISMISS_KEY = 'hive.updateDismissedFor';
let updateBannerAutoHideTimer = null;

function showUpdateBanner(text, { downloadUrl = '', showDownload = true, autoHideMs = 0 } = {}) {
  updateBannerText.textContent = text;
  updateBannerDownload.style.display = showDownload && downloadUrl ? '' : 'none';
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
function hideUpdateBanner() { updateBannerEl.classList.add('hidden'); }

// Transient (non-actionable) banners auto-hide so they don't linger
// after the user has registered the message. The "available" banner
// stays sticky — it has a Download button the user actually needs.
const UPDATE_TRANSIENT_MS = 4000;

function applyUpdateInfo(info, { manual = false } = {}) {
  if (!info) return;
  if (info.skipped) {
    if (manual) {
      showUpdateBanner('Update check skipped — this is a dev build.',
        { showDownload: false, autoHideMs: UPDATE_TRANSIENT_MS });
    }
    return;
  }
  if (info.available) {
    let dismissed = '';
    try { dismissed = localStorage.getItem(UPDATE_DISMISS_KEY) || ''; } catch {}
    if (!manual && dismissed === info.latest) return;
    // info.url is empty when the Go side rejected the release's
    // html_url for failing the github.com/<repo>/ prefix check
    // (defense-in-depth against a tampered or spoofed response).
    // Still tell the user an update exists; just don't expose a
    // one-click Download for an untrusted target.
    const trustedURL = !!info.url;
    const text = trustedURL
      ? `Hive ${info.latest} is available (you have ${info.current}).`
      : `Hive ${info.latest} is available (you have ${info.current}). Open releases page manually.`;
    showUpdateBanner(text, { downloadUrl: info.url });
    updateBannerEl.dataset.version = info.latest;
    return;
  }
  if (manual) {
    showUpdateBanner(`Hive ${info.current} is up to date.`,
      { showDownload: false, autoHideMs: UPDATE_TRANSIENT_MS });
  }
}

function wireUpdateBanner() {
  updateBannerDownload.addEventListener('click', () => {
    const url = updateBannerEl.dataset.url;
    if (url) OpenURL(url).catch(reportFailure('open link'));
  });
  updateBannerDismiss.addEventListener('click', () => {
    const v = updateBannerEl.dataset.version || '';
    if (v) {
      try { localStorage.setItem(UPDATE_DISMISS_KEY, v); } catch {}
    }
    hideUpdateBanner();
  });

  EventsOn('update:available', (info) => applyUpdateInfo(info));

  // Pull once on load. The Go side's periodic loop only fires every
  // 6h, so without this the user wouldn't see an "available" banner
  // until 6h after launch.
  // Intentionally silent: background boot poll. The manual menu path
  // below surfaces every outcome, including failures.
  CheckForUpdate().then((info) => applyUpdateInfo(info)).catch(() => {});
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
    showUpdateBanner(`Update check failed: ${err}`,
      { showDownload: false, autoHideMs: UPDATE_TRANSIENT_MS });
  } finally {
    updateCheckInFlight = false;
  }
}

export function initBanners() {
  wireDaemonBanner();
  wireUpdateBanner();
}
