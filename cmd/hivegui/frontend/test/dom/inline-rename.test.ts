// @vitest-environment jsdom
//
// The shared inline-rename control flow (src/app/inline-rename.ts) — the
// module-level registration in particular, which is what keyboard.ts's
// FIRST ladder branch reads. Get that wrong in either direction and the
// symptom is the same class of bug: either Escape closes the panel and
// silently discards an edit, or a rename nobody can see swallows every
// keystroke in the app.
//
// The mount/unmount/commit contract is exercised through its callers
// (sidebar-dblclick-rename, worktrees); what is pinned here is the
// identity guard React cleanups depend on.
import { describe, it, expect, vi, beforeEach } from 'vitest';
import {
  beginInlineRename,
  inlineRenameActive,
  cancelInlineRename,
  cancelInlineRenameFor,
} from '../../src/app/inline-rename.js';

function start(value = 'name') {
  const host = document.createElement('div');
  document.body.append(host);
  const onCommit = vi.fn();
  const onDone = vi.fn();
  const input = beginInlineRename({
    value,
    className: 'name-input',
    mount: (el) => host.append(el),
    unmount: (el) => el.remove(),
    onCommit,
    onDone,
  });
  return { input, host, onCommit, onDone };
}

beforeEach(() => {
  cancelInlineRename();
  document.body.innerHTML = '';
});

describe('cancelInlineRenameFor', () => {
  it('cancels the edit it owns', () => {
    const { input, onDone, onCommit } = start();
    expect(inlineRenameActive()).toBe(true);

    expect(cancelInlineRenameFor(input)).toBe(true);

    expect(inlineRenameActive()).toBe(false);
    expect(input.isConnected).toBe(false);
    expect(onDone).toHaveBeenCalled();
    expect(onCommit).not.toHaveBeenCalled();
  });

  // The false branch is the whole reason this function exists rather
  // than a bare cancelInlineRename(): a React cleanup runs LATE — after
  // its own edit finished, and possibly after an unrelated one started —
  // and cancelling that one would throw away an edit the user is still
  // typing.
  it('leaves an edit that started after it alone', () => {
    const first = start('first');
    // The first edit finishes on its own (Enter), then a second begins.
    first.input.value = 'committed';
    first.input.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }),
    );
    const second = start('second');
    second.input.value = 'still typing';

    // The first row's React cleanup finally runs.
    expect(cancelInlineRenameFor(first.input)).toBe(false);

    expect(inlineRenameActive()).toBe(true);
    expect(second.input.isConnected).toBe(true);
    expect(second.input.value).toBe('still typing');
    expect(second.onDone).not.toHaveBeenCalled();
  });

  it('is a no-op when nothing is being renamed', () => {
    const input = document.createElement('input');
    expect(cancelInlineRenameFor(input)).toBe(false);
    expect(inlineRenameActive()).toBe(false);
  });
});
