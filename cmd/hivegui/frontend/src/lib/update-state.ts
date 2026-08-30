// ---------- update action button state ----------
//
// One reducer, two surfaces. The update banner and the Settings modal
// both render the same Update → Updating… → Restart button, and both
// are driven by the same `main.UpdateInfo` (from CheckForUpdate /
// UpdateStatus, and from "update:progress" events while staging).
// Deriving the label in one place is what stops the two from
// disagreeing after, say, a staging failure that only one of them saw.
//
// Pure: no DOM, no bridge calls. The callers do the rendering.

/** The subset of main.UpdateInfo this module reads. Structural rather
 * than importing the generated class so the unit test can pass plain
 * objects, and so a binding regeneration can't break the tests. */
export interface UpdateInfoLike {
  available?: boolean;
  current?: string;
  latest?: string;
  url?: string;
  skipped?: boolean;
  channel?: string;
  stage?: string;
  message?: string;
}

/** Actions the button can perform. `none` means it should be hidden. */
export type UpdateAction = 'none' | 'start' | 'restart';

export interface UpdateButtonState {
  label: string;
  action: UpdateAction;
  disabled: boolean;
  /** Status line shown next to the button. */
  status: string;
}

export const CHANNEL_RELEASE = 'release';
export const CHANNEL_LATEST = 'latest';

/** Formats the version identity for a channel — a release version
 * ("2.4.0") reads differently from a commit ("8e65349"), and saying
 * "Hive 8e65349 is available" would be nonsense. */
export function describeVersion(info: UpdateInfoLike): string {
  const latest = info.latest || '';
  const current = info.current || '';
  if (info.channel === CHANNEL_LATEST) {
    return `commit ${latest} is available (you have ${current})`;
  }
  return `Hive ${latest} is available (you have ${current})`;
}

/** Derives the button from the current update state.
 *
 * `canApply` is false on platforms where the in-app swap isn't
 * implemented (everything but macOS): there the button never offers to
 * stage a build it couldn't install, and the banner's Download link
 * stays the way out. */
export function updateButtonState(
  info: UpdateInfoLike | null,
  canApply: boolean,
): UpdateButtonState {
  const hidden: UpdateButtonState = {
    label: '',
    action: 'none',
    disabled: true,
    status: '',
  };
  if (!info) return hidden;

  // An error is worth showing whatever the platform: it's the only way
  // the user learns why the update they asked for didn't happen. The
  // button offers a retry rather than dead-ending.
  if (info.stage === 'error') {
    return {
      // No retry button where we could not have staged in the first
      // place — the message still explains what happened.
      label: canApply ? 'Retry' : '',
      action: canApply ? 'start' : 'none',
      disabled: false,
      status: info.message || 'Update failed.',
    };
  }
  if (info.stage === 'staging') {
    return {
      label: 'Updating…',
      action: 'none',
      disabled: true,
      status: info.message || 'Working…',
    };
  }
  if (info.stage === 'ready') {
    return {
      label: 'Restart',
      action: 'restart',
      disabled: false,
      status: info.message || 'Update ready — restart to apply.',
    };
  }
  if (info.skipped) {
    return {
      ...hidden,
      status: info.message || "Can't check for updates for this build.",
    };
  }
  if (info.available) {
    return {
      // An in-app Update button on a platform that cannot install one
      // would be a dead end; the Download link is the way out there.
      label: canApply ? 'Update' : '',
      action: canApply ? 'start' : 'none',
      disabled: false,
      status: canApply
        ? `${describeVersion(info)}.`
        : `${describeVersion(info)} — download it manually on this platform.`,
    };
  }
  return {
    ...hidden,
    status: info.current ? `You're on ${info.current}.` : '',
  };
}
