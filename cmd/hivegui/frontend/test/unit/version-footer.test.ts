import { describe, it, expect } from 'vitest';
import { formatBinary } from '../../src/app/version-footer.js';

describe('formatBinary', () => {
  it('renders name, release and build when all are known', () => {
    expect(formatBinary('hive', 'v0.4.2', 'a3f9c1')).toBe(
      'hive v0.4.2 (a3f9c1)',
    );
  });

  it('omits the release for a daemon predating the Release wire field', () => {
    // buildinfo.Version() never returns "" on a current build, so an
    // empty release means an older daemon — render build-only rather
    // than leaving a hole where the version should be.
    expect(formatBinary('hived', '', 'b7e220')).toBe('hived (b7e220)');
  });

  it('omits the empty parens when the build is unknown', () => {
    expect(formatBinary('hive', 'v0.4.2', '')).toBe('hive v0.4.2');
  });

  it('says so explicitly when nothing is known', () => {
    expect(formatBinary('hived', '', '')).toBe('hived (unknown build)');
  });
});
