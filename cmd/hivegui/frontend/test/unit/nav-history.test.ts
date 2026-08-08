import { describe, it, expect } from 'vitest';
import {
  NAV_CAP,
  createNavHistory,
  pushNav,
  goBack,
  goForward,
  pruneNav,
} from '../../src/lib/nav-history.js';

// Default exists predicate: every id is live.
const all = () => true;
// exists predicate over an explicit live set.
const only =
  (...ids: string[]) =>
  (id: string) =>
    ids.includes(id);

describe('pushNav', () => {
  it('records a departure', () => {
    const h = createNavHistory();
    pushNav(h, 'a');
    expect(h.back).toEqual(['a']);
  });

  it('ignores a falsy id — "no active session" is not a history entry', () => {
    // Landing on an empty project (view.js) or a just-killed session
    // (events.js) sets activeId to null; those must not stack entries.
    const h = createNavHistory();
    pushNav(h, null);
    pushNav(h, undefined);
    pushNav(h, '');
    expect(h.back).toEqual([]);
  });

  it('ignores a repeat of the top entry', () => {
    // setActive can fire twice for the same session (e.g. a click on the
    // already-active tile followed by a re-render), and two identical
    // entries would make back look like it did nothing.
    const h = createNavHistory();
    pushNav(h, 'a');
    pushNav(h, 'a');
    expect(h.back).toEqual(['a']);
  });

  it('truncates the forward branch — the VS Code rule', () => {
    const h = createNavHistory();
    pushNav(h, 'a');
    pushNav(h, 'b');
    // at c; go back to b -> forward holds c
    expect(goBack(h, 'c', all)).toBe('b');
    expect(h.fwd).toEqual(['c']);
    // navigate somewhere new: the c branch is discarded
    pushNav(h, 'b');
    expect(h.fwd).toEqual([]);
  });

  it('caps the back stack at NAV_CAP, dropping the oldest', () => {
    const h = createNavHistory();
    for (let i = 0; i < NAV_CAP + 10; i++) pushNav(h, `s${i}`);
    expect(h.back).toHaveLength(NAV_CAP);
    expect(h.back[0]).toBe('s10');
    expect(h.back[NAV_CAP - 1]).toBe(`s${NAV_CAP + 9}`);
  });
});

describe('goBack / goForward', () => {
  it('walks back through visited sessions', () => {
    const h = createNavHistory();
    pushNav(h, 'a'); // left a for b
    pushNav(h, 'b'); // left b for c
    expect(goBack(h, 'c', all)).toBe('b');
    expect(goBack(h, 'b', all)).toBe('a');
    expect(goBack(h, 'a', all)).toBe(null);
  });

  it('round-trips: back then forward returns to the original session', () => {
    const h = createNavHistory();
    pushNav(h, 'a');
    pushNav(h, 'b');
    expect(goBack(h, 'c', all)).toBe('b');
    expect(goForward(h, 'b', all)).toBe('c');
    expect(goForward(h, 'c', all)).toBe(null);
  });

  it('walks back two then forward two', () => {
    const h = createNavHistory();
    pushNav(h, 'a');
    pushNav(h, 'b');
    expect(goBack(h, 'c', all)).toBe('b');
    expect(goBack(h, 'b', all)).toBe('a');
    expect(goForward(h, 'a', all)).toBe('b');
    expect(goForward(h, 'b', all)).toBe('c');
  });

  it('returns null on an empty stack', () => {
    const h = createNavHistory();
    expect(goBack(h, 'a', all)).toBe(null);
    expect(goForward(h, 'a', all)).toBe(null);
  });

  it('skips sessions that no longer exist', () => {
    // b was killed while the user was in c. Back must land on a, not
    // dead-end on a session that is gone.
    const h = createNavHistory();
    pushNav(h, 'a');
    pushNav(h, 'b');
    expect(goBack(h, 'c', only('a', 'c'))).toBe('a');
  });

  it('returns null when every entry is dead', () => {
    const h = createNavHistory();
    pushNav(h, 'a');
    pushNav(h, 'b');
    expect(goBack(h, 'c', only('c'))).toBe(null);
    expect(h.back).toEqual([]);
  });

  it('skips an entry equal to the current session', () => {
    // Can happen after a dead entry is skipped past on an earlier walk.
    const h = createNavHistory();
    h.back.push('a', 'c');
    expect(goBack(h, 'c', all)).toBe('a');
  });

  it('does not stack duplicate forward entries', () => {
    const h = createNavHistory();
    pushNav(h, 'a');
    pushNav(h, 'b');
    goBack(h, 'c', all);
    expect(h.fwd).toEqual(['c']);
    goBack(h, 'b', all);
    expect(h.fwd).toEqual(['c', 'b']);
  });

  it('does not push a forward entry when there is no current session', () => {
    const h = createNavHistory();
    pushNav(h, 'a');
    expect(goBack(h, null, all)).toBe('a');
    expect(h.fwd).toEqual([]);
  });
});

describe('pruneNav', () => {
  it('drops dead ids from both stacks', () => {
    const h = createNavHistory();
    h.back.push('a', 'dead', 'b');
    h.fwd.push('c', 'dead2');
    pruneNav(h, only('a', 'b', 'c'));
    expect(h.back).toEqual(['a', 'b']);
    expect(h.fwd).toEqual(['c']);
  });

  it('keeps order of surviving entries', () => {
    const h = createNavHistory();
    h.back.push('a', 'b', 'c');
    pruneNav(h, only('a', 'c'));
    expect(h.back).toEqual(['a', 'c']);
  });
});
