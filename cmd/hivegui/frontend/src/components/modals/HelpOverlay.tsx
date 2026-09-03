// ---------- keyboard-shortcuts help overlay (⌘/) ----------
//
// React port of src/app/modals/help-overlay.ts. The groups are static
// per open, so useMemo replaces the old module-level `helpRendered`
// latch — recomputing per mount is the store-backed equivalent of
// "render once".

import { Fragment, useEffect, useLayoutEffect, useMemo } from 'react';
import type { ReactNode } from 'react';
import { closeHelpOverlay } from '../../app/modals/help-overlay.js';
import { isMac } from '../../lib/platform.js';
import { shortcutGroups } from '../../lib/shortcuts.js';
import { useAppStore } from '../../store/store.js';
import { Kbd } from '../Kbd.js';
import { ModalShell } from './ModalShell.js';

export function HelpOverlay({ root }: { root: HTMLElement | null }): ReactNode {
  const entry = useAppStore((s) => s.modals.find((m) => m.id === 'help'));

  // #help-overlay sits outside React's tree, so its open/closed class is
  // applied here — a passive effect would paint one stale frame with the
  // backdrop up before the class caught up.
  useLayoutEffect(() => {
    root?.classList.toggle('hidden', !entry);
  }, [root, entry]);

  if (!entry || !root) return null;
  // Remounted per opening (key={entry.seq}), which is what re-runs the
  // mount-focus effect below — openHelpOverlay() used to do that focus()
  // call itself, and can't anymore now that opening is just a store write.
  return <HelpOverlayBody key={entry.seq} root={root} />;
}

function HelpOverlayBody({ root }: { root: HTMLElement }): ReactNode {
  const groups = useMemo(() => shortcutGroups({ isMac }), []);

  // Same modal-focus discipline as Settings: pull focus onto the dialog
  // so keystrokes don't leak behind the backdrop.
  useEffect(() => {
    document.getElementById('help-overlay-close')?.focus();
  }, []);

  return (
    <ModalShell
      id="help-overlay"
      root={root}
      title="Keyboard shortcuts"
      size="lg"
      onClose={closeHelpOverlay}
      hints={[{ keys: '[esc]', label: 'close' }]}
    >
      <div id="help-overlay-groups">
        {groups.map((group) => (
          <section key={group.title}>
            <h4>{group.title}</h4>
            <dl>
              {group.items.map((item) => (
                <Fragment key={item.keys}>
                  <dt>
                    <Kbd>{item.keys}</Kbd>
                  </dt>
                  <dd>{item.label}</dd>
                </Fragment>
              ))}
            </dl>
          </section>
        ))}
      </div>
    </ModalShell>
  );
}
