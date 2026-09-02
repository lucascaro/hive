// Shared presentation rule for the OSC-set terminal window title.
//
// Two surfaces render this string — the grid tile header
// (app/session-term.ts) and the sidebar row
// (components/SessionRow.tsx) — and they must agree on when there is
// nothing worth showing, or the same session reads as "busy" in one
// place and idle in the other.

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
