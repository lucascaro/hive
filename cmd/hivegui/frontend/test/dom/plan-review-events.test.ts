// @vitest-environment jsdom
//
// #457: a session whose agent waits on a plan review raises the review
// by itself, one at a time; Escape defers without answering; a review
// that ends elsewhere takes the modal down.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import * as bridge from '../../src/bridge.js';
import { createScrollTrace } from '../../src/lib/scroll-debug.js';

vi.mock('../../src/bridge.js', () => {
  const fn = () => vi.fn(() => Promise.resolve());
  return {
    ConnectControl: fn(),
    OpenSession: fn(),
    CloseAttach: fn(),
    WriteStdin: fn(),
    ResizeSession: fn(),
    RequestScrollbackReplay: fn(),
    CreateSession: fn(),
    DuplicateSession: fn(),
    KillSession: fn(),
    KillSessionAndWorktree: fn(),
    SetSessionAttention: fn(),
    RestartSession: fn(),
    UpdateSession: fn(),
    ListAgents: fn(),
    ListCustomAgents: fn(),
    SaveCustomAgents: fn(),
    CreateProject: fn(),
    KillProject: fn(),
    UpdateProject: fn(),
    LaunchDir: fn(),
    PickDirectory: fn(),
    OpenNewWindow: fn(),
    CloseWindow: fn(),
    IsGitRepo: fn(),
    OpenURL: fn(),
    OpenTerminalAt: fn(),
    Notify: fn(),
    Confirm: fn(),
    RestartDaemon: fn(),
    CheckForUpdate: fn(),
    SetClipboardText: fn(),
    ResolvePrompt: fn(),
    ResolveWorktreeChoice: fn(),
    GetPlanReview: fn(),
    ResolvePlanReview: fn(),
    LogFrontend: vi.fn(),
    EventsOn: vi.fn(),
    WindowSetTitle: vi.fn(),
    ClipboardGetText: fn(),
  };
});

let events: typeof import('../../src/app/events.js');
let store: typeof import('../../src/store/store.js');
let pr: typeof import('../../src/app/modals/plan-review.js');

beforeAll(async () => {
  document.body.innerHTML =
    '<div id="terms"></div><ul id="projects"></ul><div id="status"><span id="status-text"></span><span id="status-hint"></span></div><div id="plan-review" class="hv-dialog hidden"></div>';
  events = await import('../../src/app/events.js');
  store = await import('../../src/store/store.js');
  pr = await import('../../src/app/modals/plan-review.js');
  events.wireDaemonEvents({
    switchTo: vi.fn(),
    enforceViewFloor: vi.fn(),
    updateAppTitle: vi.fn(),
    focusActiveTerm: vi.fn(),
    refocusActiveTerm: vi.fn(),
    isDaemonRestarting: () => false,
    checkForUpdates: vi.fn(),
    scrollTrace: createScrollTrace({ enabled: false }),
  });
});

function emit(event: string, payload: unknown) {
  for (const call of vi.mocked(bridge.EventsOn).mock.calls) {
    if (call[0] === event) (call[1] as (p: unknown) => void)(payload);
  }
}

function session(id: string, reviewId = '') {
  return {
    id,
    name: `agent-${id}`,
    alive: true,
    ...(reviewId
      ? { pending_plan_review: { review_id: reviewId, source: 'claude' } }
      : {}),
  };
}

const updated = (id: string, reviewId = '') =>
  emit(
    'session:event',
    JSON.stringify({ kind: 'updated', session: session(id, reviewId) }),
  );
const planArrives = (id: string, reviewId: string, plan = '# p') =>
  emit(
    'planreview:plan',
    JSON.stringify({
      session_id: id,
      review_id: reviewId,
      source: 'claude',
      plan,
    }),
  );
const openEntry = () => store.modalEntry('plan-review');
const fetched = () =>
  vi.mocked(bridge.GetPlanReview).mock.calls.map((c) => `${c[0]}/${c[1]}`);

beforeEach(() => {
  store.closeModal('plan-review');
  pr.resetPlanReviewsForTest();
  vi.mocked(bridge.GetPlanReview).mockClear();
  // A snapshot with no sessions: nothing pending from a previous test.
  emit('session:list', JSON.stringify({ sessions: [] }));
});

describe('plan review events', () => {
  it('fetches the plan and raises the review on its own', () => {
    emit(
      'session:event',
      JSON.stringify({ kind: 'added', session: session('a') }),
    );
    updated('a', 'r1');
    expect(fetched()).toEqual(['a/r1']);
    planArrives('a', 'r1', '# the plan');
    expect(openEntry()).toMatchObject({
      sessionId: 'a',
      reviewId: 'r1',
      plan: '# the plan',
    });
  });

  it('raises reviews one at a time', () => {
    updated('a', 'r1');
    updated('b', 'r2');
    expect(fetched()).toEqual(['a/r1']);
    planArrives('a', 'r1');
    // b waits while a is on screen.
    expect(fetched()).toEqual(['a/r1']);
    updated('a', ''); // a answered
    expect(openEntry()).toBeUndefined();
    expect(fetched()).toEqual(['a/r1', 'b/r2']);
  });

  it('takes the review down when it ends elsewhere, answering nothing', () => {
    updated('a', 'r1');
    planArrives('a', 'r1');
    updated('a', ''); // answered in the terminal / another window
    expect(openEntry()).toBeUndefined();
    expect(bridge.ResolvePlanReview).not.toHaveBeenCalled();
  });

  it('replaces a superseded plan with the newer one', () => {
    updated('a', 'r1');
    planArrives('a', 'r1');
    updated('a', 'r2');
    expect(openEntry()).toBeUndefined();
    expect(fetched()).toEqual(['a/r1', 'a/r2']);
    planArrives('a', 'r2', '# v2');
    expect(openEntry()).toMatchObject({ reviewId: 'r2', plan: '# v2' });
  });

  it('Escape defers: not re-raised on its own, raised again on request', () => {
    updated('a', 'r1');
    planArrives('a', 'r1');
    pr.deferPlanReview();
    expect(openEntry()).toBeUndefined();
    updated('a', 'r1'); // any later event for the same review
    expect(fetched()).toEqual(['a/r1']);
    pr.raisePlanReview('a');
    expect(fetched()).toEqual(['a/r1', 'a/r1']);
  });

  it('a stale fetch moves on to the next review', () => {
    updated('a', 'r1');
    updated('b', 'r2');
    emit(
      'control:error',
      JSON.stringify({ code: 'plan_review_stale', session_id: 'a' }),
    );
    updated('a', '');
    expect(fetched()).toEqual(['a/r1', 'b/r2']);
  });

  it('drops the review of a removed session', () => {
    updated('a', 'r1');
    planArrives('a', 'r1');
    emit(
      'session:event',
      JSON.stringify({ kind: 'removed', session: session('a') }),
    );
    expect(openEntry()).toBeUndefined();
  });

  it('a new window learns about a waiting review from the snapshot', () => {
    emit('session:list', JSON.stringify({ sessions: [session('a', 'r9')] }));
    expect(fetched()).toEqual(['a/r9']);
  });
});
