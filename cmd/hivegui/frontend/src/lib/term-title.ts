// Shared presentation rule for the OSC-set terminal window title.
//
// Two surfaces render this string — the grid tile header
// (app/session-term.ts) and the sidebar row (app/sidebar.ts) — and they
// must agree on when there is nothing worth showing, or the same session
// reads as "busy" in one place and idle in the other.

/**
 * displayTitle returns the window title to show for a session, or '' when
 * the title carries no information the session name doesn't already.
 *
 * A title equal to the session name is suppressed because many shells set
 * the title from the command or directory, which is frequently just the
 * name the user typed — rendering it would produce "foo / foo".
 * Whitespace-only titles are treated as absent for the same reason.
 */
export function displayTitle(title?: string, name?: string): string {
  const t = (title ?? '').trim();
  if (!t) return '';
  if (t === (name ?? '').trim()) return '';
  return t;
}

/**
 * titleOnlyChange reports whether two snapshots of the same session
 * differ in the window title and in nothing else.
 *
 * The daemon reports a title change as an ordinary SESSION_EVENT(updated),
 * indistinguishable from a rename or a phase transition, and the handler's
 * response to `updated` is a full sidebar rebuild plus a tile-header and
 * tray refresh. Titles change while a program runs, so that is the wrong
 * amount of work — this is the test that lets the caller take the cheap
 * in-place path instead.
 *
 * Every other key is compared generically rather than against a list of
 * the fields the sidebar happens to read today: a new field added to
 * SessionInfo must default to "rebuild", never to "silently ignored".
 * SessionInfo is flat and primitive-valued, so !== is a sound comparison.
 */
export function titleOnlyChange<T extends { title?: string }>(
  prev: T | undefined,
  next: T | undefined,
): boolean {
  if (!prev || !next) return false;
  if ((prev.title ?? '') === (next.title ?? '')) return false;
  // The generic keeps call sites honest about comparing like with like;
  // the walk itself is deliberately key-agnostic, hence the one cast.
  const a = prev as unknown as Record<string, unknown>;
  const b = next as unknown as Record<string, unknown>;
  const keys = new Set([...Object.keys(a), ...Object.keys(b)]);
  keys.delete('title');
  for (const k of keys) {
    if (a[k] !== b[k]) return false;
  }
  return true;
}
