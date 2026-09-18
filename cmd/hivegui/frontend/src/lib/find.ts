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
 *
 * Matches may arrive in any order — the daemon lists a line's matches
 * newest (rightmost) first — so they are walked by column.
 */
export function highlightSegments(
  text: string,
  matches: LineMatch[],
): Segment[] {
  const out: Segment[] = [];
  let cursor = 0;
  let n = 0;
  for (const m of [...matches].sort((a, b) => a.col - b.col)) {
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

/**
 * Converts @xterm/addon-search's top-down result index into the
 * newest-first index the find box shows (spec 431: search runs bottom to
 * top, so `1/N` is the match nearest the bottom).
 *
 * The addon reports resultIndex -1 when the active match lies beyond its
 * highlight limit; that clamps to 0 rather than rendering "0/N".
 */
export function newestFirstIndex(resultIndex: number, total: number): number {
  if (total <= 0 || resultIndex < 0) return 0;
  return Math.max(0, Math.min(total - 1, total - 1 - resultIndex));
}

/**
 * Finds where the match the user was on sits in a refreshed, newest-first
 * list, by line and column.
 *
 * New output prepends newer matches, so the same match moves to a higher
 * index; keeping the index fixed would silently jump the user to a
 * different match. Falls back to 0 (the newest) when the match is gone.
 */
export function reanchorIndex(
  prev: { line: number; col: number } | undefined,
  next: { line: number; col: number }[],
): number {
  if (!prev) return 0;
  const i = next.findIndex((m) => m.line === prev.line && m.col === prev.col);
  return i < 0 ? 0 : i;
}

/**
 * Where the transcript pane should scroll to, or null to leave it alone.
 *
 * - No active match (the box just opened, or the query finds nothing):
 *   the bottom. The transcript opens on the most recent output, the same
 *   direction the search runs in.
 * - An active match that is rendered: centred, so it is readable in its
 *   surrounding conversation.
 * - An active match whose line is not rendered yet: null. While typing,
 *   the new match lands before the window of lines around it does;
 *   scrolling to the bottom in that gap would make the pane jump twice.
 */
export function transcriptScrollTarget(p: {
  hasActive: boolean;
  scrollHeight: number;
  clientHeight: number;
  /** The active line's offset within the pane, when it is rendered. */
  activeTop?: number;
  activeHeight?: number;
}): number | null {
  const max = Math.max(0, p.scrollHeight - p.clientHeight);
  if (!p.hasActive) return max;
  if (p.activeTop === undefined) return null;
  const centred = p.activeTop - (p.clientHeight - (p.activeHeight ?? 0)) / 2;
  return Math.max(0, Math.min(max, Math.round(centred)));
}

/** A run of consecutive lines that belong to one message. */
export interface MessageGroup<L> {
  /** Stable key: the message id, or the first line's index as fallback. */
  key: number;
  kind: string;
  tool?: string;
  lines: L[];
}

/**
 * Groups transcript lines into messages for rendering (spec 431: read
 * like an agent session — one header per message, not one per line).
 *
 * Only CONSECUTIVE lines with the same message id group: a window of
 * lines is a contiguous slice, so a message is never split and rejoined.
 * A line without a message id (an older daemon) stands alone, grouped by
 * its own line number, which degrades to the previous per-line layout.
 */
export function groupMessages<
  L extends { line: number; msg?: number; kind?: string; tool?: string },
>(lines: L[]): MessageGroup<L>[] {
  const out: MessageGroup<L>[] = [];
  for (const ln of lines) {
    const id = ln.msg ?? -1 - ln.line;
    const last = out[out.length - 1];
    if (last && last.key === id) {
      last.lines.push(ln);
      continue;
    }
    out.push({
      key: id,
      kind: ln.kind ?? 'assistant',
      tool: ln.tool,
      lines: [ln],
    });
  }
  return out;
}

/** The header a message shows, or '' for none. */
export function messageLabel(g: { kind: string; tool?: string }): string {
  switch (g.kind) {
    case 'user':
      return 'You';
    case 'tool':
      return g.tool || 'Tool output';
    default:
      return '';
  }
}

/** Tool output longer than this collapses. */
export const TOOL_COLLAPSE_OVER = 12;
/** How many lines a collapsed tool output shows. */
export const TOOL_COLLAPSED_LINES = 8;

/**
 * How many of a message's lines to render, and whether it is collapsed.
 *
 * Long tool output collapses to its first lines, the way an agent
 * session shows a tool's result — otherwise one 300-line command buries
 * the conversation around it. A collapsed message is never allowed to
 * hide a search result: it renders in full when any of its lines holds
 * a match, or when the user expanded it.
 */
export function visibleLineCount(g: {
  kind: string;
  lines: { line: number }[];
  hasMatch: boolean;
  expanded: boolean;
}): { shown: number; hidden: number } {
  const n = g.lines.length;
  if (
    g.kind !== 'tool' ||
    n <= TOOL_COLLAPSE_OVER ||
    g.hasMatch ||
    g.expanded
  ) {
    return { shown: n, hidden: 0 };
  }
  return { shown: TOOL_COLLAPSED_LINES, hidden: n - TOOL_COLLAPSED_LINES };
}
