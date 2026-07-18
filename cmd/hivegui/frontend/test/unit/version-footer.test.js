import { describe, it, expect, beforeEach, vi } from 'vitest';

// version-footer.js imports EventsOn from the bridge, which re-exports
// the Wails runtime — absent under vitest (the vite-plugin substitution
// only applies to the Playwright harnesses). Only EventsOn is needed
// here; the functions under test are pure and take their elements
// injected, so nothing else off the bridge is touched.
vi.mock('../../src/bridge.js', () => ({ EventsOn: vi.fn() }));

const { formatBinary, renderVersionFooter } = await import('../../src/app/version-footer.js');

// Minimal stand-ins for the footer elements. renderVersionFooter takes
// them injected precisely so this file needs no live document.
function makeRoot() {
  const classes = new Set();
  return {
    classes,
    classList: {
      toggle(name, on) {
        if (on) classes.add(name);
        else classes.delete(name);
      },
    },
  };
}

let els;
let root;
beforeEach(() => {
  root = makeRoot();
  els = {
    root,
    gui: { textContent: '' },
    daemon: { textContent: '', hidden: true },
  };
});

describe('formatBinary', () => {
  it('renders name, release and build when all are known', () => {
    expect(formatBinary('hive', 'v0.4.2', 'a3f9c1')).toBe('hive v0.4.2 (a3f9c1)');
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

describe('renderVersionFooter', () => {
  it('collapses to one line when the builds match', () => {
    renderVersionFooter({
      severity: 'match',
      guiBuild: 'a3f9c1',
      daemonBuild: 'a3f9c1',
      guiRelease: 'v0.4.2',
      daemonRelease: 'v0.4.2',
    }, els);

    expect(els.gui.textContent).toBe('hive v0.4.2 (a3f9c1)');
    expect(els.daemon.hidden).toBe(true);
    expect(els.daemon.textContent).toBe('');
    expect(root.classes.has('mismatch')).toBe(false);
  });

  it('expands to two lines and flags mismatch when builds differ', () => {
    renderVersionFooter({
      severity: 'mismatch',
      guiBuild: 'a3f9c1',
      daemonBuild: 'b7e220',
      guiRelease: 'v0.4.2',
      daemonRelease: 'v0.4.1',
    }, els);

    expect(els.gui.textContent).toBe('hive v0.4.2 (a3f9c1)');
    expect(els.daemon.textContent).toBe('hived v0.4.1 (b7e220)');
    expect(els.daemon.hidden).toBe(false);
    expect(root.classes.has('mismatch')).toBe(true);
  });

  it('falls back to build-only for an older daemon that sends no release', () => {
    renderVersionFooter({
      severity: 'mismatch',
      guiBuild: 'a3f9c1',
      daemonBuild: 'b7e220',
      guiRelease: 'v0.4.2',
      daemonRelease: '',
    }, els);

    expect(els.daemon.textContent).toBe('hived (b7e220)');
    expect(els.daemon.textContent).not.toContain('()');
  });

  it('shows both lines on unknown severity but does not flag mismatch', () => {
    // "unknown" means one side didn't advertise a build. Worth showing
    // in full, but it isn't evidence of an actual version conflict, so
    // it must not get the warning colour.
    renderVersionFooter({
      severity: 'unknown',
      guiBuild: 'a3f9c1',
      daemonBuild: '',
      guiRelease: 'v0.4.2',
      daemonRelease: '',
    }, els);

    expect(els.daemon.hidden).toBe(false);
    expect(els.daemon.textContent).toBe('hived (unknown build)');
    expect(root.classes.has('mismatch')).toBe(false);
  });

  it('clears the mismatch flag when a later connect matches', () => {
    renderVersionFooter({
      severity: 'mismatch', guiBuild: 'a', daemonBuild: 'b',
      guiRelease: 'v1', daemonRelease: 'v2',
    }, els);
    expect(root.classes.has('mismatch')).toBe(true);

    renderVersionFooter({
      severity: 'match', guiBuild: 'a', daemonBuild: 'a',
      guiRelease: 'v1', daemonRelease: 'v1',
    }, els);
    expect(root.classes.has('mismatch')).toBe(false);
    expect(els.daemon.hidden).toBe(true);
  });

  it('ignores a null payload without throwing', () => {
    expect(() => renderVersionFooter(null, els)).not.toThrow();
    expect(els.gui.textContent).toBe('');
  });
});
