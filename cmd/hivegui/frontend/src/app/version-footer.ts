// ---------- sidebar footer version/build readout ----------
//
// Shows which build of hive (GUI) and hived (daemon) is running. This
// replaced the static keyboard-hint line: the shortcuts were already
// discoverable via the command palette (⇧⌘K) and help overlay (⌘/),
// whereas build identity had no always-visible surface at all — the
// stale-daemon banner only appears when the two *disagree*.
//
// Data arrives on the "daemon:stale" event, which fires on every control
// connect carrying both build IDs, both releases, and a computed
// severity — nothing to poll and no extra Wails-bound method to call.
//
// The rendering and the event subscription are in
// components/VersionFooter.tsx since Phase 2 of the React rewrite. What
// stays here is the event's shape and the one formatting rule, both of
// which app/banners.ts also reads.

// Payload of the "daemon:stale" Wails event — cmd/hivegui/app.go's
// DaemonStaleEvent. Hand-written like state.ts's SessionInfo: the struct
// only ever crosses the boundary via EventsEmit, never as a bound method's
// return, so it is absent from the generated wailsjs/go/models.ts. Every
// field is a required string; daemonVersionEvent() fills all five and the
// struct carries no omitempty.
export interface DaemonStaleEvent {
  // Closed set — daemonVersionEvent() in app_control.go emits exactly
  // these four. A literal union catches a typo'd comparison at the
  // call sites in banners.ts / components/VersionFooter.tsx.
  //
  // 'reloadable' is the one that matters for what the user is offered:
  // the builds differ but the daemon contracts agree, so relaunching
  // the GUI alone picks up the change and every session survives.
  // 'mismatch' means the daemon itself has to restart, which ends them.
  severity: 'match' | 'reloadable' | 'mismatch' | 'unknown';
  guiBuild: string;
  daemonBuild: string;
  guiRelease: string;
  daemonRelease: string;
  // The two daemon contracts (buildinfo.DaemonContract), so the copy
  // can say why a restart is needed instead of just asserting it. 0 on
  // the daemon side means it predates the field.
  guiContract: number;
  daemonContract: number;
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
