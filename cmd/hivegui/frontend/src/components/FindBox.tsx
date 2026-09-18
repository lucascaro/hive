// The in-session find box (spec 431).
//
// One box, two sources. On a normal-buffer session it drives
// @xterm/addon-search and the terminal highlights in place, so the box
// is just the input and the counter. On an alt-screen session — where
// the terminal keeps no scrollback and holds one screenful — it renders
// the agent's transcript above the terminal, with the active match
// centred in its surrounding conversation.

import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import {
  closeFindBox,
  extendLines,
  runQuery,
  stepMatch,
} from '../app/find-box.js';
import {
  formatCount,
  groupMessages,
  highlightSegments,
  messageLabel,
  visibleLineCount,
  transcriptScrollTarget,
  unavailableMessage,
  type LineMatch,
} from '../lib/find.js';
import type { FindState } from '../store/store.js';
import { Icon } from './Icon.js';
import { findKey } from '../lib/keymap.js';
import { isMac } from '../lib/platform.js';

// The find chord pressed while the box already has focus selects the
// query, as in a browser or editor: press it and type to replace. On
// macOS the native menu accelerator normally takes ⌘F before the webview
// sees it (and refocuses the same way); this covers the builds and focus
// states where the keydown reaches the input instead.
function isFindChord(e: {
  key: string;
  code: string;
  metaKey: boolean;
  ctrlKey: boolean;
  altKey: boolean;
  shiftKey: boolean;
}): boolean {
  if (isMac) {
    const f = e.code === 'KeyF' || e.key === 'f' || e.key === 'F';
    return f && e.metaKey && !e.ctrlKey && !e.altKey;
  }
  return findKey(e, false);
}

// The bar is rendered FIRST in both modes and styled identically, so it
// sits in exactly the same spot whichever source is active — the
// transcript, when there is one, fills the tile below it. The user's eye
// should never have to hunt for the box because an agent is running.
// Match columns are relative to the full line; a long line arrives as a
// slice starting `offset` units in, so shift them onto the slice.
function rebase(ms: LineMatch[], offset: number): LineMatch[] {
  return offset ? ms.map((m) => ({ col: m.col - offset, len: m.len })) : ms;
}

// How close to an edge of the transcript pane the reader gets before the
// next block of history loads.
const EDGE_PX = 400;

// The first line at least partly visible at the top of the pane, and its
// distance from the pane's top edge.
function topVisibleLine(
  body: HTMLElement,
): { line: number; offset: number } | null {
  // Binary search: lines are in document order, so their offsetTop only
  // grows. Runs on every scroll event over up to MAX_LOADED_LINES lines.
  const els = body.querySelectorAll<HTMLElement>('[data-find-line]');
  let lo = 0;
  let hi = els.length - 1;
  let found = -1;
  while (lo <= hi) {
    const mid = (lo + hi) >> 1;
    const el = els[mid];
    if (el.offsetTop + el.offsetHeight > body.scrollTop) {
      found = mid;
      hi = mid - 1;
    } else {
      lo = mid + 1;
    }
  }
  if (found < 0) return null;
  const el = els[found];
  return {
    line: Number(el.dataset.findLine),
    offset: el.offsetTop - body.scrollTop,
  };
}

// Puts a remembered line back where it was on screen. False when the
// line is not rendered any more.
function restoreAnchor(
  body: HTMLElement,
  anchor: { line: number; offset: number } | null,
): boolean {
  if (!anchor) return false;
  const el = body.querySelector<HTMLElement>(
    `[data-find-line="${anchor.line}"]`,
  );
  if (!el) return false;
  body.scrollTop = el.offsetTop - anchor.offset;
  return true;
}

