import { describe, it, expect } from 'vitest';
import { relativeAge } from '../../src/lib/ideas.js';
import {
  addIdea,
  appStore,
  openIdeasOf,
  removeIdea,
  resetStore,
  setIdeas,
  updateIdea,
} from '../../src/store/store.js';
import type { IdeaInfo } from '../../src/app/state.js';

const NOW = Date.parse('2026-09-05T12:00:00Z');
const ago = (ms: number) => new Date(NOW - ms).toISOString();

describe('relativeAge', () => {
  it('picks the coarsest unit that fits', () => {
    expect(relativeAge(ago(5_000), NOW)).toBe('just now');
    expect(relativeAge(ago(90_000), NOW)).toBe('1m ago');
    expect(relativeAge(ago(3 * 3600_000), NOW)).toBe('3h ago');
    expect(relativeAge(ago(50 * 3600_000), NOW)).toBe('2d ago');
  });

  it('does not render a negative age for a daemon clock that is ahead', () => {
    expect(relativeAge(new Date(NOW + 5_000).toISOString(), NOW)).toBe(
      'just now',
    );
  });

  it('renders nothing for an unparseable timestamp', () => {
    expect(relativeAge('not a date', NOW)).toBe('');
  });
});

function idea(over: Partial<IdeaInfo> = {}): IdeaInfo {
  return {
    id: 'i1',
    project_id: 'p1',
    kind: 'idea',
    text: 'a note',
    status: 'open',
    created: ago(1000),
    updated: ago(1000),
    ...over,
  };
}

describe('idea store', () => {
  it('keeps the list newest-first however entries arrive', () => {
    resetStore();
    setIdeas([
      idea({ id: 'old', created: ago(10 * 3600_000) }),
      idea({ id: 'new', created: ago(60_000) }),
    ]);
    addIdea(idea({ id: 'mid', created: ago(3600_000) }));
    expect(appStore.getState().ideas.map((i) => i.id)).toEqual([
      'new',
      'mid',
      'old',
    ]);
  });

  it('orders same-second ideas by id rather than unstably', () => {
    // A burst of `hived idea add` lands several ideas in the same
    // second; a comparator that never returns 0 lets them swap places
    // on every re-sort, moving a row under the cursor.
    resetStore();
    const same = ago(1000);
    setIdeas([
      idea({ id: 'c', created: same }),
      idea({ id: 'a', created: same }),
      idea({ id: 'b', created: same }),
    ]);
    const first = appStore.getState().ideas.map((i) => i.id);
    expect(first).toEqual(['a', 'b', 'c']);
    // Re-sorting through another entry point must not reshuffle them.
    addIdea(idea({ id: 'd', created: ago(10) }));
    expect(appStore.getState().ideas.map((i) => i.id)).toEqual([
      'd',
      'a',
      'b',
      'c',
    ]);
  });

  it('treats an update for an unknown idea as an add', () => {
    resetStore();
    updateIdea(idea({ id: 'unseen' }));
    expect(appStore.getState().ideas.map((i) => i.id)).toEqual(['unseen']);
  });

  it('drops a removed idea and ignores an unknown removal', () => {
    resetStore();
    setIdeas([idea({ id: 'i1' }), idea({ id: 'i2' })]);
    removeIdea('nope');
    expect(appStore.getState().ideas).toHaveLength(2);
    removeIdea('i1');
    expect(appStore.getState().ideas.map((i) => i.id)).toEqual(['i2']);
  });

  it('counts only this project’s ideas that are not done', () => {
    const list = [
      idea({ id: 'a', project_id: 'p1', status: 'open' }),
      idea({ id: 'b', project_id: 'p1', status: 'started' }),
      idea({ id: 'c', project_id: 'p1', status: 'done' }),
      idea({ id: 'd', project_id: 'p2', status: 'open' }),
    ];
    expect(openIdeasOf(list, 'p1').map((i) => i.id)).toEqual(['a', 'b']);
  });
});
