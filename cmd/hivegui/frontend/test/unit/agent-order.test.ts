import { describe, expect, it } from 'vitest';
import {
  EMPTY_AGENT_PREFS,
  moveIndex,
  moveItem,
  orderAgents,
  parseAgentPrefs,
} from '../../src/lib/agent-order';

// Catalog order, as the Go side returns it.
const CATALOG = ['shell', 'claude', 'codex', 'gemini', 'pi'].map((id) => ({
  id,
}));
const ids = (list: { id: string }[]) => list.map((a) => a.id);

describe('orderAgents', () => {
  it('with no prefs, is the usage sort with catalog-order ties', () => {
    // The launcher's pre-existing rule, unchanged for a fresh profile.
    const usage = { codex: 5, pi: 5, claude: 9 };
    expect(ids(orderAgents(CATALOG, usage, EMPTY_AGENT_PREFS))).toEqual([
      'claude',
      'codex',
      'pi',
      'shell',
      'gemini',
    ]);
  });

  it('drops hidden agents', () => {
    const prefs = { hidden: ['shell', 'gemini'], pinned: [] };
    expect(ids(orderAgents(CATALOG, {}, prefs))).toEqual([
      'claude',
      'codex',
      'pi',
    ]);
  });

  it('puts pinned agents first in pinned order, whatever their usage', () => {
    const usage = { claude: 100, shell: 50 };
    const prefs = { hidden: [], pinned: ['pi', 'gemini'] };
    expect(ids(orderAgents(CATALOG, usage, prefs))).toEqual([
      'pi',
      'gemini',
      'claude',
      'shell',
      'codex',
    ]);
  });

  it('hides an agent that is both hidden and pinned', () => {
    const prefs = { hidden: ['pi'], pinned: ['pi', 'codex'] };
    expect(ids(orderAgents(CATALOG, {}, prefs))).toEqual([
      'codex',
      'shell',
      'claude',
      'gemini',
    ]);
  });

  it('ignores ids no longer in the catalog', () => {
    const prefs = { hidden: ['deleted-a'], pinned: ['deleted-b', 'codex'] };
    expect(ids(orderAgents(CATALOG, {}, prefs))).toEqual([
      'codex',
      'shell',
      'claude',
      'gemini',
      'pi',
    ]);
  });
});

describe('parseAgentPrefs', () => {
  it('reads a stored value', () => {
    expect(parseAgentPrefs('{"hidden":["a"],"pinned":["b","c"]}')).toEqual({
      hidden: ['a'],
      pinned: ['b', 'c'],
    });
  });

  it.each([
    ['missing', null],
    ['empty', ''],
    ['not JSON', '{nope'],
    ['not an object', '"x"'],
    ['null', 'null'],
  ])('degrades %s to no prefs', (_label, raw) => {
    expect(parseAgentPrefs(raw)).toEqual(EMPTY_AGENT_PREFS);
  });

  it('keeps only string ids from the right fields', () => {
    expect(
      parseAgentPrefs('{"hidden":"shell","pinned":["a",3,null,"b"]}'),
    ).toEqual({ hidden: [], pinned: ['a', 'b'] });
  });
});

describe('moveIndex', () => {
  // [from, target, above] → final index of the dragged item.
  it.each([
    [0, 2, true, 1], // down, above the target: removal shifts target up
    [0, 2, false, 2], // down, below the target
    [3, 1, true, 1], // up, above the target
    [3, 1, false, 2], // up, below the target
    [1, 1, true, 1], // onto itself: no-op
    [1, 0, false, 1], // just below its upper neighbour: no-op
    [1, 2, true, 1], // just above its lower neighbour: no-op
  ])('from %i onto %i (above=%s) lands at %i', (from, target, above, want) => {
    expect(moveIndex(from, target, above)).toBe(want);
  });

  it('agrees with moveItem on the resulting order', () => {
    const list = ['a', 'b', 'c', 'd'];
    // Drag a below d.
    expect(moveItem(list, 0, moveIndex(0, 3, false))).toEqual([
      'b',
      'c',
      'd',
      'a',
    ]);
    // Drag d above a.
    expect(moveItem(list, 3, moveIndex(3, 0, true))).toEqual([
      'd',
      'a',
      'b',
      'c',
    ]);
  });
});