export function FindBox({ id, find }: { id: string; find: FindState }) {
  const inputRef = useRef<HTMLInputElement>(null);

  // Autofocus without a click (criterion 1). Keyed on nothing but
  // mount: reopening the box remounts it, because the controller sets
  // `find` to a fresh object.
  useEffect(() => {
    inputRef.current?.focus();
    inputRef.current?.select();
  }, []);

  const takeover = find.source === 'transcript';
  const unavailable = takeover && find.ready && find.reason !== '';
  const bodyRef = useRef<HTMLDivElement>(null);
  const active = find.matches[find.index];

  // How the pane looked before the latest change, maintained on scroll
  // and after every commit: whether the reader was at the bottom, and
  // which line sat at the top of the view and how far from its edge.
  const view = useRef<{
    atBottom: boolean;
    fromBottom: number;
    anchor: { line: number; offset: number } | null;
  }>({ atBottom: true, fromBottom: 0, anchor: null });
  const seenLoad = useRef(-1);

  const measure = () => {
    const body = bodyRef.current;
    if (!body) return;
    const fromBottom = body.scrollHeight - body.clientHeight - body.scrollTop;
    view.current = {
      atBottom: fromBottom <= 4,
      fromBottom,
      anchor: topVisibleLine(body),
    };
  };

  // Loads more history as the reader nears either edge — and, since it
  // also runs after every commit, keeps loading until a short pane is
  // full. The transcript used to hold one fixed window, and scrolling up
  // simply stopped: it read as the history being cut off.
  const extendNearEdges = () => {
    const body = bodyRef.current;
    if (!body || !takeover) return;
    if (body.scrollTop < EDGE_PX) extendLines(id, 'up');
    if (body.scrollHeight - body.clientHeight - body.scrollTop < EDGE_PX) {
      extendLines(id, 'down');
    }
  };

  // Positions the pane after its content changes, by cause:
  //  - older history loaded above: hold the reader's place, re-anchoring
  //    on the line that was at the top (robust to the far end being
  //    trimmed, which a plain height delta is not);
  //  - newer output loaded below: follow it if the reader was at the
  //    bottom, like a terminal, and otherwise hold still;
  //  - a new search, match or window: the bottom with nothing active,
  //    the active match centred otherwise (lib/find.ts
  //    transcriptScrollTarget).
  //
  // Keyed on the active match's LINE and column, not its index: while
  // typing, the index stays 0 but the match moves to another line.
  // Sets scrollTop directly: scrollIntoView would also scroll the tile
  // the box is drawn over. A layout effect, so the pane never paints at
  // the wrong position first.
  // biome-ignore lint/correctness/useExhaustiveDependencies: deps are re-run triggers
  useLayoutEffect(() => {
    const body = bodyRef.current;
    if (!body || !takeover) return;
    const loadChanged = find.loadSeq !== seenLoad.current;
    seenLoad.current = find.loadSeq;

    if (loadChanged && find.lastLoad !== 'replace') {
      // Pinned to the bottom stays pinned — the most recent output is
      // what the pane is for. Otherwise hold the reader's line; if a
      // tool output re-collapsed around it (its true start just loaded),
      // hold the distance from the bottom instead.
      if (view.current.atBottom) body.scrollTop = body.scrollHeight;
      else if (!restoreAnchor(body, view.current.anchor)) {
        body.scrollTop =
          body.scrollHeight - body.clientHeight - view.current.fromBottom;
      }
    } else {
      const el = active
        ? body.querySelector<HTMLElement>(`[data-find-line="${active.line}"]`)
        : null;
      const top = transcriptScrollTarget({
        hasActive: Boolean(active),
        scrollHeight: body.scrollHeight,
        clientHeight: body.clientHeight,
        activeTop: el ? el.offsetTop : undefined,
        activeHeight: el ? el.offsetHeight : undefined,
      });
      if (top !== null) body.scrollTop = top;
    }
    measure();
    extendNearEdges();
  }, [active?.line, active?.col, find.loadSeq, takeover, find.ready]);

  // New output while the plain transcript is open raises the line count;
  // a reader pinned to the bottom gets it appended.
  // biome-ignore lint/correctness/useExhaustiveDependencies: totalLines is the trigger
  useEffect(() => {
    if (view.current.atBottom) extendLines(id, 'down');
  }, [find.totalLines]);

  return (
    <search
      className={takeover ? 'hv-find hv-find-takeover' : 'hv-find'}
      data-find-source={find.source}
    >
      <div className="hv-find-bar">
        <input
          ref={inputRef}
          className="hv-find-input"
          data-find-input={id}
          type="text"
          // A search term is not prose: macOS autocorrect and
          // autocapitalize would rewrite it under the user's cursor.
          autoCorrect="off"
          autoCapitalize="off"
          autoComplete="off"
          spellCheck={false}
          placeholder="Find"
          aria-label="Find in session"
          value={find.query}
          onChange={(e) => runQuery(id, e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Escape') {
              // Consumed here: nothing else — least of all the session,
              // where Escape interrupts an agent — may see it.
              e.preventDefault();
              e.stopPropagation();
              closeFindBox(id, { deferFocus: true });
            } else if (isFindChord(e)) {
              e.preventDefault();
              e.stopPropagation();
              e.currentTarget.select();
            } else if (e.key === 'Enter') {
              // Enter confirms an IME candidate while composing.
              if (e.nativeEvent.isComposing) return;
              e.preventDefault();
              stepMatch(id, e.shiftKey ? -1 : 1);
            }
          }}
        />
        {/* Fixed-width and tabular-nums: digit jitter as the count
            changes is the most likely source of perceived flicker, and
            it is pure CSS rather than a reason to delay the number. */}
        <span className="hv-find-count" data-find-count={id}>
          {formatCount(find.index, find.total, find.capped)}
        </span>
        {/* Search runs bottom to top, so the arrows follow the screen:
            up is the next, OLDER match (same as Enter), down the newer
            one (Shift+Enter). */}
        <button
          type="button"
          className="hv-find-btn"
          aria-label="Next match (older, above)"
          data-find-next={id}
          onClick={() => stepMatch(id, 1)}
        >
          <Icon name="chevron-up" />
        </button>
        <button
          type="button"
          className="hv-find-btn"
          aria-label="Previous match (newer, below)"
          data-find-prev={id}
          onClick={() => stepMatch(id, -1)}
        >
          <Icon name="chevron-down" />
        </button>
        <button
          type="button"
          className="hv-find-btn"
          aria-label="Close find"
          data-find-close={id}
          onClick={() => closeFindBox(id)}
        >
          <Icon name="x" />
        </button>
      </div>

      {takeover ? (
        <div
          className="hv-find-body"
          data-find-body={id}
          ref={bodyRef}
          onScroll={() => {
            measure();
            extendNearEdges();
          }}
        >
          {!find.ready ? (
            <p className="hv-find-note">Reading transcript…</p>
          ) : unavailable ? (
            <p className="hv-find-note" data-find-unavailable="1">
              {unavailableMessage(find.reason)}
            </p>
          ) : (
            <TranscriptLines find={find} />
          )}
        </div>
      ) : null}
    </search>
  );
}

