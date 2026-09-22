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
let events: typeof import('../../src/app/events.js');
let choice: typeof import('../../src/app/modals/choice-dialog.js');
let store: typeof import('../../src/store/store.js');

beforeAll(async () => {
  document.body.innerHTML =
    '<div id="terms"></div><ul id="projects"></ul><div id="status"><span id="status-text"></span><span id="status-hint"></span></div>';
  events = await import('../../src/app/events.js');
  ({ wireDaemonEvents } = events);
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
      park_id: 'park-1',
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
  beforeEach(async () => {
    // Questions are asked one at a time through a module-level queue,
    // so a dialog left unanswered by the previous test would block
    // every later one. Answer it with a REAL choice: a dismissal is
    // deliberately not an answer any more, so dismissing here would
    // re-ask forever instead of draining.
    for (let i = 0; i < 20 && store.appStore.getState().choiceDialog; i++) {
      choice.resolveChoiceDialog('cancel');
      await new Promise((r) => setTimeout(r, 0));
    }
    await new Promise((r) => setTimeout(r, 0));
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
      'park-1',
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

  it('never lets a second parked session answer the first', async () => {
    // The regression this file exists for. openChoiceDialog dismisses
    // whatever is open, resolving it to choices[0] — Cancel. When the
    // remote is unreachable EVERY session in a batch parks, so without
    // serialisation the second dialog silently cancels the first
    // session's create: worktree discarded, session deleted, question
    // never seen.
    emit(
      'session:event',
      JSON.stringify({ kind: 'added', session: parkedSession('s-one') }),
    );
    const first = await pendingDialog();
    expect(first?.detail).toBeDefined();
    const firstSeqSeen = dialogSeq();

    emit(
      'session:event',
      JSON.stringify({ kind: 'added', session: parkedSession('s-two') }),
    );
    await new Promise((r) => setTimeout(r, 0));

    // Nothing was answered on the user's behalf.
    expect(bridge.ResolveWorktreeChoice).not.toHaveBeenCalled();
    // And the first question is still the one on screen.
    expect(dialogSeq()).toBe(firstSeqSeen);

    // Answer it; the queued one then takes its turn.
    choice.resolveChoiceDialog('retry');
    await new Promise((r) => setTimeout(r, 0));
    expect(bridge.ResolveWorktreeChoice).toHaveBeenCalledWith(
      's-one',
      'retry',
      'park-1',
    );
    await new Promise((r) => setTimeout(r, 0));
    const second = await pendingDialog();
    expect(second).toBeTruthy();
    choice.resolveChoiceDialog('cancel');
    await new Promise((r) => setTimeout(r, 0));
    expect(bridge.ResolveWorktreeChoice).toHaveBeenCalledWith(
      's-two',
      'cancel',
      'park-1',
    );
  });

  it('raises the dialog from a session:list snapshot', async () => {
    // A parked session waits indefinitely, so it is routinely still
    // parked when a reloaded GUI, a reconnect, or a second window
    // arrives. Those only ever see the snapshot; without this the
    // session is unanswerable and can only be killed.
    emit(
      'session:list',
      JSON.stringify({ sessions: [parkedSession('s-snapshot')] }),
    );
    const spec = await pendingDialog();
    expect(spec).toBeTruthy();
    choice.resolveChoiceDialog('proceed');
    await new Promise((r) => setTimeout(r, 0));
    expect(bridge.ResolveWorktreeChoice).toHaveBeenCalledWith(
      's-snapshot',
      'proceed',
      'park-1',
    );
  });

  it('says local HEAD when origin never resolved', async () => {
    // No cached_ref means origin/HEAD was never resolved, so the
    // daemon branches from local HEAD. Labelling that button
    // "origin/main" would tell the user one base while they got
    // another — the silent-wrong-base outcome this feature deletes.
    emit(
      'session:event',
      JSON.stringify({
        kind: 'added',
        session: parkedSession('s-nohead', {
          cached_ref: undefined,
          cached_tip: undefined,
          cached_tip_age_secs: undefined,
        }),
      }),
    );
    const spec = await pendingDialog();
    const proceed = spec?.choices.find((c) => c.value === 'proceed');
    expect(proceed?.label).toContain('local HEAD');
    expect(proceed?.label).not.toContain('origin/main');
  });

  it('takes the dialog down when its session is removed', async () => {
    emit(
      'session:event',
      JSON.stringify({ kind: 'added', session: parkedSession('s-killed') }),
    );
    await pendingDialog();
    emit(
      'session:event',
      JSON.stringify({
        kind: 'removed',
        session: { id: 's-killed', alive: false },
      }),
    );
    await new Promise((r) => setTimeout(r, 0));
    // No modal left asking about a session that is gone, and no answer
    // sent on its behalf — the session no longer exists to receive one.
    expect(store.appStore.getState().choiceDialog).toBeNull();
    expect(bridge.ResolveWorktreeChoice).not.toHaveBeenCalled();
  });

  it('never answers the parked question on an unrelated dismissal', async () => {
    // The round-2 blocker. dismissChoiceDialog() is called by code with
    // nothing to do with this question — every worktree:list repaint,
    // closing the worktree browser or the idea inbox, Escape. Resolving
    // to choices[0] there would answer 'cancel': worktree discarded,
    // session deleted, question never seen. The session waits
    // indefinitely by design, so that window is wide open.
    emit(
      'session:event',
      JSON.stringify({ kind: 'added', session: parkedSession('s-dismiss') }),
    );
    await pendingDialog();

    choice.dismissChoiceDialog();
    await new Promise((r) => setTimeout(r, 0));

    // Nothing was decided.
    expect(bridge.ResolveWorktreeChoice).not.toHaveBeenCalled();
    // And it does NOT re-raise itself: the choice dialog owns the
    // keyboard app-wide, so re-asking in the same turn would make
    // Escape inert and leave the GUI unreachable until every parked
    // session is answered. The session stays parked and its tile
    // carries the way back.
    expect(store.appStore.getState().choiceDialog).toBeNull();

    // The tile's "Answer…" button re-raises it.
    events.raiseWorktreeChoice('s-dismiss');
    const again = await pendingDialog();
    expect(again).toBeTruthy();
    expect(JSON.stringify(again)).toContain('Could not resolve hostname');
  });

  it('takes a stale dialog down when another window answers it', async () => {
    emit(
      'session:event',
      JSON.stringify({ kind: 'added', session: parkedSession('s-elsewhere') }),
    );
    await pendingDialog();
    // The daemon broadcasts the session with the question cleared once
    // any window answers it. A second window must not keep showing a
    // modal whose buttons are now no-ops.
    emit(
      'session:event',
      JSON.stringify({
        kind: 'updated',
        session: { id: 's-elsewhere', alive: true, phase: '' },
      }),
    );
    await new Promise((r) => setTimeout(r, 0));
    expect(store.appStore.getState().choiceDialog).toBeNull();
    expect(bridge.ResolveWorktreeChoice).not.toHaveBeenCalled();
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
