// lib/whats-new.ts — the What's New list's grouping, ordering and unread rule.
import { describe, expect, it } from 'vitest';
import {
  compareVersions,
  FEATURES,
  type Feature,
  groupByVersion,
  hasUnread,
  latestVersion,
  plannedOf,
  UNRELEASED,
} from '../../src/lib/whats-new.js';

const f = (title: string, status: string, since?: string): Feature => ({
  title,
  status,
  ...(since ? { since } : {}),
});

describe('compareVersions', () => {
  // The two cases a plain string sort gets backwards, which is the whole
  // reason this function exists.
  it('orders by number, not lexically', () => {
    expect(compareVersions('2.10.0', '2.9.0')).toBeGreaterThan(0);
    expect(compareVersions('2.9.0', '2.10.0')).toBeLessThan(0);
  });

  it('sorts a pre-release before its release', () => {
    expect(compareVersions('2.0.0', '2.0.0-alpha.2')).toBeGreaterThan(0);
    expect(compareVersions('2.0.0-alpha.1', '2.0.0-alpha.2')).toBeLessThan(0);
  });

  it('keeps every pre-release identifier, hyphens included', () => {
    // split('-', 2) drops the tail, making these two compare equal.
    expect(compareVersions('2.0.0-rc-2', '2.0.0-rc-1')).toBeGreaterThan(0);
  });

  it('compares numeric pre-release identifiers as numbers', () => {
    // A whole-string compare puts alpha.10 before alpha.9.
    expect(compareVersions('2.0.0-alpha.10', '2.0.0-alpha.9')).toBeGreaterThan(
      0,
    );
    // A shorter identifier list is the lower precedence one.
    expect(compareVersions('2.0.0-alpha', '2.0.0-alpha.1')).toBeLessThan(0);
  });

  it('is zero for equal versions', () => {
    expect(compareVersions('2.6.0', '2.6.0')).toBe(0);
  });
});

describe('groupByVersion', () => {
  const list = [
    f('a', 'shipped', '2.0.0'),
    f('b', 'shipped', '2.10.0'),
    f('c', 'shipped', '2.0.0'),
    f('d', 'shipped', '2.9.0'),
    f('later', 'planned'),
    f('no-since', 'shipped'),
  ];

  it('returns versions newest first', () => {
    expect(groupByVersion(list).map((g) => g.version)).toEqual([
      '2.10.0',
      '2.9.0',
      '2.0.0',
    ]);
  });

  it('buckets every shipped entry with a since exactly once', () => {
    const titles = groupByVersion(list).flatMap((g) =>
      g.entries.map((e) => e.title),
    );
    expect(titles.sort()).toEqual(['a', 'b', 'c', 'd']);
  });

  it('sorts Unreleased above every stamped release', () => {
    const withUnreleased = [...list, f('new', 'shipped', UNRELEASED)];
    expect(groupByVersion(withUnreleased).map((g) => g.version)).toEqual([
      UNRELEASED,
      '2.10.0',
      '2.9.0',
      '2.0.0',
    ]);
  });

  it('excludes planned entries', () => {
    const titles = groupByVersion(list).flatMap((g) =>
      g.entries.map((e) => e.title),
    );
    expect(titles).not.toContain('later');
  });
});

describe('plannedOf', () => {
  it('returns only planned entries, in file order', () => {
    const list = [
      f('one', 'planned'),
      f('shipped', 'shipped', '2.0.0'),
      f('two', 'planned'),
    ];
    expect(plannedOf(list).map((e) => e.title)).toEqual(['one', 'two']);
  });
});

describe('hasUnread', () => {
  it('is true when nothing is stored', () => {
    // The update-into-this-release case: whoever gets the release that adds
    // the modal has no stored value, and is exactly who should see the dot.
    expect(hasUnread('2.6.0', null)).toBe(true);
  });

  it('is false once the latest version has been seen', () => {
    expect(hasUnread('2.6.0', '2.6.0')).toBe(false);
  });

  it('is true when a newer version has shipped since', () => {
    expect(hasUnread('2.7.0', '2.6.0')).toBe(true);
  });

  it('does not nag on a downgrade', () => {
    expect(hasUnread('2.6.0', '2.7.0')).toBe(false);
  });

  it('treats an unparseable stored value as unread', () => {
    // Once — opening the modal rewrites the key with a real version.
    expect(hasUnread('2.6.0', 'garbage')).toBe(true);
    expect(hasUnread('2.6.0', '')).toBe(true);
  });

  it('is false when the list has no shipped versions at all', () => {
    expect(hasUnread(null, null)).toBe(false);
  });
});

describe('latestVersion', () => {
  it('ignores the Unreleased bucket', () => {
    // Otherwise every dev build shows the dot forever: opening writes a real
    // release, which can never catch up to an Unreleased frontier.
    const list = [
      f('new', 'shipped', UNRELEASED),
      f('old', 'shipped', '2.6.0'),
    ];
    expect(latestVersion(list)).toBe('2.6.0');
    expect(hasUnread(latestVersion(list), '2.6.0')).toBe(false);
  });

  it('is null when everything shipped is unreleased', () => {
    expect(latestVersion([f('new', 'shipped', UNRELEASED)])).toBeNull();
  });
});

describe('the bundled site/features.json', () => {
  // site/build.mjs asserts this too, but nothing in .github/workflows/ci.yml
  // builds site/ — only pages.yml does, and only on main. So this copy is
  // what actually gates a PR. A shipped entry with no `since` renders on the
  // website and silently vanishes from the modal.
  it('gives every shipped feature a semver since', () => {
    const bad = FEATURES.filter(
      (x) =>
        x.status === 'shipped' &&
        x.since !== UNRELEASED &&
        !/^\d+\.\d+\.\d+/.test(x.since ?? ''),
    );
    expect(bad.map((x) => x.title)).toEqual([]);
  });

  it('has at least one shipped version to show', () => {
    expect(latestVersion()).not.toBeNull();
  });
});
