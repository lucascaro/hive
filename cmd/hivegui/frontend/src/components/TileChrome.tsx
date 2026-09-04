// The terminal tile's chrome, rendered into a host React must never own.
//
// The boundary, and why it is drawn here rather than anywhere else:
// app/session-term.ts keeps creating and destroying `.term-host` and
// `.term-body`, because a SessionTerm holds an xterm instance, one of
// eight process-wide WebGL slots (lib/webgl-budget.ts) and a live PTY
// attachment. Hosts are reparented, never recreated (app/grid-layout.ts),
// and unmount/remount of a mounted terminal is the bug the whole React
// migration exists to avoid. So React reaches the tile through a portal
// and never through children.
//
// Two mount points, deliberately different:
//
// - `.tile-header` is created EMPTY by SessionTerm and only its children
//   are portalled. tile-header.css pins it to `height: 28px;
//   flex-shrink: 0`, so an empty header on the frame before React fills
//   it leaves `.term-body`'s box byte-identical — which is what keeps
//   first-attach fit() measuring the right rows. Creating the header in
//   React would shrink the body for one frame and fire a spurious PTY
//   resize mid-attach.
// - The overlays mount into `.tile-overlays`, a `display: contents`
//   wrapper after the body, and React creates them outright. They are
//   absolutely positioned, so they contribute no layout and cost nothing
//   either way. See TileOverlays.tsx.
//
// Membership comes from store/terms.ts's useTermIds — which ids have a
// live host, never the SessionTerm values. Observable, not reactive.
import { createPortal } from 'react-dom';
import { useRef, type ReactNode } from 'react';

import { UpdateSession } from '../bridge.js';
import { reportFailure } from '../app/dom.js';
import { beginInlineRename } from '../app/inline-rename.js';
import { setFocusedTile, refocusActiveTerm } from '../app/focus.js';
import { minimizeSession } from '../app/view.js';
import { openWorktrees } from '../app/modals/worktrees.js';
import { sessionState } from '../lib/session-state.js';
import { displayTitle } from '../lib/term-title.js';
import { appStore, useAppStore, type TileChromeState } from '../store/store.js';
import { getTerm, useTermIds } from '../store/terms.js';
import { StateIcon } from './Icon.js';
import { IconButton } from './IconButton.js';
import { TileOverlays } from './TileOverlays.js';

const appData = () => appStore.getState();

export function TileChromeHost(): ReactNode {
  const ids = useTermIds();
  return (
    <>
      {ids.map((id) => (
        <TileChrome key={id} id={id} />
      ))}
    </>
  );
}

function TileChrome({ id }: { id: string }): ReactNode {
  // Two narrow subscriptions rather than one wide one. `attention` is a
  // Set, so `.has(id)` collapses it to a boolean before zustand's
  // Object.is comparison — a bell on ANOTHER session re-renders nothing.
  //
  // GridView.tsx excludes `attention` from its subscription because a
  // pass there calls ensureAttached() on every in-grid tile, which
  // re-latches follow-bottom. That hazard does not apply here: this
  // component renders no layout and calls no attach path, so a bell
  // repaints one header and nothing else.
  const chrome = useAppStore((s) => s.tileChrome.get(id));
  const attention = useAppStore((s) => s.attention.has(id));
  const term = getTerm(id);
  // `header` and `overlays` are optional on TermTile because the
  // dom-test stubs render no chrome at all; a real tile always has both.
  if (!chrome || !term?.header) return null;
  return (
    <>
      {createPortal(
        <TileHeader id={id} chrome={chrome} attention={attention} />,
        term.header,
      )}
      {term.overlays
        ? createPortal(
            <TileOverlays
              chrome={chrome}
              onClose={() => term._closeDead()}
              onDismiss={() => term._dismissDead()}
            />,
            term.overlays,
          )
        : null}
    </>
  );
}

