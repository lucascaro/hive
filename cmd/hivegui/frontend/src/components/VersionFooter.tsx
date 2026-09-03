// Sidebar footer version/build readout for hive (GUI) and hived
// (daemon). Mounted on #sidebar-hints, which keeps its id and the
// `.mismatch` class.
//
// It takes its OWN "daemon:stale" subscription rather than reading a
// store slice the banner writes: the banner's handler early-returns on
// severity === 'match', which is exactly the case this footer must
// render (and the overwhelmingly common one). Wails supports multiple
// listeners per event, so app/banners.ts's control flow is untouched.
//
// Left empty until the first connect. The daemon's version is
// unknowable before the handshake, and a placeholder would only
// duplicate the daemon-down messaging the banner already owns.
import { useEffect, useLayoutEffect, useState, type ReactNode } from 'react';
import { EventsOn } from '../bridge.js';
import { formatBinary, type DaemonStaleEvent } from '../app/version-footer.js';

export function VersionFooter({
  root,
}: {
  root: HTMLElement | null;
}): ReactNode {
  const [ev, setEv] = useState<DaemonStaleEvent | null>(null);

  useEffect(() => {
    // EventsOn returns its own unsubscribe; the imperative module never
    // called it (nothing ever tore the footer down), but a component has
    // an unmount, and the single root's teardown goes through it.
    return EventsOn('daemon:stale', (next: DaemonStaleEvent | null) => {
      if (next) setEv(next);
    });
  }, []);

  const mismatch = ev?.severity === 'mismatch';
  useLayoutEffect(() => {
    root?.classList.toggle('mismatch', mismatch);
  }, [root, mismatch]);

  // Equal build IDs are equal git revisions, so the releases match too
  // — collapse to a single line and don't make the user read the same
  // version twice. Any other severity ('mismatch' or 'unknown') is
  // worth showing in full.
  const matched = ev?.severity === 'match';
  return (
    <>
      <span id="ver-gui">
        {ev ? formatBinary('hive', ev.guiRelease, ev.guiBuild) : ''}
      </span>
      <span id="ver-daemon" hidden={!ev || matched}>
        {ev && !matched
          ? formatBinary('hived', ev.daemonRelease, ev.daemonBuild)
          : ''}
      </span>
    </>
  );
}
