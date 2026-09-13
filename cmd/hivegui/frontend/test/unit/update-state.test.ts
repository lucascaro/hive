import { describe, it, expect } from 'vitest';
import {
  updateButtonState,
  describeVersion,
  CHANNEL_LATEST,
  CHANNEL_RELEASE,
} from '../../src/lib/update-state.js';

// These were MAC / NOT_MAC when the reducer sniffed the user agent.
// The flag now means "the backend says this build can install an
// update in place", which is a different question with the same shape.
const CAN_APPLY = true;
const CANNOT_APPLY = false;

describe('updateButtonState', () => {
  it('hides the button when there is nothing to do', () => {
    expect(updateButtonState(null, CAN_APPLY).action).toBe('none');
    expect(updateButtonState(null, CAN_APPLY).label).toBe('');

    const upToDate = updateButtonState(
      { available: false, current: '2.4.0', stage: 'idle' },
      CAN_APPLY,
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
      CAN_APPLY,
    );
    expect(s.label).toBe('Update');
    expect(s.action).toBe('start');
    expect(s.disabled).toBe(false);
    expect(s.status).toContain('2.5.0');
  });

  it('walks Update -> Updating -> Restart', () => {
    const staging = updateButtonState(
      { available: true, stage: 'staging', message: 'Downloading…' },
      CAN_APPLY,
    );
    expect(staging.label).toBe('Updating…');
    expect(staging.disabled).toBe(true);
    expect(staging.action).toBe('none');
    expect(staging.status).toBe('Downloading…');

    const ready = updateButtonState(
      { available: true, stage: 'ready', message: 'Update ready' },
      CAN_APPLY,
    );
    expect(ready.label).toBe('Restart');
    expect(ready.action).toBe('restart');
    expect(ready.disabled).toBe(false);
  });

  it('offers a retry after a failure, with the reason', () => {
    const s = updateButtonState(
      { available: true, stage: 'error', message: 'checksum mismatch' },
      CAN_APPLY,
    );
    expect(s.label).toBe('Retry');
    expect(s.action).toBe('start');
    expect(s.status).toBe('checksum mismatch');
  });

  // Staging a build we cannot install would be a dead end. The user
  // still gets told an update exists, and why this install cannot take
  // it — see the capability block at the bottom for where the reason
  // comes from.
  it('offers no in-app update when the backend says it cannot apply', () => {
    const s = updateButtonState(
      { available: true, current: '2.4.0', latest: '2.5.0' },
      CANNOT_APPLY,
    );
    expect(s.label).toBe('');
    expect(s.action).toBe('none');
    expect(s.status).toContain('2.5.0');
  });

  it('explains a skipped check instead of claiming up to date', () => {
    const s = updateButtonState(
      { skipped: true, message: 'untagged build', current: 'dev' },
      CAN_APPLY,
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

// The reducer used to take its own guess at the platform from
// navigator.platform, which could only ever answer "is this a Mac" —
// and so told a Windows user to "download it manually on this platform"
// even on the latest channel, which has no download. The answer now
// comes from Go, which knows the actual reason.
describe('updateButtonState capability', () => {
  it('reads canApply off the info when no flag is passed', () => {
    const allowed = updateButtonState({
      available: true,
      current: '2.4.0',
      latest: '2.5.0',
      canApply: true,
    });
    expect(allowed.label).toBe('Update');
    expect(allowed.action).toBe('start');

    const refused = updateButtonState({
      available: true,
      current: '2.4.0',
      latest: '2.5.0',
      canApply: false,
    });
    expect(refused.label).toBe('');
    expect(refused.action).toBe('none');
  });

  it('renders the backend reason rather than a platform guess', () => {
    const s = updateButtonState({
      available: true,
      current: '2.4.0',
      latest: '2.5.0',
      canApply: false,
      canApplyReason:
        'C:\\Program Files\\Hive is not writable by Hive',
    });
    expect(s.status).toContain('C:\\Program Files\\Hive is not writable');
    expect(s.status).not.toContain('on this platform');
  });

  it('still says something when the backend gave no reason', () => {
    const s = updateButtonState({
      available: true,
      current: '2.4.0',
      latest: '2.5.0',
      canApply: false,
    });
    expect(s.status).toContain('2.5.0');
    expect(s.status).toMatch(/cannot update in place/i);
  });

  // A missing canApply is a payload from a build that predates the
  // field. Treating it as "yes" would offer a button that dead-ends.
  it('treats a missing canApply as no', () => {
    const s = updateButtonState({
      available: true,
      current: '2.4.0',
      latest: '2.5.0',
    });
    expect(s.action).toBe('none');
  });
});
