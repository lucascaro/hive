// ---------- sidebar footer version/build readout ----------
//
// Shows which build of hive (GUI) and hived (daemon) is running. This
// replaced the static keyboard-hint line: the shortcuts were already
// discoverable via the command palette (⇧⌘K) and help overlay (⌘/),
// whereas build identity had no always-visible surface at all — the
// stale-daemon banner only appears when the two *disagree*.
//
// Data arrives on the same "daemon:stale" event the banner uses. That
// event already fires on every control connect carrying both build IDs,
// both releases, and a computed severity, so there is nothing to poll
// and no extra Wails-bound method to call.
//
// This module takes its OWN EventsOn subscription rather than extending
// the handler in banners.ts: that one early-returns on severity ===
// 'match', which is exactly the case this footer must render (and the
// overwhelmingly common one). Wails supports multiple listeners per
// event, so the banner's control flow is left untouched.

import { EventsOn } from '../bridge.js';

// Payload of the "daemon:stale" Wails event — cmd/hivegui/app.go's
// DaemonStaleEvent. Hand-written like state.ts's SessionInfo: the struct
// only ever crosses the boundary via EventsEmit, never as a bound method's
// return, so it is absent from the generated wailsjs/go/models.ts. Every
// field is a required string; daemonVersionEvent() fills all five and the
// struct carries no omitempty.
export interface DaemonStaleEvent {
  // Closed set — daemonVersionEvent() in app.go emits exactly these
  // three. A literal union catches a typo'd comparison at the three
  // call sites in banners.ts / version-footer.ts.
  severity: 'match' | 'mismatch' | 'unknown';
  guiBuild: string;
  daemonBuild: string;
  guiRelease: string;
  daemonRelease: string;
}

// Structural rather than HTMLElement on purpose: the unit test injects
// fakes carrying exactly these members, and that is the contract this
// function actually has. Nullable because initVersionFooter() feeds it
// getElementById results and renderVersionFooter branches on their absence.
export interface VersionFooterEls {
  root: { classList: { toggle(name: string, force: boolean): unknown } } | null;
  gui: { textContent: string | null } | null;
  // `hidden` is `string | boolean` to match lib.dom's HTMLElement, which
  // widened it for the `until-found` value. This code only ever writes
  // booleans.
  daemon: { textContent: string | null; hidden: string | boolean } | null;
}

// Renders one binary as "<name> <release> (<build>)", degrading as
// fields go missing. An older daemon — one built before wire.Welcome
// gained the Release field — sends no release, so it must not render
// as "hived  (a3f9c1)" with a hole in it. buildinfo.Version() never
// returns "" on a current build, so an empty release is a reliable
// signal of exactly that older-daemon case.
export function formatBinary(
  name: string,
  release: string,
  build: string,
): string {
  const parts = [name];
  if (release) parts.push(release);
  if (build) parts.push(`(${build})`);
  // Neither release nor build known: say so rather than emitting a
  // bare name that reads like a truncated render.
  if (parts.length === 1) parts.push('(unknown build)');
  return parts.join(' ');
}

// Pure render step, exported for unit tests. `els` is injected so the
// tests can pass fakes instead of reaching into a live document.
export function renderVersionFooter(
  ev: DaemonStaleEvent | null,
  els: VersionFooterEls,
) {
  const { root, gui, daemon } = els;
  if (!root || !gui || !daemon) return;
  if (!ev) return;

  // Equal build IDs are equal git revisions, so the releases match too
  // — collapse to a single line and don't make the user read the same
  // version twice. Any other severity ('mismatch' or 'unknown') is
  // worth showing in full.
  const matched = ev.severity === 'match';
  root.classList.toggle('mismatch', ev.severity === 'mismatch');

  if (matched) {
    gui.textContent = formatBinary('hive', ev.guiRelease, ev.guiBuild);
    daemon.textContent = '';
    daemon.hidden = true;
    return;
  }

  gui.textContent = formatBinary('hive', ev.guiRelease, ev.guiBuild);
  daemon.textContent = formatBinary('hived', ev.daemonRelease, ev.daemonBuild);
  daemon.hidden = false;
}

export function initVersionFooter() {
  const els: VersionFooterEls = {
    root: document.getElementById('sidebar-hints'),
    gui: document.getElementById('ver-gui'),
    daemon: document.getElementById('ver-daemon'),
  };
  // Left empty until the first connect. The daemon's version is
  // unknowable before the handshake, and a placeholder would only
  // duplicate the daemon-down messaging the banner already owns.
  EventsOn('daemon:stale', (ev: DaemonStaleEvent | null) =>
    renderVersionFooter(ev, els),
  );
}
