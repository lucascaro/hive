// The three banner slots, in the DOM order the imperative code produced
// by prepending into #app: undo-close, then daemon, then update.
// banner.css places [data-slot='daemon'] on grid row 1 and
// [data-slot='update'] on row 2, so this region's container carries
// `display: contents` (layout.css) and the banners stay direct grid
// children of #app.
//
// This file is the STATIC half of a banner — kind, element id, action
// ids and labels, and the handler each action runs. The runtime half
// (text, visibility, dismiss keys, per-action label/disabled) lives in
// the store, written by app/banners.ts and app/undo-close.ts. Splitting
// it this way keeps the policy modules free of markup without threading
// callbacks through the store.
import type { ReactNode } from 'react';
import {
  dismissDaemonBanner,
  dismissUpdateBanner,
  onUpdateAction,
  openDownloadUrl,
  restartHive,
} from '../app/banners.js';
import { dismissUndoBanner, undoLastClose } from '../app/undo-close.js';
import { useAppStore } from '../store/store.js';
import { Banner } from './Banner.js';

export function Banners(): ReactNode {
  const banners = useAppStore((s) => s.banners);
  return (
    <>
      <Banner
        slot="undo-close"
        kind="info"
        data={banners['undo-close']}
        actions={[{ id: 'undo', label: 'Undo', onClick: undoLastClose }]}
        onDismiss={dismissUndoBanner}
      />
      <Banner
        slot="daemon"
        kind="error"
        id="daemon-banner"
        data={banners.daemon}
        actions={[
          { id: 'restart', label: 'Restart Hive', onClick: restartHive },
        ]}
        onDismiss={dismissDaemonBanner}
      />
      <Banner
        slot="update"
        kind="info"
        id="update-banner"
        data={banners.update}
        actions={[
          { id: 'action', label: 'Update', onClick: onUpdateAction },
          { id: 'download', label: 'Download', onClick: openDownloadUrl },
        ]}
        onDismiss={dismissUpdateBanner}
      />
    </>
  );
}
