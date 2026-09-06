// ---------- the What's New modal (sidebar gift) ----------
//
// Renders site/features.json — the same curated list the website shows — as
// what shipped, grouped by release and newest first, then what is still to
// come. There is no markdown here to parse: the source is structured data, so
// rendering is a map and nothing ever reaches innerHTML.

import { useEffect, useLayoutEffect, useMemo } from 'react';
import type { ReactNode } from 'react';
import { closeWhatsNew } from '../../app/modals/whats-new.js';
import { groupByVersion, plannedOf } from '../../lib/whats-new.js';
import { useAppStore } from '../../store/store.js';
import { ModalShell } from './ModalShell.js';

export function WhatsNew({ root }: { root: HTMLElement | null }): ReactNode {
  const entry = useAppStore((s) => s.modals.find((m) => m.id === 'whats-new'));

  // #whats-new sits outside React's tree, so its open/closed class is applied
  // here — a passive effect would paint one stale frame with the backdrop up.
  useLayoutEffect(() => {
    root?.classList.toggle('hidden', !entry);
  }, [root, entry]);

  if (!entry || !root) return null;
  return <WhatsNewBody key={entry.seq} root={root} />;
}

function WhatsNewBody({ root }: { root: HTMLElement }): ReactNode {
  // The list is bundled, so it cannot change between opens — but the grouping
  // is a sort and a Map build, so it is still worth not redoing per render.
  const releases = useMemo(() => groupByVersion(), []);
  const planned = useMemo(() => plannedOf(), []);

  // Same modal-focus discipline as the help overlay: pull focus onto the
  // dialog so keystrokes don't leak behind the backdrop.
  useEffect(() => {
    document.getElementById('whats-new-close')?.focus();
  }, []);

  return (
    <ModalShell
      id="whats-new"
      root={root}
      title="What's new"
      size="lg"
      onClose={closeWhatsNew}
      hints={[{ keys: '[esc]', label: 'close' }]}
    >
      <div id="whats-new-body">
        {releases.map((release) => (
          <section key={release.version} className="whats-new-release">
            <h4>{release.version}</h4>
            <ul>
              {release.entries.map((feature) => (
                <li key={feature.title}>
                  <strong>{feature.title}</strong>
                  {feature.blurb ? <span> — {feature.blurb}</span> : null}
                </li>
              ))}
            </ul>
          </section>
        ))}
        {planned.length > 0 ? (
          <section className="whats-new-release whats-new-planned">
            <h4>Coming next</h4>
            <ul>
              {planned.map((feature) => (
                <li key={feature.title}>
                  <strong>{feature.title}</strong>
                  {feature.blurb ? <span> — {feature.blurb}</span> : null}
                </li>
              ))}
            </ul>
          </section>
        ) : null}
      </div>
    </ModalShell>
  );
}
