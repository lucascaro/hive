// Pure helpers for the in-session find box (spec 431).
//
// ⌘F drives two sources behind one UI. Which one is chosen is decided
// here, from the terminal's buffer type — not from the agent type.
// Alt-screen is precisely *why* a buffer search fails (the alternate
// screen keeps no scrollback, so the terminal holds one screenful), so
// it is the condition that should select the source.

/** Where a find box's matches come from. */
export type FindSource = 'buffer' | 'transcript';

/**
 * Picks the source for a terminal currently on `bufferType`.
 *
 * xterm reports 'normal' or 'alternate'. Anything else is treated as
 * normal: an unknown buffer type means we cannot justify taking the
 * terminal away from the user, and buffer search degrades to "finds
 * less" rather than to "finds the wrong thing".
 */
export function findSourceFor(bufferType: string | undefined): FindSource {
  return bufferType === 'alternate' ? 'transcript' : 'buffer';
}

/**
 * True when the xterm search addon may be asked to search.
 *
 * This is an invariant, not an optimization. Any search performed while
 * the alternate buffer is active permanently poisons `@xterm/addon-search`
 * for the normal buffer — clearDecorations() does not clear it and an
 * empty query does not clear it; only a fresh addon instance recovers.
 * Untreated that reads as "search a Claude session, Claude exits, search
 * again, silently find nothing, forever".
 *
 * Routing alt-screen sessions to the transcript makes that path
 * unreachable rather than merely mitigated.
 */
export function mayUseSearchAddon(bufferType: string | undefined): boolean {
  return findSourceFor(bufferType) === 'buffer';
}

/** One piece of a line, marked as matched or not, for rendering. */
export interface Segment {
  text: string;
  hit: boolean;
  /** Index of this match among the line's matches; -1 when not a hit. */
  at: number;
  /**
   * Offset of this segment in the line. A segment's position in the
   * text IS its identity, so this is its stable render key — no index
   * key needed.
   */
  start: number;
}

/** A match anchored inside one line. */
export interface LineMatch {
  col: number;
  len: number;
}

/**
 * Splits `text` into alternating plain/matched segments.
 *
 * Columns are byte-free: they index the string the caller renders,
 * which is what the daemon guarantees by stripping control characters
 * before it counts. Out-of-range or overlapping matches are skipped
 * rather than throwing — a stale match list paired with a fresh line
 * must degrade to "highlights nothing" and never to a crash.
 */
export function highlightSegments(
  text: string,
  matches: LineMatch[],
): Segment[] {
  const out: Segment[] = [];
  let cursor = 0;
  let n = 0;
  for (const m of matches) {
    if (m.len <= 0) continue;
    if (m.col < cursor || m.col >= text.length) continue;
    const end = Math.min(m.col + m.len, text.length);
    if (end <= m.col) continue;
    if (m.col > cursor)
      out.push({
        text: text.slice(cursor, m.col),
        hit: false,
        at: -1,
        start: cursor,
      });
    out.push({ text: text.slice(m.col, end), hit: true, at: n, start: m.col });
    cursor = end;
    n += 1;
  }
  if (cursor < text.length)
    out.push({ text: text.slice(cursor), hit: false, at: -1, start: cursor });
  return out;
}

/**
 * Renders the match counter.
 *
 * `capped` is for the xterm addon, which hard-caps its reported result
 * count at 1000 — a bare "1000" would be a confidently wrong number, so
 * it renders "1000+".
 *
 * The caller pairs this with a fixed-width, tabular-nums field: digit
 * jitter as the count changes is the most likely source of perceived
 * flicker, and it is pure CSS rather than a reason to delay the number.
 */
export function formatCount(
  index: number,
  total: number,
  capped = false,
): string {
  if (total <= 0) return '0/0';
  const t = capped ? `${total}+` : `${total}`;
  return `${Math.min(index + 1, total)}/${t}`;
}

/** Steps the active match index, wrapping at both ends. */
export function stepIndex(index: number, total: number, delta: number): number {
  if (total <= 0) return 0;
  return (((index + delta) % total) + total) % total;
}

/**
 * Human text for a session whose history cannot be searched.
 *
 * The reasons are distinct on purpose. "This agent keeps no transcript"
 * is a permanent fact; "it should have one and none was found" is a
 * transient state, and telling a real Claude session it has no history
 * would read as a bug in the feature rather than in the session.
 */
export function unavailableMessage(reason: string): string {
  switch (reason) {
    case 'unsupported_agent':
      return 'No searchable history for this session type.';
    case 'no_transcript_file':
      return 'No transcript on disk yet for this session.';
    case 'no_such_session':
      return 'That session is no longer open.';
    default:
      return 'No searchable history.';
  }
}
