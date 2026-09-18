// The in-session find box (spec 431).
//
// One box, two sources. On a normal-buffer session it drives
// @xterm/addon-search and the terminal highlights in place, so the box
// is just the input and the counter. On an alt-screen session — where
// the terminal keeps no scrollback and holds one screenful — it renders
// the agent's transcript above the terminal, with the active match
// centred in its surrounding conversation.

import { useEffect, useLayoutEffect, useRef } from 'react';
import { closeFindBox, runQuery, stepMatch } from '../app/find-box.js';
import {
  formatCount,
  highlightSegments,
  transcriptScrollTarget,
  unavailableMessage,
  type LineMatch,
} from '../lib/find.js';
import type { FindState } from '../store/store.js';
import { Icon } from './Icon.js';

// The bar is rendered FIRST in both modes and styled identically, so it
// sits in exactly the same spot whichever source is active — the
// transcript, when there is one, fills the tile below it. The user's eye
// should never have to hunt for the box because an agent is running.
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

  // Keep the transcript pane on what matters: the bottom (most recent)
  // when nothing is selected, the active match centred when something is
  // (lib/find.ts transcriptScrollTarget has the rules).
  //
  // Keyed on the active match's LINE and column, not its index: while
  // typing the index stays 0 but the match moves to a different line,
  // and an index-keyed effect never re-ran. Also keyed on `lines`, since
  // the window around a new match arrives after the match does.
  //
  // Sets scrollTop on the pane directly rather than calling
  // scrollIntoView, which also scrolls every scrollable ancestor — here,
  // the tile the box is drawn over.
  //
  // Layout effect so the pane never paints at the wrong position first.
  // biome-ignore lint/correctness/useExhaustiveDependencies: deps are re-run triggers
  useLayoutEffect(() => {
    const body = bodyRef.current;
    if (!body || !takeover) return;
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
  }, [active?.line, active?.col, find.lines, takeover, find.ready]);

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
              e.preventDefault();
              closeFindBox(id);
            } else if (e.key === 'Enter') {
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
        <div className="hv-find-body" data-find-body={id} ref={bodyRef}>
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

  if (find.lines.length === 0) {
    return <p className="hv-find-note">No transcript lines to show.</p>;
  }

  return (
    <div className="hv-find-lines">
      {find.lines.map((ln) => {
        const isActive = active !== undefined && active.line === ln.line;
        const onThisLine: LineMatch[] = find.matches
          .filter((m) => m.line === ln.line)
          .map((m) => ({ col: m.col, len: m.len }));
        return (
          <div
            key={ln.line}
            className={
              isActive ? 'hv-find-line hv-find-line-active' : 'hv-find-line'
            }
            data-find-line={ln.line}
          >
            <span className="hv-find-role">{ln.role ?? ''}</span>
            <span className="hv-find-text">
              {highlightSegments(ln.text, onThisLine).map((seg) => (
                <span
                  key={seg.start}
                  className={seg.hit ? 'hv-find-hit' : undefined}
                >
                  {seg.text}
                </span>
              ))}
              {ln.truncated ? <span className="hv-find-cut"> …</span> : null}
            </span>
          </div>
        );
      })}
    </div>
  );
}
