// @vitest-environment jsdom
//
// Sidebar footer version/build readout (src/components/VersionFooter.tsx,
// mounted on #sidebar-hints). It takes its own "daemon:stale" subscription
// — see the component's file header for why that is not read off the
// store the banner writes — so these tests drive it by emitting the raw
// Wails event, the way app.go's daemonVersionEvent() would.
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { act, render } from '@testing-library/react';
import { VersionFooter } from '../../src/components/VersionFooter.js';
import type { DaemonStaleEvent } from '../../src/app/version-footer.js';

const bridge = vi.hoisted(() => ({ EventsOn: vi.fn() }));
vi.mock('../../src/bridge.js', () => bridge);

// The handler registered via EventsOn, replayed the way Go would. Wrapped
// in act() since it fires the listener directly rather than through a DOM
// event RTL already wraps.
function emit(payload: DaemonStaleEvent | null) {
  const call = bridge.EventsOn.mock.calls.find(
    (c: unknown[]) => c[0] === 'daemon:stale',
  );
  if (!call) throw new Error('daemon:stale listener was never registered');
  act(() => {
    (call[1] as (p: DaemonStaleEvent | null) => void)(payload);
  });
}

function mount(): HTMLElement {
  const root = document.createElement('div');
  root.id = 'sidebar-hints';
  document.body.appendChild(root);
  render(<VersionFooter root={root} />, { container: root });
  return root;
}

let root: HTMLElement;

beforeEach(() => {
  document.body.innerHTML = '';
  bridge.EventsOn.mockClear();
  root = mount();
});

function guiText(): string | null {
  return document.getElementById('ver-gui')?.textContent ?? null;
}

function daemonEl(): HTMLElement {
  return document.getElementById('ver-daemon') as HTMLElement;
}

describe('VersionFooter', () => {
  it('collapses to one line when the builds match', () => {
    emit({
      severity: 'match',
      guiBuild: 'a3f9c1',
      daemonBuild: 'a3f9c1',
      guiRelease: 'v0.4.2',
      daemonRelease: 'v0.4.2',
    });

    expect(guiText()).toBe('hive v0.4.2 (a3f9c1)');
    expect(daemonEl().hidden).toBe(true);
    expect(daemonEl().textContent).toBe('');
    expect(root.classList.contains('mismatch')).toBe(false);
  });

  it('expands to two lines and flags mismatch when builds differ', () => {
    emit({
      severity: 'mismatch',
      guiBuild: 'a3f9c1',
      daemonBuild: 'b7e220',
      guiRelease: 'v0.4.2',
      daemonRelease: 'v0.4.1',
    });

    expect(guiText()).toBe('hive v0.4.2 (a3f9c1)');
    expect(daemonEl().textContent).toBe('hived v0.4.1 (b7e220)');
    expect(daemonEl().hidden).toBe(false);
    expect(root.classList.contains('mismatch')).toBe(true);
  });

  it('falls back to build-only for an older daemon that sends no release', () => {
    emit({
      severity: 'mismatch',
      guiBuild: 'a3f9c1',
      daemonBuild: 'b7e220',
      guiRelease: 'v0.4.2',
      daemonRelease: '',
    });

    expect(daemonEl().textContent).toBe('hived (b7e220)');
    expect(daemonEl().textContent).not.toContain('()');
  });

  it('shows both lines on unknown severity but does not flag mismatch', () => {
    // "unknown" means one side didn't advertise a build. Worth showing
    // in full, but it isn't evidence of an actual version conflict, so
    // it must not get the warning colour.
    emit({
      severity: 'unknown',
      guiBuild: 'a3f9c1',
      daemonBuild: '',
      guiRelease: 'v0.4.2',
      daemonRelease: '',
    });

    expect(daemonEl().hidden).toBe(false);
    expect(daemonEl().textContent).toBe('hived (unknown build)');
    expect(root.classList.contains('mismatch')).toBe(false);
  });

  it('clears the mismatch flag when a later connect matches', () => {
    emit({
      severity: 'mismatch',
      guiBuild: 'a',
      daemonBuild: 'b',
      guiRelease: 'v1',
      daemonRelease: 'v2',
    });
    expect(root.classList.contains('mismatch')).toBe(true);

    emit({
      severity: 'match',
      guiBuild: 'a',
      daemonBuild: 'a',
      guiRelease: 'v1',
      daemonRelease: 'v1',
    });
    expect(root.classList.contains('mismatch')).toBe(false);
    expect(daemonEl().hidden).toBe(true);
  });

  it('ignores a null payload without throwing', () => {
    expect(() => emit(null)).not.toThrow();
    expect(guiText()).toBe('');
  });
});
