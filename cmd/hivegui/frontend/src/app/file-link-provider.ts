// The xterm link provider behind ⌘-click on a file path.
//
// xterm asks for links on the hovered line only, so the cost is one
// bridge round-trip per line the pointer crosses, and the underline
// appears only for paths Go confirms exist.
//
// Two rules live here rather than in Go, because they are about the
// *gesture*, not the file:
//   - a file link needs the platform modifier (⌘ / Ctrl). A plain
//     click keeps selecting text and positioning the cursor, which is
//     what a click in a terminal has always done.
//   - Shift picks the editor over the OS default handler.

import type { IBufferLine, ILink, ILinkProvider, Terminal } from '@xterm/xterm';
import { OpenFile, ResolveFilePaths } from '../bridge.js';
import { findPathCandidates, isFileUri } from '../lib/file-links.js';
import { cmdOrCtrl } from '../lib/platform.js';
import { reportFailure } from './dom.js';

// A link this provider created. session-term.ts's mouse-protocol
// workaround checks for this flag before swallowing a click: URL links
// keep activating on a plain click, file links do not.
export interface HiveFileLink extends ILink {
  hiveFile: true;
}

export function isHiveFileLink(link: unknown): link is HiveFileLink {
  return !!link && (link as HiveFileLink).hiveFile === true;
}

/**
 * True when the link under the cursor activates through openFileLink,
 * and so does nothing without the platform modifier.
 *
 * Two kinds qualify: the links this provider creates, and the OSC 8
 * `file://` links xterm's own OscLinkProvider emits (which
 * linkHandler.activate routes to openFileLink too). session-term.ts's
 * mouse-protocol workaround must not swallow a plain click on either,
 * or the click is lost to selection and click-to-position as well.
 */
export function isFileLinkTarget(
  link: { text?: string; hiveFile?: boolean } | null | undefined,
): boolean {
  return isHiveFileLink(link) || isFileUri(link?.text ?? '');
}

/** True when this mouse event should activate a file link at all. */
export function fileLinkModifier(e: MouseEvent): boolean {
  return cmdOrCtrl(e);
}

/** True when the click asks for the configured editor (⇧⌘-click). */
export function fileLinkEditorModifier(e: MouseEvent): boolean {
  return cmdOrCtrl(e) && e.shiftKey;
}

/**
 * openFileLink is the single activation path for both provider links
 * and OSC 8 `file://` links. Returns false when the gesture was not a
 * file-link gesture, so the caller can leave the event alone.
 */
export function openFileLink(
  e: MouseEvent,
  baseDir: string,
  path: string,
  line?: number,
  col?: number,
): boolean {
  if (!fileLinkModifier(e)) return false;
  OpenFile(baseDir, path, line ?? 0, col ?? 0, fileLinkEditorModifier(e)).catch(
    reportFailure('open file'),
  );
  return true;
}

// A logical line and the buffer rows it spans. xterm wraps a long line
// across rows, and a path is exactly the kind of long token that gets
// split, so provideLinks has to reassemble the logical line and map
// string offsets back to (x, y) cells.
interface LogicalLine {
  text: string;
  /**
   * Cell position of each character of `text`, same length as `text`.
   *
   * A cell is not a character: a CJK glyph or an emoji occupies two
   * columns, a combining mark adds a character to the same cell, and a
   * wrapped row restarts at column 1. Deriving (x, y) from the string
   * offset arithmetically — offset % cols — is therefore right only for
   * pure-ASCII lines, and lands the underline on the wrong cells for
   * everything else.
   */
  cells: { x: number; y: number }[];
}

// Bounds on one reassembly. A logical line is walked cell by cell, so
// an agent printing a megabyte without a newline would otherwise be
// walked in full on every hovered row. Past these the line is not
// plausibly a path any more; give up rather than burn the frame.
const MAX_WRAP_ROWS = 64;
const MAX_LOGICAL_CHARS = 8192;

