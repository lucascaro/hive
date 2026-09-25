// The way back to a deferred plan review (#457): a bar for the ACTIVE
// session while its agent waits on one. Escape on the review decides
// nothing, so without this a deferred plan would be reachable only by
// answering in the terminal. ⌘B's jump-to-attention lands here too: a
// waiting agent already reads as needing attention.

import type { ReactNode } from 'react';
import { raisePlanReview } from '../app/modals/plan-review.js';
import { pendingPlanReviewId } from '../app/state.js';
import { useAppStore } from '../store/store.js';
import { Button } from './Button.js';

export function PlanReviewBar(): ReactNode {
  const session = useAppStore((s) =>
    s.sessions.find((x) => x.id === s.activeId),
  );
  const open = useAppStore((s) => s.modals.some((m) => m.id === 'plan-review'));
  if (!session || !pendingPlanReviewId(session) || open) return null;
  return (
    <div className="hv-plan-review-bar" id="plan-review-bar" role="status">
      <span className="hv-plan-review-bar__label">
        {session.name ?? 'This session'} is waiting for you to review its plan
      </span>
      <Button
        id="plan-review-bar-open"
        label="Review plan…"
        kind="primary"
        onClick={() => raisePlanReview(session.id)}
      />
    </div>
  );
}
