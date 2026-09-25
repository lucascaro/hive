// ---------- plan review (#457): the non-React half ----------
//
// An agent (Claude at ExitPlanMode, Pi through hive_submit_plan) is
// blocked until the user approves or denies its plan. The daemon parks
// the review as session data: SessionInfo.pending_plan_review carries
// only its id, and the plan text is fetched once with GetPlanReview
// (answered on the "planreview:plan" event), because SessionInfo rides
// every broadcast and a plan can run to 128 KiB.
//
// Reviews are raised one at a time, like the worktree-choice dialog: a
// second agent planning at the same time queues behind the first rather
// than taking it off the screen unseen. Escape DEFERS, it never answers:
// the agent keeps waiting, its terminal still shows its own approval
// prompt, and the bar for the active session (PlanReviewBar) is the way
// back in. A review that ends elsewhere — answered in another window,
// answered in the terminal, the session exiting — takes the modal down
// with it.

import { flushSync } from 'react-dom';
import { GetPlanReview, ResolvePlanReview } from '../../bridge.js';
import { releaseFocus } from '../../lib/focus-trap.js';
import {
  closeModal,
  isModalOpen,
  modalEntry,
  openModal,
} from '../../store/store.js';
import { reportFailure } from '../dom.js';
import { pageEl } from '../el.js';
import { pendingPlanReviewId, type SessionInfo } from '../state.js';

export interface PlanComment {
  quote: string;
  text: string;
}

export interface PlanReviewDeps {
  setFocusedTile: (id: string | null) => void;
  focusActiveTerm: () => void;
}

let deps: PlanReviewDeps = {
  setFocusedTile: () => {},
  focusActiveTerm: () => {},
};

export function initPlanReview(injected: PlanReviewDeps) {
  deps = injected;
}

// session id → the review it is waiting on, in arrival order.
const pending = new Map<string, string>();
// Reviews the user deferred with Escape. Not re-raised on their own;
// raisePlanReview is the deliberate way back.
const deferred = new Set<string>();
// The review whose text is being fetched, if any.
let fetching: { sessionId: string; reviewId: string } | null = null;

/** Track one session's pending review from a SessionInfo. */
export function syncPlanReview(info: SessionInfo) {
  const reviewId = pendingPlanReviewId(info);
  const open = modalEntry('plan-review');
  if (!reviewId) {
    pending.delete(info.id);
  } else {
    pending.set(info.id, reviewId);
  }
  // The review on screen ended or was replaced by a newer plan: take it
  // down. Nothing is answered; the newer plan is raised below.
  if (open && open.sessionId === info.id && open.reviewId !== reviewId) {
    dismiss(false);
  }
  raiseNext();
}

/** A session went away: forget its review. */
export function dropPlanReview(sessionId: string) {
  pending.delete(sessionId);
  const open = modalEntry('plan-review');
  if (open?.sessionId === sessionId) dismiss(false);
}

/** Keep only reviews of sessions in liveIds (a full session snapshot). */
export function prunePlanReviews(liveIds: Set<string>) {
  for (const id of pending.keys()) if (!liveIds.has(id)) dropPlanReview(id);
}

function raiseNext() {
  if (fetching || isModalOpen('plan-review')) return;
  for (const [sessionId, reviewId] of pending) {
    if (deferred.has(reviewId)) continue;
    fetchReview(sessionId, reviewId);
    return;
  }
}

function fetchReview(sessionId: string, reviewId: string) {
  fetching = { sessionId, reviewId };
  GetPlanReview(sessionId, reviewId).catch((err) => {
    fetching = null;
    reportFailure('fetch plan')(err);
  });
}

/** The PLAN_REVIEW answer to GetPlanReview. */
export function onPlanReviewText(msg: {
  session_id?: string;
  review_id?: string;
  source?: string;
  plan?: string;
}) {
  const sessionId = msg.session_id ?? '';
  const reviewId = msg.review_id ?? '';
  if (!fetching || fetching.reviewId !== reviewId) return;
  fetching = null;
  // Still the review this session waits on? It may have been answered
  // elsewhere, or replaced, while the text was on its way.
  if (pending.get(sessionId) !== reviewId || isModalOpen('plan-review')) {
    raiseNext();
    return;
  }
  openModal({
    id: 'plan-review',
    sessionId,
    reviewId,
    source: msg.source ?? '',
    plan: msg.plan ?? '',
  });
  // Same modal-focus discipline as the other dialogs: the terminal's
  // visual focus goes, the keyboard goes to the review.
  deps.setFocusedTile(null);
}

/**
 * GET_PLAN_REVIEW found nothing pending: that review is over. Forget it
 * before moving on, or the queue would fetch the same stale review
 * forever and never reach the next one. A newer review for the session
 * arrives on its own session event.
 */
export function onPlanReviewStale(sessionId = '') {
  const was = fetching;
  fetching = null;
  const id = sessionId || was?.sessionId || '';
  if (was && pending.get(id) === was.reviewId) pending.delete(id);
  raiseNext();
}

/**
 * Open the review a session is waiting on, including one the user
 * deferred. The bar's "Review plan…" calls this.
 */
export function raisePlanReview(sessionId: string) {
  const reviewId = pending.get(sessionId);
  if (!reviewId) return;
  deferred.delete(reviewId);
  const open = modalEntry('plan-review');
  if (open?.reviewId === reviewId) return;
  // Something else is on screen or on its way: no longer deferred, this
  // one is raised when its turn in the queue comes.
  if (open || fetching) return;
  fetchReview(sessionId, reviewId);
}

function dismiss(deferIt: boolean) {
  const open = modalEntry('plan-review');
  if (!open) return;
  if (deferIt) deferred.add(open.reviewId);
  releaseFocus(pageEl('plan-review'));
  // flushSync: this also runs from plain listeners (keyboard.ts), and
  // focusActiveTerm must not run while the dialog is still visible.
  flushSync(() => closeModal('plan-review'));
  deps.focusActiveTerm();
  raiseNext();
}

/** Escape or the close button: decide later. The agent keeps waiting. */
export function deferPlanReview() {
  dismiss(true);
}

/** The user's answer. The modal closes once it is sent. */
export async function answerPlanReview(
  decision: 'approve' | 'deny',
  comments: PlanComment[],
  feedback: string,
) {
  const open = modalEntry('plan-review');
  if (!open) return;
  try {
    await ResolvePlanReview({
      session_id: open.sessionId,
      review_id: open.reviewId,
      decision,
      comments,
      feedback,
    } as Parameters<typeof ResolvePlanReview>[0]);
  } catch (err) {
    reportFailure('answer plan review')(err);
    return;
  }
  dismiss(false);
}

/** For tests: forget everything. */
export function resetPlanReviewsForTest() {
  pending.clear();
  deferred.clear();
  fetching = null;
}
