// ---------- the failed-build log viewer ----------
//
// Shows everything build.sh printed on a failed latest-channel update.
// The Go side strips ANSI escapes and carriage-return redraws before the
// text gets here (streamBuildOutput), and it lands as a text child of a
// <pre>, so nothing in it is ever parsed as markup.

import { useEffect, useLayoutEffect, useRef } from 'react';
import type { ReactNode } from 'react';
import { closeBuildLog } from '../../app/modals/build-log.js';
import { useAppStore } from '../../store/store.js';
import { ModalShell } from './ModalShell.js';

export function BuildLog({ root }: { root: HTMLElement | null }): ReactNode {
  const entry = useAppStore((s) => s.modals.find((m) => m.id === 'build-log'));

  // #build-log sits outside React's tree, so its open/closed class is applied
  // here — a passive effect would paint one stale frame with the backdrop up.
  useLayoutEffect(() => {
    root?.classList.toggle('hidden', !entry);
  }, [root, entry]);

  if (!entry || entry.id !== 'build-log' || !root) return null;
  return <BuildLogBody key={entry.seq} root={root} log={entry.log} />;
}

function BuildLogBody({
  root,
  log,
}: {
  root: HTMLElement;
  log: string;
}): ReactNode {
  const pre = useRef<HTMLPreElement>(null);

  // A build fails at the end, so open on the tail. The dialog body is the
  // scroller (dialog.css), not the <pre>. Focus the log so arrow keys and
  // PageUp scroll it straight away.
  useEffect(() => {
    const el = pre.current;
    if (!el) return;
    const body = el.closest('.hv-dialog__body');
    if (body) body.scrollTop = body.scrollHeight;
    el.focus({ preventScroll: true });
  }, []);

  return (
    <ModalShell
      id="build-log"
      root={root}
      title="Build log"
      size="lg"
      onClose={closeBuildLog}
      hints={[{ keys: '[esc]', label: 'close' }]}
    >
      <pre
        id="build-log-text"
        ref={pre}
        // biome-ignore lint/a11y/noNoninteractiveTabindex: plain text with nothing tabbable in it; without a focus stop a keyboard user cannot scroll it
        tabIndex={0}
      >
        {log || 'No build output was captured.'}
      </pre>
    </ModalShell>
  );
}
