// The status bar's two slots. Mounted on #status, so React owns the two
// spans and the container keeps the id, the grid row and the `.error`
// tint.
//
// The flash/persistent arbitration is NOT here: lib/status.ts's
// createStatus still owns it (app/dom.ts holds the instance), and the
// store carries only what that engine last rendered.
import { Fragment, useLayoutEffect, type ReactNode } from 'react';
import { useAppStore } from '../store/store.js';
import { Kbd } from './Kbd.js';

export function StatusBar({ root }: { root: HTMLElement | null }): ReactNode {
  const status = useAppStore((s) => s.status);
  const hints = useAppStore((s) => s.modeHint);

  // The error tint is on the bar, not the span: the whole row flashes.
  // #status is the portal-style container outside React's tree, so the
  // class is applied by an effect — layout, not passive, so the tint
  // lands in the same frame as the text it belongs to.
  useLayoutEffect(() => {
    root?.classList.toggle('error', status.isError);
  }, [root, status.isError]);

  return (
    <>
      {/* The live region is the text slot alone. With it on the bar, the
          mode hints re-announced on every navigation even though they
          only restate a static per-mode shortcut. */}
      <span
        id="status-text"
        role="status"
        aria-live="polite"
        // Same reason as the banner's text: the slot ellipsises, and an
        // error string is exactly the case where the tail matters.
        title={status.text || undefined}
      >
        {status.text}
      </span>
      {/* The right slot: the current mode's top shortcuts. */}
      <span id="status-hint">
        {hints.map((h) => (
          <Fragment key={h.key}>
            <Kbd>{h.key}</Kbd>
            <span className="hv-status__hint-label">{h.label}</span>
          </Fragment>
        ))}
      </span>
    </>
  );
}