function readLogicalLine(
  term: Terminal,
  bufferLineNumber: number,
): LogicalLine | null {
  const buf = term.buffer.active;
  // xterm hands provideLinks a 1-based line number.
  const lineAt = (y: number): IBufferLine | undefined => buf.getLine(y);
  const hovered = bufferLineNumber - 1;
  if (!lineAt(hovered)) return null;

  let first = hovered;
  let walked = 0;
  while (first > 0 && lineAt(first)?.isWrapped && walked++ < MAX_WRAP_ROWS) {
    first--;
  }
  let last = hovered;
  walked = 0;
  while (lineAt(last + 1)?.isWrapped && walked++ < MAX_WRAP_ROWS) last++;

  let text = '';
  const cells: { x: number; y: number }[] = [];
  const cell = buf.getNullCell();
  for (let y = first; y <= last; y++) {
    const line = lineAt(y);
    if (!line) break;
    for (let x = 0; x < line.length; x++) {
      if (!line.getCell(x, cell)) continue;
      const width = cell.getWidth();
      // Width 0 is the trailing half of a wide glyph: it holds no
      // characters of its own and must not shift the mapping.
      if (width === 0) continue;
      const chars = cell.getChars() || ' ';
      // One entry per UTF-16 code unit, not per code point: the offsets
      // findPathCandidates returns come from String.matchAll and index
      // `text` in UTF-16 units. An astral-plane glyph (😀) is one code
      // point but two units, so iterating code points here would leave
      // cells shorter than text and shift every later link's range.
      for (let k = 0; k < chars.length; k++) {
        text += chars[k];
        // xterm's buffer ranges are 1-based in both axes.
        cells.push({ x: x + 1, y: y + 1 });
      }
    }
    if (text.length > MAX_LOGICAL_CHARS) return null;
  }
  return { text, cells };
}

function rangeFor(logical: LogicalLine, start: number, end: number) {
  const { cells } = logical;
  const first = cells[start];
  // end is exclusive in the candidate, inclusive in xterm's range.
  const lastCell = cells[Math.min(end, cells.length) - 1];
  if (!first || !lastCell) return null;
  return { start: first, end: lastCell };
}

/**
 * createFileLinkProvider builds the provider for one session.
 * getBaseDir is read per call, not captured, so a session that learns
 * its worktree path later still resolves relative paths correctly.
 */
export function createFileLinkProvider(
  term: Terminal,
  getBaseDir: () => string,
): ILinkProvider {
  // Moving the pointer along one row asks for that row's links over and
  // over, and each miss costs an IPC round-trip plus one stat per
  // candidate. Memoise the *answer* per (baseDir, candidate list) so a
  // hover that crosses the same line repeatedly is free.
  //
  // Bounded and short-lived on purpose: a file created or deleted after
  // the answer was cached should start (or stop) underlining without a
  // restart, so entries expire quickly and the map stays small.
  // The in-flight promise is cached, not just the settled answer: the
  // pointer crossing a row asks again within milliseconds, well before
  // the first bridge call returns, and caching only results would fire
  // a duplicate round-trip every time.
  const memo = new Map<string, { at: number; resolved: Promise<string[]> }>();
  const MEMO_TTL_MS = 3000;
  const MEMO_MAX = 64;
  const resolveCached = (
    baseDir: string,
    paths: string[],
  ): Promise<string[]> => {
    const key = `${baseDir}\u0000${paths.join('\u0000')}`;
    const hit = memo.get(key);
    const now = Date.now();
    if (hit && now - hit.at < MEMO_TTL_MS) return hit.resolved;
    const resolved = ResolveFilePaths(baseDir, paths);
    // A rejected lookup must not be cached, or one transient bridge
    // failure suppresses every link on that line for the whole TTL.
    resolved.catch(() => memo.delete(key));
    if (memo.size >= MEMO_MAX) {
      // Oldest insertion first: Map preserves insertion order.
      const oldest = memo.keys().next().value;
      if (oldest !== undefined) memo.delete(oldest);
    }
    memo.set(key, { at: now, resolved });
    return resolved;
  };

  return {
    provideLinks(bufferLineNumber, callback) {
      const baseDir = getBaseDir();
      const logical = readLogicalLine(term, bufferLineNumber);
      if (!logical || !logical.text.trim()) {
        callback(undefined);
        return;
      }
      const candidates = findPathCandidates(logical.text);
      if (candidates.length === 0) {
        callback(undefined);
        return;
      }
      // One batched existence check per hovered line. Anything Go
      // cannot resolve comes back empty and gets no underline —
      // that check is the whole underline rule (spec criterion 1).
      resolveCached(
        baseDir,
        candidates.map((c) => c.path),
      )
        .then((resolved) => {
          const links: HiveFileLink[] = [];
          candidates.forEach((c, i) => {
            if (!resolved[i]) return;
            const range = rangeFor(logical, c.start, c.end);
            if (!range) return;
            links.push({
              hiveFile: true,
              text: c.path,
              range,
              // Underline on hover, but keep the I-beam: the hand
              // cursor promises a plain click does something, and for
              // a file link a plain click deliberately does nothing
              // (it stays selection and click-to-position). xterm
              // defaults both decorations to true when the field is
              // absent, so this has to be stated.
              decorations: { underline: true, pointerCursor: false },
              activate: (e) => {
                openFileLink(e, baseDir, c.path, c.line, c.col);
              },
            });
          });
          callback(links.length ? links : undefined);
        })
        .catch(() => callback(undefined));
    },
  };
}