// The header's children, in the order the e2e specs select them:
// state icon, name, worktree marker, terminal title, project, actions.
// The worktree marker sits OUTSIDE .tile-actions on purpose —
// worktree-ness is a fact about the session, not an action, so it has to
// read at rest the way the sidebar row's marker does. Only minimize is
// hover-revealed.
function TileHeader({
  id,
  chrome,
  attention,
}: {
  id: string;
  chrome: TileChromeState;
  attention: boolean;
}): ReactNode {
  const nameRef = useRef<HTMLSpanElement>(null);
  // The session list, not a snapshot of it. The header used to render
  // from SessionTerm's own copy, refreshed only when the layout ran
  // ensureTerm() — so a session event that repainted the sidebar left
  // the tile showing the previous value, and the two surfaces disagreed
  // about the same session. One source, narrowly subscribed: the
  // selector returns this session's object, so a change to any other
  // re-renders nothing.
  const info = useAppStore((s) => s.sessions.find((x) => x.id === id));
  // `chrome.phase` overrides `info.phase`: setPhase() updates the tile's
  // own phase and never writes back to the session list, so resolving
  // from the list alone would repaint the icon from whatever the last
  // list said — stale for exactly the transition setPhase exists for.
  const state = sessionState({ ...info, phase: chrome.phase }, attention);
  const branch = info?.worktreeBranch ?? info?.worktree_branch;
  const title = displayTitle(info?.title, info?.name);
  const projectId = info?.projectId ?? info?.project_id ?? '';
  const project = useAppStore((s) =>
    s.projects.find((p) => p.id === projectId),
  );
  // A tile briefly outlives its session row on teardown: `removed`
  // drops the session before dropTileChrome runs. Render nothing rather
  // than a header full of blanks. After every hook, so the hook order
  // is the same on every render.
  if (!info) return null;
  return (
    <>
      <StateIcon state={state} />
      {/* Double-click the tile name to rename inline, the same affordance
          the sidebar row has (SessionRow.tsx carries the identical
          ignore, for the identical reason). The listener is carried over
          verbatim from the imperative header, so this is not a new
          accessibility gap: dblclick-to-rename is a shortcut for people
          already using a pointer, the surrounding host's mousedown does
          the selecting, and every control in the header is a real
          <button>. */}
      {/* biome-ignore lint/a11y/noStaticElementInteractions: see above */}
      <span
        ref={nameRef}
        className="tile-name"
        onDoubleClick={(e) => {
          e.stopPropagation();
          const el = nameRef.current;
          if (!el) return;
          beginInlineRename({
            className: 'tile-name-input',
            value: info.name ?? '',
            mount: (input) => el.replaceWith(input),
            unmount: (input) => input.replaceWith(el),
            // Drop the visual focus border before stealing keyboard
            // focus — setFocusedTile is the only writer of
            // .term-focused, so without this the border would linger
            // while the rename input owns input.
            beforeFocus: () => setFocusedTile(null),
            onCommit: (next) =>
              UpdateSession(id, next, '', -1).catch(reportFailure('rename')),
            onDone: () => refocusActiveTerm(),
          });
        }}
      >
        {info.name ?? ''}
      </span>
      <IconButton
        icon="branch"
        label="Manage worktrees"
        className="tile-worktree"
        hidden={!branch}
        title={
          branch ? `Worktree: ${branch} — click to manage worktrees` : undefined
        }
        onClick={(e) => {
          // The tile header also focuses/activates the tile; this click
          // is about the worktree, not about switching sessions.
          e.stopPropagation();
          const proj = appData().projects.find((p) => p.id === projectId);
          if (proj) openWorktrees(proj);
        }}
      />
      {/* Hidden until a title actually arrives: the separator lives in
          the element's ::before, so an empty-but-visible span renders a
          lone '·' next to the session name. */}
      <span
        className="tile-term-title"
        hidden={!title}
        title={title || undefined}
      >
        {title}
      </span>
      <span className="tile-project">{project?.name ?? ''}</span>
      {/* Deliberately NOT hover-revealed as a group (see
          tile-header.css): a control that is display:none until hover
          cannot be reached by keyboard. */}
      <div className="tile-actions">
        <IconButton
          icon="minus"
          label="Minimize session"
          className="tile-minimize"
          onClick={(e) => {
            e.stopPropagation();
            minimizeSession(id);
          }}
          // Block the surrounding tile mousedown so minimizing doesn't
          // also select / switch to this tile.
          onMouseDown={(e) => e.stopPropagation()}
        />
      </div>
    </>
  );
}
