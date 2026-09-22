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
import { findPathCandidates } from '../lib/file-links.js';
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
  /** Buffer row of the first row of the logical line, 0-based. */
  firstRow: number;
  /** Cells per row; the width used to translate offsets. */
  width: number;
}

function readLogicalLine(
  term: Terminal,
  bufferLineNumber: number,
): LogicalLine | null {
  const buf = term.buffer.active;
  // xterm hands provideLinks a 1-based line number.
  let first = bufferLineNumber - 1;
  const lineAt = (y: number): IBufferLine | undefined => buf.getLine(y);
  if (!lineAt(first)) return null;
  while (first > 0 && lineAt(first)?.isWrapped) first--;
  let last = bufferLineNumber - 1;
  while (lineAt(last + 1)?.isWrapped) last++;

  let text = '';
  for (let y = first; y <= last; y++) {
    text += lineAt(y)?.translateToString(false) ?? '';
  }
  return { text, firstRow: first, width: term.cols };
}

function rangeFor(logical: LogicalLine, start: number, end: number) {
  const { firstRow, width } = logical;
  return {
    start: {
      x: (start % width) + 1,
      y: firstRow + Math.floor(start / width) + 1,
    },
    // end is inclusive in xterm's model, hence end - 1.
    end: {
      x: ((end - 1) % width) + 1,
      y: firstRow + Math.floor((end - 1) / width) + 1,
    },
  };
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
      ResolveFilePaths(
        baseDir,
        candidates.map((c) => c.path),
      )
        .then((resolved) => {
          const links: HiveFileLink[] = [];
          candidates.forEach((c, i) => {
            if (!resolved[i]) return;
            links.push({
              hiveFile: true,
              text: c.path,
              range: rangeFor(logical, c.start, c.end),
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
