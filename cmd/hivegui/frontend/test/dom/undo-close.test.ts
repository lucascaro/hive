// @vitest-environment jsdom
//
// Undo for an accidental session close (src/app/undo-close.ts).
//
// Two things this file exists to pin. First: the banner belongs to
// whoever pressed close — reacting to every `removed` event would pop
// an undo offer in a window that did not ask for one, for a session it
// may not even have been showing. Second: the follow-up message has to
// report what the restore actually lost. A restore that silently drops
// the worktree or the agent conversation while the banner says
// "reopened" is worse than no undo at all, because the user stops
// looking.
import { describe, it, expect, vi, beforeEach } from 'vitest';

// vi.mock factories are hoisted above every top-level binding, so the
// spies have to be created inside them and read back afterwards.
vi.mock('../../src/bridge.js', () => ({
  KillSession: vi.fn(() => Promise.resolve()),
  RestoreSession: vi.fn(() => Promise.resolve()),
}));

vi.mock('../../src/app/dom.js', () => ({
  reportFailure: () => () => {},
}));

import { KillSession, RestoreSession } from '../../src/bridge.js';

import {
  initUndoClose,
  noteLocalClose,
  onSessionRemoved,
  onSessionRestored,
  closeActiveSession,
  reopenLastClosedSession,
  resetUndoCloseForTest,
} from '../../src/app/undo-close.js';
import { state } from '../../src/app/state.js';

function bannerEl(): HTMLElement | null {
  return document.querySelector('[data-slot="undo-close"]');
}
function bannerText(): string {
  return bannerEl()?.querySelector('.hv-banner__text')?.textContent ?? '';
}
function undoButton(): HTMLButtonElement | null {
  return bannerEl()?.querySelector('[data-action-id="undo"]') ?? null;
}
function visible(): boolean {
  const el = bannerEl();
  return !!el && !el.hidden;
}

beforeEach(() => {
  vi.clearAllMocks();
  resetUndoCloseForTest();
  document.body.innerHTML = '<div id="app"></div>';
  state.activeId = null;
  state.sessions = [];
  initUndoClose();
});

describe('undo banner', () => {
  it('shows an undo banner after a locally initiated close', () => {
    expect(visible()).toBe(false);
    noteLocalClose('s1', 'api-refactor');
    onSessionRemoved('s1');

    expect(visible()).toBe(true);
    expect(bannerText()).toContain('api-refactor');
    expect(undoButton()?.hidden).toBe(false);
    expect(undoButton()?.textContent).toBe('Undo');
  });

  it('does not show a banner for a close initiated elsewhere', () => {
    // No noteLocalClose: another window (or a project kill) removed it.
    onSessionRemoved('s-remote');
    expect(visible()).toBe(false);
  });

  it('offers undo only once per close', () => {
    noteLocalClose('s1', 'one');
    onSessionRemoved('s1');
    bannerEl()!.hidden = true;

    // A duplicate removed event must not re-raise the offer: the
    // tombstone it referred to is already being acted on.
    onSessionRemoved('s1');
    expect(visible()).toBe(false);
  });

  it('labels a close-and-delete-worktree undo as unrecoverable', () => {
    noteLocalClose('s1', 'api-refactor', true);
    onSessionRemoved('s1');

    expect(bannerText()).toContain('deleted its worktree');
    // Not the word "Undo": the worktree's uncommitted state does not
    // come back, so the button must not promise that it does.
    expect(undoButton()?.textContent).toBe('Reopen session');
  });

  it('restores the session the banner is about, not the last one closed', () => {
    noteLocalClose('s1', 'first');
    onSessionRemoved('s1');
    undoButton()!.click();

    expect(RestoreSession).toHaveBeenCalledWith('s1');
  });
});

describe('closeActiveSession', () => {
  it('notes the close so the banner can appear', () => {
    state.activeId = 's1';
    state.sessions = [{ id: 's1', name: 'api-refactor' } as never];

    closeActiveSession();

    expect(KillSession).toHaveBeenCalledWith('s1', false);
    onSessionRemoved('s1');
    expect(visible()).toBe(true);
    expect(bannerText()).toContain('api-refactor');
  });

  it('does nothing with no active session', () => {
    state.activeId = null;
    closeActiveSession();
    expect(KillSession).not.toHaveBeenCalled();
  });
});

describe('reopen last closed', () => {
  it('sends an empty id so the daemon picks the newest', () => {
    // Resolving "the last one" client-side would race the retention
    // prune between listing and restoring.
    reopenLastClosedSession();
    expect(RestoreSession).toHaveBeenCalledWith('');
  });
});

describe('restore outcome', () => {
  it('reports a clean undo without claiming scrollback came back', () => {
    onSessionRestored({ session_id: 's1' });

    expect(bannerText()).toContain('Scrollback is gone');
    expect(undoButton()?.hidden).toBe(true);
  });

  it('reports degradation instead of a bare success', () => {
    onSessionRestored({
      session_id: 's1',
      worktree_lost: true,
      conversation_lost: true,
    });

    const text = bannerText();
    expect(text).toContain('worktree could not be restored');
    expect(text).toContain('new conversation');
  });

  it('surfaces the recovery patch path when the worktree was deleted', () => {
    onSessionRestored({
      session_id: 's1',
      worktree_recreated: true,
      patch_path: '/state/closed/s1.patch',
    });

    const text = bannerText();
    expect(text).toContain('/state/closed/s1.patch');
    expect(text).toContain('rebuilt from its branch');
  });

  it('distinguishes a skipped patch from no patch at all', () => {
    // Empty patch_path plus patch_skipped means work WAS at stake and
    // could not be saved — the opposite of "nothing to save".
    onSessionRestored({ session_id: 's1', patch_skipped: true });
    expect(bannerText()).toContain('too large to save');
  });

  it('reads camelCase payloads too', () => {
    // Wire JSON is snake_case, but the boundary reads both.
    onSessionRestored({ session_id: 's1', worktreeLost: true });
    expect(bannerText()).toContain('worktree could not be restored');
  });
});
