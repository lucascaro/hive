// @vitest-environment jsdom
//
// Plan review (#457) as the bundled plan-review plugin (spec 471). The
// REAL plugins/plan-review/ui.mjs is loaded through the plugin host, and
// session events go through the app's own wiring (app/events.ts), so
// this is the path the app takes: a session's pending review raises the
// review by itself, one at a time; Escape defers without answering; a
// review that ends elsewhere takes the view down; the answer reaches the
// daemon with every comment and the text it refers to.
import { act, cleanup, fireEvent, render } from '@testing-library/react';
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from 'vitest';
import type { PluginInfo } from '../../src/app/state.js';
import { createScrollTrace } from '../../src/lib/scroll-debug.js';

// The file the app ships, by absolute path: a bare relative specifier in
// a dynamic import resolves against the Vite root, not this file.
const PLUGIN = new URL(
  '../../../../../plugins/plan-review/ui.mjs',
  import.meta.url,
).pathname;

// Every EventsOn subscriber, so both the app and the plugin hear a
// daemon event, and a plugin's off() really unsubscribes.
const handlers = new Map<string, Set<(p: unknown) => void>>();

vi.mock('../../src/bridge.js', () => {
  const fn = () => vi.fn((..._a: unknown[]) => Promise.resolve());
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
    ListAgents: vi.fn(() => Promise.resolve([])),
    ListCustomAgents: fn(),
    SaveCustomAgents: fn(),
    CreateProject: fn(),
    KillProject: fn(),
    UpdateProject: fn(),
    LaunchDir: vi.fn(() => Promise.resolve('')),
    PickDirectory: fn(),
    OpenNewWindow: fn(),
    CloseWindow: fn(),
    IsGitRepo: vi.fn(() => Promise.resolve(false)),
    OpenURL: fn(),
    OpenTerminalAt: fn(),
    Notify: fn(),
    Confirm: fn(),
    RestartDaemon: fn(),
    CheckForUpdate: fn(),
    SetClipboardText: fn(),
    ClipboardGetText: fn(),
    ResolvePrompt: fn(),
    ResolveWorktreeChoice: fn(),
    GetPlanReview: fn(),
    ResolvePlanReview: fn(),
    GetExternalPlanReviewers: vi.fn(() => Promise.resolve([])),
    ListPlugins: fn(),
    SetPluginConfig: fn(),
    SetClientUI: fn(),
    PluginAssetBase: vi.fn(() => Promise.resolve('')),
    LogFrontend: vi.fn(),
    WindowSetTitle: vi.fn(),
    EventsOn: vi.fn((name: string, cb: (p: unknown) => void) => {
      let set = handlers.get(name);
      if (!set) {
        set = new Set();
        handlers.set(name, set);
      }
      set.add(cb);
      return () => set.delete(cb);
    }),
  };
});

let bridge: typeof import('../../src/bridge.js');
let store: typeof import('../../src/store/store.js');
let host: typeof import('../../src/app/plugin-host.js');
let surfaces: typeof import('../../src/components/PluginSurfaces.js');

const BODY =
  '<div id="app"><div id="terms"></div><ul id="projects"></ul>' +
  '<div id="status"><span id="status-text"></span><span id="status-hint"></span></div>' +
  '<div id="banners"></div><aside id="activity-panel" hidden></aside>' +
  '<aside id="plugin-panel" hidden></aside>' +
  '<div id="plugin-view" class="hv-dialog hidden" role="dialog"></div></div>';

