// The grid shell: React owns *when* the terminal area is laid out,
// app/grid-layout.ts owns *what* it does. This component renders no DOM
// of its own — the tiles are SessionTerm hosts, which React must never
// create, destroy or reparent (each holds an xterm, a WebGL slot from the
// 8-wide process budget, and a live PTY attachment).
//
// So: one subscription, one layout effect, no children.
import { useLayoutEffect, type ReactNode } from 'react';

import {
  applyGridLayout,
  applySingle,
  gridScopeSessions,
} from '../app/grid-layout.js';
import { appStore, useAppStore } from '../store/store.js';

// Live read of the store, for the layout effect's non-reactive lookups.
const appData = () => appStore.getState();

// The subscription is a derived SIGNATURE, not the raw store fields, and
// what it leaves out is load-bearing:
//
// - `sessions` is out. `session:event(updated)` is the high-frequency
//   kind — one per phase step, one per surviving session when a kill
//   recompacts the order, one per agent-id capture poll — and each one
//   replaces the array reference. Today only two of those branches
//   repaint the grid (a removal, and an order change); both move this
//   signature, while a rename moves neither the signature nor, today,
//   the grid.
// - `attention` is out for the same reason, sharper: a bell never called
//   renderGrid(). events.ts and focus.ts patch the class straight onto
//   the host, and applyGridLayout() reads `attention` non-reactively when
//   a pass happens for some other reason. Subscribing would run a full
//   pass per bell, and every pass calls ensureAttached() on every in-grid
//   tile — which re-latches follow-bottom and would drag background tiles
//   out of history at bell rate.
//
// What IS in: the view mode, the active tile, the grid's project scope,
// and the ordered id list of the sessions the scope actually tiles —
// which is what carries minimize/restore, add/remove and reorder.
// A string, so the store's Object.is comparison holds and a notification
// that changes none of it re-renders nothing.
function gridSignature(): string {
  return [
    appData().view,
    appData().activeId ?? '',
    appData().gridProjectId ?? '',
    gridScopeSessions()
      .map((s) => s.id)
      .join(' '),
  ].join('|');
}

export function GridView(): ReactNode {
  const signature = useAppStore(gridSignature);

  // useLayoutEffect, not useEffect: callers that mutate the store and then
  // do post-layout work (focusActiveTerm, snapVisibleTermsToBottom) wrap
  // the write in flushSync, and only a LAYOUT effect is guaranteed to have
  // run by the time flushSync returns.
  //
  // The signature IS the dependency. The body reads the store through the
  // non-reactive appData() on purpose — a pass needs fields, like
  // `attention`, that must not be allowed to trigger one — so the linter
  // sees a dependency it cannot connect to anything the body names.
  // Dropping it would run the pass on every render instead.
  // biome-ignore lint/correctness/useExhaustiveDependencies: see above
  useLayoutEffect(() => {
    if (appData().view === 'single') applySingle(appData().activeId);
    else applyGridLayout();
  }, [signature]);

  return null;
}
