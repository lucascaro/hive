// @vitest-environment jsdom
//
// #451: a session parked on a worktree-setup failure must raise the
// choice dialog by itself, and send the user's answer back. The whole
// point of the feature is that these failures stop being silent, so
// "the dialog appeared at all" is the assertion that matters most.
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
    LogFrontend: vi.fn(),
    EventsOn: vi.fn(),
    WindowSetTitle: vi.fn(),
    ClipboardGetText: fn(),
  };
});

let wireDaemonEvents: typeof import('../../src/app/events.js').wireDaemonEvents;
let choice: typeof import('../../src/app/modals/choice-dialog.js');
let store: typeof import('../../src/store/store.js');

beforeAll(async () => {
  document.body.innerHTML =
    '<div id="terms"></div><ul id="projects"></ul><div id="status"><span id="status-text"></span><span id="status-hint"></span></div>';
  ({ wireDaemonEvents } = await import('../../src/app/events.js'));
  choice = await import('../../src/app/modals/choice-dialog.js');
  store = await import('../../src/store/store.js');

  wireDaemonEvents({
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

function parkedSession(id: string, over: Record<string, unknown> = {}) {
  return {
    id,
    name: 'stale-brook',
    alive: false,
    phase: 'blocked',
    pending_worktree_choice: {
      kind: 'fetch_failed',
      message: 'ssh: Could not resolve hostname gh.example.invalid',
      branch: 'stale-brook',
      cached_ref: 'origin/main',
      cached_tip: 'deadbeef',
      cached_tip_age_secs: 259200,
      ...over,
    },
  };
}

/** Wait for the dialog spec to reach the store. The store wraps it as
 *  { spec, seq }; the seq is what makes a re-open a distinct entry. */
async function pendingDialog() {
  for (let i = 0; i < 20; i++) {
    const entry = store.appStore.getState().choiceDialog;
    if (entry) return entry.spec;
    await new Promise((r) => setTimeout(r, 0));
  }
  return null;
}

function dialogSeq(): number | null {
  return store.appStore.getState().choiceDialog?.seq ?? null;
}

describe('parked worktree choice', () => {
  beforeEach(() => {
    vi.mocked(bridge.ResolveWorktreeChoice).mockClear();
    store.setChoiceDialog(null);
  });

  it('raises the dialog on its own when a parked session arrives', async () => {
    emit(
      'session:event',
      JSON.stringify({ kind: 'added', session: parkedSession('s-fetch') }),
    );
    const spec = await pendingDialog();
    if (!spec) throw new Error('no dialog was raised for a parked session');
    // git's own words, not a paraphrase: the user judges staleness
    // from them.
    expect(JSON.stringify(spec)).toContain('Could not resolve hostname');
    // Cancel must be FIRST: Escape and the scrim resolve to choice[0],
    // and the safe outcome is to create nothing.
    expect(spec.choices[0].value).toBe('cancel');
    expect(spec.choices.map((c) => c.value)).toEqual([
      'cancel',
      'retry',
      'proceed',
    ]);
    // The age is what makes "use the cached ref" an informed choice.
    const proceed = spec.choices.find((c) => c.value === 'proceed');
    expect(proceed?.label).toContain('origin/main');
    expect(proceed?.label).toContain('3 days old');
  });

  it('sends the chosen value back to the daemon', async () => {
    emit(
      'session:event',
      JSON.stringify({ kind: 'added', session: parkedSession('s-send') }),
    );
    await pendingDialog();
    choice.resolveChoiceDialog('retry');
    await new Promise((r) => setTimeout(r, 0));
    expect(bridge.ResolveWorktreeChoice).toHaveBeenCalledWith(
      's-send',
      'retry',
    );
  });

  it('offers the project directory when the worktree add failed', async () => {
    emit(
      'session:event',
      JSON.stringify({
        kind: 'added',
        session: parkedSession('s-add', {
          kind: 'create_failed',
          message: 'fatal: invalid reference',
          cached_ref: undefined,
        }),
      }),
    );
    const spec = await pendingDialog();
    if (!spec) throw new Error('no dialog was raised for a failed add');
    const proceed = spec.choices.find((c) => c.value === 'proceed');
    expect(proceed?.label).toBe('Use project directory');
  });

  it('does not stack a second dialog for repeated events', async () => {
    emit(
      'session:event',
      JSON.stringify({ kind: 'added', session: parkedSession('s-dupe') }),
    );
    await pendingDialog();
    const firstSeq = dialogSeq();
    // A parked session keeps attracting `updated` events; each one must
    // not raise another identical dialog on top of the open one.
    emit(
      'session:event',
      JSON.stringify({ kind: 'updated', session: parkedSession('s-dupe') }),
    );
    await new Promise((r) => setTimeout(r, 0));
    // Same entry, not a re-opened one: a re-open bumps seq.
    expect(dialogSeq()).toBe(firstSeq);
  });

  it('ignores a session with nothing pending', async () => {
    emit(
      'session:event',
      JSON.stringify({
        kind: 'added',
        session: { id: 's-ok', alive: true, phase: '' },
      }),
    );
    await new Promise((r) => setTimeout(r, 0));
    expect(store.appStore.getState().choiceDialog).toBeNull();
  });
});
