// @vitest-environment jsdom
//
// Spec 389: the palette command that grants or revokes the focused
// session's leave to message its siblings. The daemon owns the flag, so
// the command only asks — with the opposite of what the store shows.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';

const { SetSessionOrchestrator } = vi.hoisted(() => ({
  SetSessionOrchestrator: vi.fn(
    (_id: string, _want: boolean): Promise<void> => Promise.resolve(),
  ),
}));

// Every other bridge call becomes an inert stub: this test is about one.
vi.mock('../../src/bridge.js', async (importOriginal) => {
  const real = await importOriginal<Record<string, unknown>>();
  const stubs = Object.fromEntries(
    Object.keys(real).map((k) => [k, vi.fn(() => Promise.resolve())]),
  );
  return { ...stubs, SetSessionOrchestrator };
});

type Store = typeof import('../../src/store/store.js');
let store: Store;
let actions: typeof import('../../src/app/actions.js');

beforeAll(async () => {
  // The real modules are imported underneath the stubs; jsdom has no
  // ResizeObserver for them to find.
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
  document.body.innerHTML = '<div id="terms"></div><ul id="projects"></ul>';
  store = await import('../../src/store/store.js');
  actions = await import('../../src/app/actions.js');
});

beforeEach(() => {
  SetSessionOrchestrator.mockClear();
  store.resetStore();
});

function seed(orchestrator?: boolean) {
  store.hiveStateView.sessions.push({
    id: 's1',
    name: 'api',
    alive: true,
    ...(orchestrator === undefined ? {} : { orchestrator }),
  } as never);
  store.hiveStateView.activeId = 's1';
}

describe('toggleOrchestrator', () => {
  it('grants when the session is not yet granted', () => {
    seed();
    actions.toggleOrchestrator();
    expect(SetSessionOrchestrator).toHaveBeenCalledWith('s1', true);
  });

  it('revokes when it is', () => {
    seed(true);
    actions.toggleOrchestrator();
    expect(SetSessionOrchestrator).toHaveBeenCalledWith('s1', false);
  });

  it('does nothing with no session', () => {
    expect(actions.toggleOrchestrator()).toBe(false);
    expect(SetSessionOrchestrator).not.toHaveBeenCalled();
  });
});
