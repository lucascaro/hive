import { describe, it, expect } from 'vitest';
import { displayTitle } from '../../src/lib/term-title.js';

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
