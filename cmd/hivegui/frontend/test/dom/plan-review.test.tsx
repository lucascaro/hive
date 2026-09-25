// The plan review modal (#457): rendered markdown, comments anchored to
// selected passages, and the answer that reaches the agent.
import { act, cleanup, fireEvent, render } from '@testing-library/react';
import { createPortal } from 'react-dom';
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from 'vitest';

const resolvePlanReview = vi.fn((_a: unknown) => Promise.resolve());
vi.mock('../../src/bridge.js', () => ({
  GetPlanReview: vi.fn(() => Promise.resolve()),
  ResolvePlanReview: (a: unknown) => resolvePlanReview(a),
}));

let store: typeof import('../../src/store/store.js');
let PlanReview: typeof import('../../src/components/modals/PlanReview.js').PlanReview;

const PLAN = [
  '# Plan',
  '',
  'Add a **greeting** file.',
  '',
  '1. Create `hello.txt`',
  '2. Verify it',
  '',
  '```sh',
  'cat hello.txt',
  '```',
  '',
  '<script>window.__pwned = true</script>',
  '',
  '[docs](javascript:alert(1))',
].join('\n');

beforeAll(async () => {
  document.body.innerHTML = `
    <div id="terms"></div><ul id="projects"></ul>
    <div id="status"><span id="status-text"></span><span id="status-hint"></span></div>
    <div id="plan-review" class="hv-dialog hidden" role="dialog" aria-modal="true"
         aria-labelledby="plan-review-title"></div>`;
  store = await import('../../src/store/store.js');
  ({ PlanReview } = await import('../../src/components/modals/PlanReview.js'));
});

const root = () => document.getElementById('plan-review') as HTMLElement;
const byId = <T extends HTMLElement = HTMLElement>(id: string) =>
  document.getElementById(id) as T;

function openReview(plan = PLAN) {
  act(() =>
    store.openModal({
      id: 'plan-review',
      sessionId: 's1',
      reviewId: 'r1',
      source: 'claude',
      plan,
    }),
  );
}

/** Select the whole text of the first element matching sel. */
function selectText(sel: string) {
  const node = root().querySelector(sel) as HTMLElement;
  const range = document.createRange();
  range.selectNodeContents(node);
  const s = window.getSelection();
  s?.removeAllRanges();
  s?.addRange(range);
  fireEvent.mouseUp(byId('plan-review-body'));
}

beforeEach(() => {
  store.resetStore();
  resolvePlanReview.mockClear();
  render(createPortal(<PlanReview root={root()} />, root()));
  openReview();
});
afterEach(() => {
  cleanup();
  window.getSelection()?.removeAllRanges();
});

describe('plan review modal', () => {
  it('renders the plan as formatted markdown', () => {
    expect(root().classList.contains('hidden')).toBe(false);
    const body = byId('plan-review-body');
    expect(body.querySelector('.hv-md-heading')?.textContent).toBe('Plan');
    expect(body.querySelector('strong')?.textContent).toBe('greeting');
    expect(body.querySelectorAll('ol > li')).toHaveLength(2);
    expect(body.querySelector('li code')?.textContent).toBe('hello.txt');
    expect(body.querySelector('pre code')?.textContent).toContain(
      'cat hello.txt',
    );
  });

  it('shows raw HTML as text and never runs or links it', () => {
    const body = byId('plan-review-body');
    expect(body.querySelector('script')).toBeNull();
    expect(body.textContent).toContain('<script>');
    expect(
      (window as unknown as { __pwned?: boolean }).__pwned,
    ).toBeUndefined();
    expect(body.querySelector('a')).toBeNull();
    expect(body.innerHTML).not.toContain('href=');
  });

  it('does not focus a button, so a stray Enter cannot approve', () => {
    expect(document.activeElement?.id).toBe('plan-review-body');
  });

  it('anchors a comment to the selected passage and sends it on deny', async () => {
    expect(byId<HTMLButtonElement>('plan-review-deny').disabled).toBe(true);
    expect(byId<HTMLButtonElement>('plan-review-comment-start').disabled).toBe(
      true,
    );

    selectText('li code');
    fireEvent.click(byId('plan-review-comment-start'));
    fireEvent.change(byId('plan-review-comment'), {
      target: { value: 'Call it greeting.txt' },
    });
    fireEvent.click(byId('plan-review-comment-add'));
    expect(byId('plan-review-comments').textContent).toContain('hello.txt');
    fireEvent.change(byId('plan-review-feedback'), {
      target: { value: 'Add a test.' },
    });

    await act(async () => {
      fireEvent.click(byId('plan-review-deny'));
    });
    expect(resolvePlanReview).toHaveBeenCalledWith({
      session_id: 's1',
      review_id: 'r1',
      decision: 'deny',
      comments: [{ quote: 'hello.txt', text: 'Call it greeting.txt' }],
      feedback: 'Add a test.',
    });
    expect(store.isModalOpen('plan-review')).toBe(false);
  });

  it('approves with the review id', async () => {
    await act(async () => {
      fireEvent.click(byId('plan-review-approve'));
    });
    expect(resolvePlanReview).toHaveBeenCalledWith(
      expect.objectContaining({
        session_id: 's1',
        review_id: 'r1',
        decision: 'approve',
      }),
    );
  });

  it('ignores a selection outside the plan', () => {
    const outside = document.getElementById('status-text') as HTMLElement;
    outside.textContent = 'not the plan';
    const range = document.createRange();
    range.selectNodeContents(outside);
    window.getSelection()?.addRange(range);
    fireEvent.mouseUp(byId('plan-review-body'));
    expect(byId<HTMLButtonElement>('plan-review-comment-start').disabled).toBe(
      true,
    );
  });
});

describe('plan review bar', () => {
  it('shows for the active session while its review is deferred, and re-opens it', async () => {
    const { PlanReviewBar } = await import(
      '../../src/components/PlanReviewBar.js'
    );
    const pr = await import('../../src/app/modals/plan-review.js');
    const bridge = await import('../../src/bridge.js');
    cleanup();
    act(() => store.closeModal('plan-review'));
    pr.resetPlanReviewsForTest();
    const s = {
      id: 's1',
      name: 'planner',
      alive: true,
      pending_plan_review: { review_id: 'r1', source: 'claude' },
    };
    act(() => {
      store.setSessions([s]);
      store.setActiveId('s1');
    });
    pr.syncPlanReview(s);
    pr.onPlanReviewText({ session_id: 's1', review_id: 'r1', plan: '# p' });
    render(<PlanReviewBar />);
    // Hidden while the review itself is on screen.
    expect(document.getElementById('plan-review-bar')).toBeNull();
    act(() => pr.deferPlanReview());
    expect(byId('plan-review-bar').textContent).toContain('planner');
    vi.mocked(bridge.GetPlanReview).mockClear();
    fireEvent.click(byId('plan-review-bar-open'));
    expect(bridge.GetPlanReview).toHaveBeenCalledWith('s1', 'r1');
  });
});