function TranscriptLines({ find }: { find: FindState }) {
  const active = find.matches[find.index];
  // Tool outputs the user opened, by message id.
  const [expanded, setExpanded] = useState<ReadonlySet<number>>(new Set());

  if (find.lines.length === 0) {
    return <p className="hv-find-note">No transcript lines to show.</p>;
  }

  // Matches indexed by line once, rather than filtering the whole match
  // list for every rendered line.
  const byLine = new Map<number, LineMatch[]>();
  for (const m of find.matches) {
    const list = byLine.get(m.line);
    if (list) list.push({ col: m.col, len: m.len });
    else byLine.set(m.line, [{ col: m.col, len: m.len }]);
  }

  // Rendered as messages, the way an agent session reads: a prompt, the
  // agent's reply, each tool's output under the tool's name — one header
  // per message and space between messages, instead of a role label
  // repeated on every line.
  return (
    <div className="hv-find-lines">
      {groupMessages(find.lines).map((g) => {
        const label = messageLabel(g);
        return (
          <div
            key={g.key}
            className={`hv-tx-msg hv-tx-${g.kind}`}
            data-tx-kind={g.kind}
          >
            {label ? <div className="hv-tx-label">{label}</div> : null}
            <div className="hv-tx-body">
              {(() => {
                const { shown, hidden } = visibleLineCount({
                  kind: g.kind,
                  lines: g.lines,
                  hasMatch: g.lines.some((ln) => byLine.has(ln.line)),
                  expanded: expanded.has(g.key),
                });
                return (
                  <>
                    {g.lines.slice(0, shown).map((ln) => {
                      const isActive =
                        active !== undefined && active.line === ln.line;
                      return (
                        <div
                          key={ln.line}
                          className={
                            isActive
                              ? 'hv-find-line hv-find-line-active'
                              : 'hv-find-line'
                          }
                          data-find-line={ln.line}
                        >
                          {ln.offset ? (
                            <span className="hv-find-cut">… </span>
                          ) : null}
                          {highlightSegments(
                            ln.text,
                            rebase(byLine.get(ln.line) ?? [], ln.offset ?? 0),
                          ).map((seg) => (
                            <span
                              key={seg.start}
                              className={
                                !seg.hit
                                  ? undefined
                                  : isActive && seg.start === active.col
                                    ? 'hv-find-hit hv-find-hit-active'
                                    : 'hv-find-hit'
                              }
                            >
                              {seg.text}
                            </span>
                          ))}
                          {ln.truncated ? (
                            <span className="hv-find-cut"> …</span>
                          ) : null}
                        </div>
                      );
                    })}
                    {hidden > 0 ? (
                      <button
                        type="button"
                        className="hv-tx-more"
                        data-tx-more={g.key}
                        onClick={() =>
                          setExpanded((s) => new Set(s).add(g.key))
                        }
                      >
                        Show {hidden} more {hidden === 1 ? 'line' : 'lines'}
                      </button>
                    ) : null}
                  </>
                );
              })()}
            </div>
          </div>
        );
      })}
    </div>
  );
}
