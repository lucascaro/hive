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
