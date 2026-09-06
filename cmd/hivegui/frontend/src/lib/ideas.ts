// Pure helpers for the idea inbox. No DOM, unit-testable — the same
// split lib/worktrees.ts makes for the worktree browser.

// relativeAge renders how long ago an idea was captured, in the one
// coarse unit that fits. Coarse on purpose: the inbox is a list of
// notes to triage, where "3d" is the whole signal and a live-updating
// "2 minutes ago" is churn.
//
// `iso` is the daemon's RFC 3339 timestamp. An unparseable one renders
// as an empty string rather than "NaNs": a bad clock on one row must
// not be louder than the note itself.
export function relativeAge(iso: string, now: number = Date.now()): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return '';
  // Clamped at zero: a daemon whose clock is a second ahead of the
  // webview's would otherwise render a negative age.
  const secs = Math.max(0, Math.round((now - t) / 1000));
  if (secs < 60) return 'just now';
  const mins = Math.floor(secs / 60);
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}
