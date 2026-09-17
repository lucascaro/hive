// The activity renderer registry (spec 416). Each placement looks its
// renderer up here, so a second visualization is a new file plus one
// entry — an in-repo seam, not a plugin API (see the spec's non-goals).
import type { ComponentType } from 'react';

import { ActivityPanel } from './ActivityPanel.js';
import { ActivityTile } from './ActivityTile.js';

export type ActivityPlacement = 'panel' | 'tile';

export const activityRenderers: Record<
  ActivityPlacement,
  ComponentType<{ sessionId: string }>
> = {
  panel: ActivityPanel,
  tile: ActivityTile,
};
