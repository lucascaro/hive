import { describe, it, expect } from 'vitest';
import {
  updateButtonState,
  describeVersion,
  CHANNEL_LATEST,
  CHANNEL_RELEASE,
} from '../../src/lib/update-state.js';

const MAC = true;
const NOT_MAC = false;

describe('updateButtonState', () => {
  it('hides the button when there is nothing to do', () => {
    expect(updateButtonState(null, MAC).action).toBe('none');
    expect(updateButtonState(null, MAC).label).toBe('');

    const upToDate = updateButtonState(
      { available: false, current: '2.4.0', stage: 'idle' },
      MAC,
    );
    expect(upToDate.action).toBe('none');
    expect(upToDate.label).toBe('');
    expect(upToDate.status).toContain('2.4.0');
  });

  it('offers Update when one is available', () => {
    const s = updateButtonState(
      {
        available: true,
        current: '2.4.0',
        latest: '2.5.0',
        stage: 'available',
        channel: CHANNEL_RELEASE,
      },
      MAC,
    );
    expect(s.label).toBe('Update');
    expect(s.action).toBe('start');
    expect(s.disabled).toBe(false);
    expect(s.status).toContain('2.5.0');
  });

  it('walks Update -> Updating -> Restart', () => {
    const staging = updateButtonState(
      { available: true, stage: 'staging', message: 'Downloading…' },
      MAC,
    );
    expect(staging.label).toBe('Updating…');
    expect(staging.disabled).toBe(true);
    expect(staging.action).toBe('none');
    expect(staging.status).toBe('Downloading…');

    const ready = updateButtonState(
      { available: true, stage: 'ready', message: 'Update ready' },
      MAC,
    );
    expect(ready.label).toBe('Restart');
    expect(ready.action).toBe('restart');
    expect(ready.disabled).toBe(false);
  });

  it('offers a retry after a failure, with the reason', () => {
    const s = updateButtonState(
      { available: true, stage: 'error', message: 'checksum mismatch' },
      MAC,
    );
    expect(s.label).toBe('Retry');
    expect(s.action).toBe('start');
    expect(s.status).toBe('checksum mismatch');
  });

  // Staging a build we cannot install would be a dead end; those
  // platforms keep the Download link instead.
  it('offers no in-app update off macOS', () => {
    const s = updateButtonState(
      { available: true, current: '2.4.0', latest: '2.5.0' },
      NOT_MAC,
    );
    expect(s.label).toBe('');
    expect(s.action).toBe('none');
    expect(s.status).toContain('manually');
  });

  it('explains a skipped check instead of claiming up to date', () => {
    const s = updateButtonState(
      { skipped: true, message: 'untagged build', current: 'dev' },
      MAC,
    );
    expect(s.action).toBe('none');
    expect(s.status).toBe('untagged build');
    expect(s.status).not.toContain('up to date');
  });
});

describe('describeVersion', () => {
  it('says version on the release channel and commit on latest', () => {
    expect(
      describeVersion({
        channel: CHANNEL_RELEASE,
        latest: '2.5.0',
        current: '2.4.0',
      }),
    ).toContain('Hive 2.5.0');
    expect(
      describeVersion({
        channel: CHANNEL_LATEST,
        latest: '8e65349',
        current: 'b5101ff',
      }),
    ).toContain('commit 8e65349');
  });
});

// The reducer is where "what will this cost me?" becomes visible. Go
// works the answer out from the staged daemon's contract; if the label
// does not follow it, every update keeps reading as destructive and
// the whole feature is invisible.
describe('updateButtonState restart kind', () => {
  it('offers Reload and promises the sessions survive', () => {
    const s = updateButtonState(
      { stage: 'ready', restartKind: 'gui', latest: '2.5.0' },
      true,
    );
    expect(s.label).toBe('Reload');
    expect(s.action).toBe('reload');
    expect(s.status).toMatch(/sessions keep running/i);
  });

  it('offers Restart and names the cost when the daemon changed', () => {
    const s = updateButtonState(
      { stage: 'ready', restartKind: 'full', latest: '2.5.0' },
      true,
    );
    expect(s.label).toBe('Restart');
    expect(s.action).toBe('restart');
    expect(s.status).toMatch(/ends every running session/i);
  });

  // A bundle staged by an older build carries no kind. Defaulting to
  // reload there would silently drop a GUI into a daemon it may not
  // understand, so the missing case must take the safe path.
  it('falls back to Restart when the kind is missing', () => {
    const s = updateButtonState({ stage: 'ready', latest: '2.5.0' }, true);
    expect(s.action).toBe('restart');
  });

  // Go's own message wins when it has one — it is more specific than
  // anything the reducer can say.
  it('prefers the backend message over its own copy', () => {
    const s = updateButtonState(
      {
        stage: 'ready',
        restartKind: 'gui',
        message: 'Update ready — reload to apply',
      },
      true,
    );
    expect(s.status).toBe('Update ready — reload to apply');
  });
});