beforeAll(async () => {
  document.body.innerHTML = BODY;
  bridge = await import('../../src/bridge.js');
  store = await import('../../src/store/store.js');
  host = await import('../../src/app/plugin-host.js');
  surfaces = await import('../../src/components/PluginSurfaces.js');
  const events = await import('../../src/app/events.js');
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
  act(() => {
    for (const cb of [...(handlers.get(event) ?? [])]) cb(payload);
  });
}

async function flush() {
  for (let i = 0; i < 8; i++) {
    await act(async () => {
      await Promise.resolve();
    });
  }
}

function planReviewInfo(enabled = true): PluginInfo {
  return {
    id: 'plan-review',
    name: 'Plan review',
    version: '1.0.0',
    api_version: '0.2',
    source: 'builtin',
    command: [],
    enabled,
    status: enabled ? 'running' : 'stopped',
    restarts: 0,
    ui: { entry: 'ui.mjs', style: 'ui.css' },
  };
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

// The plugin reads sessions from the app's store, which learns of a
// session from its "added" event; an "updated" for one it never saw is
// dropped, as it is in the app.
const updated = (id: string, reviewId = '') => {
  const known = store.appStore.getState().sessions.some((s) => s.id === id);
  emit(
    'session:event',
    JSON.stringify({
      kind: known ? 'updated' : 'added',
      session: session(id, reviewId),
    }),
  );
};
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
const view = () => store.modalEntry('plugin-view');
const fetched = () =>
  vi.mocked(bridge.GetPlanReview).mock.calls.map((c) => `${c[0]}/${c[1]}`);
const byId = <T extends HTMLElement = HTMLElement>(id: string) =>
  document.getElementById(id) as T;
const root = () => byId('plugin-view');

async function loadPlugin() {
  const { Button } = await import('../../src/components/Button.js');
  const { Kbd } = await import('../../src/components/Kbd.js');
  const { Markdown } = await import('../../src/components/Markdown.js');
  host.initPluginHost({
    components: { Button, Kbd, Markdown },
    switchTo: vi.fn(),
    refocusActiveTerm: vi.fn(),
    importer: () => import(/* @vite-ignore */ PLUGIN),
  });
  act(() => store.setPlugins([planReviewInfo()]));
  await vi.waitFor(async () => {
    await flush();
    expect(store.appStore.getState().pluginUI['plan-review']).toMatchObject({
      status: 'active',
    });
  });
}

beforeEach(async () => {
  document.body.innerHTML = BODY;
  store.resetStore();
  host.resetPluginHostForTest();
  vi.mocked(bridge.GetPlanReview).mockClear();
  vi.mocked(bridge.ResolvePlanReview).mockClear();
  vi.mocked(bridge.SetClientUI).mockClear();
  vi.mocked(bridge.SetPluginConfig).mockClear();
  emit('session:list', JSON.stringify({ sessions: [] }));
  render(<surfaces.PluginSurfaces />);
  await loadPlugin();
});
afterEach(() => {
  cleanup();
  window.getSelection()?.removeAllRanges();
});

describe('the queue', () => {
  it('fetches the plan and raises the review on its own', () => {
    emit(
      'session:event',
      JSON.stringify({ kind: 'added', session: session('a') }),
    );
    updated('a', 'r1');
    expect(fetched()).toEqual(['a/r1']);
    planArrives('a', 'r1', '# the plan');
    expect(view()).toMatchObject({
      pluginId: 'plan-review',
      sessionId: 'a',
      props: { reviewId: 'r1', plan: '# the plan' },
    });
    expect(root().querySelector('#plugin-view-title')?.textContent).toContain(
      'Review plan · agent-a',
    );
  });

  it('raises reviews one at a time', () => {
    updated('a', 'r1');
    updated('b', 'r2');
    expect(fetched()).toEqual(['a/r1']);
    planArrives('a', 'r1');
    // b waits while a is on screen.
    expect(fetched()).toEqual(['a/r1']);
    updated('a', ''); // a answered
    expect(view()).toBeUndefined();
    expect(fetched()).toEqual(['a/r1', 'b/r2']);
  });

  it('takes the review down when it ends elsewhere, answering nothing', () => {
    updated('a', 'r1');
    planArrives('a', 'r1');
    updated('a', ''); // answered in the terminal / another window
    expect(view()).toBeUndefined();
    expect(bridge.ResolvePlanReview).not.toHaveBeenCalled();
  });

  it('replaces a superseded plan with the newer one', () => {
    updated('a', 'r1');
    planArrives('a', 'r1');
    updated('a', 'r2');
    expect(view()).toBeUndefined();
    expect(fetched()).toEqual(['a/r1', 'a/r2']);
    planArrives('a', 'r2', '# v2');
    expect(view()).toMatchObject({ props: { reviewId: 'r2', plan: '# v2' } });
  });

  it('Escape defers: not re-raised on its own, raised again from the banner', async () => {
    act(() => store.setActiveId('a'));
    updated('a', 'r1');
    planArrives('a', 'r1');
    const { container } = render(<surfaces.PluginBanner />);
    // Hidden while the review itself is on screen.
    expect(container.querySelector('.hv-plugin-banner')).toBeNull();
    act(() => host.closeSessionView('dismissed'));
    await flush();
    expect(view()).toBeUndefined();
    updated('a', 'r1'); // any later event for the same review
    expect(fetched()).toEqual(['a/r1']);
    expect(
      container.querySelector('.hv-plugin-banner__label')?.textContent,
    ).toContain('agent-a is waiting for you to review its plan');
    fireEvent.click(
      container.querySelector('.hv-plugin-banner__action') as HTMLElement,
    );
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
    expect(view()).toBeUndefined();
  });

  it('a new window learns about a waiting review from the snapshot', () => {
    emit('session:list', JSON.stringify({ sessions: [session('a', 'r9')] }));
    expect(fetched()).toEqual(['a/r9']);
  });

  it('marks a waiting session in the sidebar', () => {
    updated('a', 'r1');
    const s = store.appStore.getState().sessions.find((x) => x.id === 'a');
    if (!s) throw new Error('no session');
    const { container } = render(<surfaces.PluginBadges session={s} />);
    expect(container.querySelector('.hv-plugin-badge')?.textContent).toBe(
      'Review',
    );
  });
});

describe('announcing the review UI', () => {
  it('tells the daemon it can show reviews, and stops when disabled or failed', async () => {
    expect(bridge.SetClientUI).toHaveBeenLastCalledWith(['plan-review']);
    act(() => store.setPlugins([planReviewInfo(false)]));
    await flush();
    expect(bridge.SetClientUI).toHaveBeenLastCalledWith([]);

    act(() => store.setPlugins([planReviewInfo()]));
    await flush();
    expect(bridge.SetClientUI).toHaveBeenLastCalledWith(['plan-review']);
    act(() => host.failPlugin('plan-review', new Error('boom')));
    await flush();
    expect(bridge.SetClientUI).toHaveBeenLastCalledWith([]);
  });
});

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

describe('the review', () => {
  beforeEach(async () => {
    updated('s1', 'r1');
    planArrives('s1', 'r1', PLAN);
    await flush();
  });

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
    await flush();
    expect(bridge.ResolvePlanReview).toHaveBeenCalledWith({
      session_id: 's1',
      review_id: 'r1',
      decision: 'deny',
      comments: [{ quote: 'hello.txt', text: 'Call it greeting.txt' }],
      feedback: 'Add a test.',
    });
    expect(view()).toBeUndefined();
  });

  it('approves with the review id', async () => {
    await act(async () => {
      fireEvent.click(byId('plan-review-approve'));
    });
    expect(bridge.ResolvePlanReview).toHaveBeenCalledWith(
      expect.objectContaining({
        session_id: 's1',
        review_id: 'r1',
        decision: 'approve',
      }),
    );
  });

  it('keeps the review up and says so when the answer fails to send', async () => {
    vi.mocked(bridge.ResolvePlanReview).mockRejectedValueOnce(
      new Error('no control connection'),
    );
    await act(async () => {
      fireEvent.click(byId('plan-review-approve'));
    });
    await flush();
    expect(view()).toBeDefined();
    expect(root().querySelector('[role="alert"]')?.textContent).toContain(
      'no control connection',
    );
    expect(byId<HTMLButtonElement>('plan-review-approve').disabled).toBe(false);
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

// The reviewer choice, formerly on the Agents tab, is the plugin's own
// settings section, saved to its settings (the daemon reads "reviewer"
// from there at spawn).
describe('settings section', () => {
  it('saves the reviewer choice and warns about a settings-file hook only when Hive is chosen', async () => {
    vi.mocked(bridge.GetExternalPlanReviewers).mockResolvedValue([
      { kind: 'settings', id: '/home/u/.claude/settings.json', active: true },
      { kind: 'plugin', id: 'plannotator@plannotator', active: true },
    ] as Awaited<ReturnType<typeof bridge.GetExternalPlanReviewers>>);
    render(<surfaces.PluginSettingsSection id="plan-review" />);
    await flush();
    const select = byId<HTMLSelectElement>('plan-review-reviewer');
    expect(select.value).toBe('external');
    expect(byId('plan-review-reviewer-warning')).toBeNull();

    fireEvent.change(select, { target: { value: 'hive' } });
    await flush();
    expect(bridge.SetPluginConfig).toHaveBeenLastCalledWith('plan-review', {
      reviewer: 'hive',
    });
    expect(byId<HTMLSelectElement>('plan-review-reviewer').value).toBe('hive');
    const warn = byId('plan-review-reviewer-warning').textContent ?? '';
    expect(warn).toContain('/home/u/.claude/settings.json');
    // A plugin reviewer CAN be switched off, so it is not warned about.
    expect(warn).not.toContain('plannotator');
  });
});

describe('capBytes', () => {
  it('caps in UTF-8 bytes on a code-point boundary, matching the wire', async () => {
    const { capBytes } = (await import(/* @vite-ignore */ PLUGIN)) as {
      capBytes: (s: string, max: number) => string;
    };
    const bytes = (s: string) => new TextEncoder().encode(s).length;
    expect(capBytes('abc', 4096)).toBe('abc');
    const cjk = capBytes('界'.repeat(4096), 4096);
    expect(bytes(cjk)).toBeLessThanOrEqual(4096);
    expect(cjk).toBe('界'.repeat(1365));
    // A surrogate pair is never split.
    expect(capBytes('a😀', 4)).toBe('a');
    expect(capBytes('a😀', 5)).toBe('a😀');
  });
});
