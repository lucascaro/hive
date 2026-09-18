// The in-session find box (spec 431).
//
// One box, two sources. On a normal-buffer session it drives
// @xterm/addon-search and the terminal highlights in place, so the box
// is just the input and the counter. On an alt-screen session — where
// the terminal keeps no scrollback and holds one screenful — it renders
// the agent's transcript above the terminal, with the active match
// centred in its surrounding conversation.

import { useEffect, useRef } from 'react';
import { closeFindBox, runQuery, stepMatch } from '../app/find-box.js';
import {
  formatCount,
  highlightSegments,
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
        <div className="hv-find-body" data-find-body={id}>
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
  const activeRef = useRef<HTMLDivElement>(null);
  const active = find.matches[find.index];

  // Centre the active match in the scroller. The daemon already centres
  // the fetched window on it; this handles the scroll within that
  // window so the line is readable in context rather than at an edge.
  // The deps are the triggers, not values the body reads: the effect must
  // re-run when the active match moves or a new window arrives.
  // biome-ignore lint/correctness/useExhaustiveDependencies: deps are re-run triggers
  useEffect(() => {
    activeRef.current?.scrollIntoView({ block: 'center' });
  }, [find.index, find.lineStart]);

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
            ref={isActive ? activeRef : undefined}
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
