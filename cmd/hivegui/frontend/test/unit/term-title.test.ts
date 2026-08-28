import { describe, it, expect } from 'vitest';
import { displayTitle, titleOnlyChange } from '../../src/lib/term-title.js';

describe('displayTitle', () => {
  it('returns the title when it says something the name does not', () => {
    expect(displayTitle('npm run build', 'api')).toBe('npm run build');
  });

  it('suppresses an absent title', () => {
    expect(displayTitle('', 'api')).toBe('');
    expect(displayTitle(undefined, 'api')).toBe('');
  });

  it('suppresses a whitespace-only title', () => {
    expect(displayTitle('   ', 'api')).toBe('');
  });

  it('suppresses a title that just echoes the session name', () => {
    expect(displayTitle('api', 'api')).toBe('');
    expect(displayTitle('  api  ', 'api')).toBe('');
  });

  it('trims surrounding whitespace off a real title', () => {
    expect(displayTitle('  building  ', 'api')).toBe('building');
  });

  it('handles a missing session name', () => {
    expect(displayTitle('building', undefined)).toBe('building');
    expect(displayTitle('', undefined)).toBe('');
  });
});

describe('titleOnlyChange', () => {
  // Typed loosely on purpose: titleOnlyChange walks whatever keys it is
  // handed, and one case below adds a key SessionInfo does not declare.
  const base: Record<string, unknown> & { title?: string } = {
    id: 'a',
    name: 'api',
    order: 0,
    alive: true,
    title: 'one',
  };

  it('is true when only the title moved', () => {
    expect(titleOnlyChange(base, { ...base, title: 'two' })).toBe(true);
  });

  it('is false when the title is unchanged', () => {
    expect(titleOnlyChange(base, { ...base })).toBe(false);
  });

  it('is false when another field moved alongside the title', () => {
    expect(titleOnlyChange(base, { ...base, title: 'two', name: 'web' })).toBe(
      false,
    );
    expect(titleOnlyChange(base, { ...base, title: 'two', alive: false })).toBe(
      false,
    );
    expect(titleOnlyChange(base, { ...base, title: 'two', order: 1 })).toBe(
      false,
    );
  });

  // A field the sidebar does not read today must still force a rebuild:
  // the walk is key-agnostic on purpose, so adding to SessionInfo can
  // never silently opt that field out of rendering.
  it('is false for a field it has never heard of', () => {
    expect(
      titleOnlyChange(base, { ...base, title: 'two', somethingNew: 'x' }),
    ).toBe(false);
  });

  it('treats an absent title as empty rather than a change', () => {
    const { title: _drop, ...untitled } = base;
    expect(titleOnlyChange(untitled, { ...untitled, title: '' })).toBe(false);
    expect(titleOnlyChange(untitled, { ...untitled, title: 'one' })).toBe(true);
  });

  it('is false when either side is missing', () => {
    expect(titleOnlyChange(undefined, base)).toBe(false);
    expect(titleOnlyChange(base, undefined)).toBe(false);
  });
});
