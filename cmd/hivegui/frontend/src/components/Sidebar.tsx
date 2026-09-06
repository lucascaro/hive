// The sidebar — the React replacement for src/app/sidebar.ts.
//
// One root on #projects renders the project cards and their session
// rows; the minimized-project tray (#minimized-projects) is a sibling
// element, so it is reached from this same tree through a portal — one
// subscription, one render pass, and the tray's `.hidden` class (which
// lives on the portal container, outside React's tree) is applied by an
// effect.
//
// What the imperative module needed three code paths for — renderSidebar,
// updateSidebarSelection, updateSidebarTitles — is one render here.
// Stable `key`s are what fixes the bug those paths existed to work
// around: a full rebuild between two clicks replaced the <li> and ate the
// dblclick pair that starts a rename.
//
// View/focus callbacks arrive as props from main.tsx, the composition
// root, for the same reason initSidebar(deps) took them: importing
// view.ts / keyboard.ts here would close an import cycle.
import {
  memo,
  useLayoutEffect,
  useRef,
  useState,
  type DragEvent as ReactDragEvent,
  type MouseEvent,
  type ReactNode,
} from 'react';
import { createPortal } from 'react-dom';
import {
  Confirm,
  KillSession,
  RestartSession,
  UpdateProject,
  UpdateSession,
} from '../bridge.js';
import { manualUpdateCheck } from '../app/banners.js';
import { reportFailure } from '../app/dom.js';
import { beginInlineRename } from '../app/inline-rename.js';
import { openLauncher } from '../app/modals/launcher.js';
import { openProjectEditor } from '../app/modals/project-editor.js';
import { openWhatsNew, readSeenVersion } from '../app/modals/whats-new.js';
import { openWorktrees } from '../app/modals/worktrees.js';
import { activeProjectId, orderedSessions } from '../app/selectors.js';
import type { ProjectInfo, SessionInfo } from '../app/state.js';
import { noteLocalClose } from '../app/undo-close.js';
import {
  beginDrag,
  endDrag,
  moveTo as movePlaceholder,
} from '../lib/drag-placeholder.js';
import { dropTargetIndex } from '../lib/reorder.js';
import { attentionSummary, sessionState } from '../lib/session-state.js';
import { hasUnread, latestVersion } from '../lib/whats-new.js';
import { readProjectId } from '../lib/wire.js';
import { appStore, toggleCollapsed, useAppStore } from '../store/store.js';

// Live read of the store, for the event handlers' non-reactive lookups.
const appData = () => appStore.getState();
import { Chip } from './Chip.js';
import { Icon } from './Icon.js';
import { IconButton } from './IconButton.js';
import { ProjectCard } from './ProjectCard.js';
import { SessionRow } from './SessionRow.js';

// Per-module, not a shared deps union: the sidebar wants
// refocusActiveTerm where view wants focusActiveTerm, and one union type
// would loosen both.
export interface SidebarProps {
  switchTo: (id: string) => void;
  switchToProject: (pid: string) => void;
  // Owned by view.ts: minimizing repaints the grid and can move focus.
  minimizeProject: (pid: string) => void;
  restoreProject: (pid: string) => void;
  minimizeSession: (id: string) => void;
  restoreSession: (id: string) => void;
  confirmAndDeleteProject: (p: ProjectInfo) => void;
  refocusActiveTerm: () => void;
  /** #minimized-projects. Null in tests that mount only #projects. */
  trayEl: HTMLElement | null;
}

// keyHints maps a session id to the digit ⌘n actually selects it with.
// app/keyboard.ts resolves ⌘n against orderedSessions()[n-1], so the hint
// has to be read off the same list — a per-project counter would label
// rows with keys that jump somewhere else entirely.
function keyHints(): Map<string, number> {
  const hints = new Map<string, number>();
  orderedSessions()
    .slice(0, 9)
    .forEach((s, i) => {
      hints.set(s.id, i + 1);
    });
  return hints;
}

// restoreChipLabel is the words channel for what a minimized project chip
// shows as glyphs. A screen reader gets no count from a number rendered in
// a span next to an icon, so the accessible name carries both — the same
// obligation the state icon meets with its own <title> (icons.md › state
// is shape + colour + words).
function restoreChipLabel(
  name: string,
  total: number,
  wanting: number,
): string {
  const n = `${total} session${total === 1 ? '' : 's'}`;
  const k =
    wanting === 0 ? '' : `, ${wanting} need${wanting === 1 ? 's' : ''} you`;
  return `Restore ${name}, ${n}${k}`;
}

// killSession routes a live session through the native confirm (AGENTS.md:
// destructive actions never skip it) and a dead one straight through, which
// is the rule session-term.ts's _closeDead already follows — there is
// nothing left to lose once the process is gone.
//
// force=false on the live branch, like every other kill path (main.tsx's
// close-session command, ⌘W, the menu): it lets the daemon refuse with
// worktree_dirty so events.ts can ask the real three-way question
// (cancel / close / close and delete the worktree). Forcing here would
// make this confirm — which only mentions scrollback — silently agree to
// throwing away uncommitted changes. The dead branch keeps force=true:
// there is no process to refuse and no worktree state worth guarding.
function killSession(s: SessionInfo) {
  const alive = s.alive !== false;
  if (!alive) {
    KillSession(s.id, true).catch(reportFailure('kill session'));
    return;
  }
  Confirm(
    'Kill session',
    `Kill ${s.name ?? 'this session'}? Its scrollback is lost.`,
  )
    .then((ok) => {
      if (!ok) return;
      noteLocalClose(s.id, s.name ?? 'Session');
      KillSession(s.id, false).catch(reportFailure('kill session'));
    })
    .catch(reportFailure('kill session'));
}

// reorderDroppedProject converts an above/below drop into the new
// Order index expected by UpdateProject. The daemon's moveProjectLocked
// removes the dragged project then inserts at newOrder, so we
// compensate when the source sits before the target.
function reorderDroppedProject(
  draggedID: string,
  targetID: string,
  above: boolean,
) {
  const ordered = [...appData().projects].sort(
    (a, b) => (a.order ?? 0) - (b.order ?? 0),
  );
  const targetIdx = ordered.findIndex((p) => p.id === targetID);
  const draggedIdx = ordered.findIndex((p) => p.id === draggedID);
  if (targetIdx < 0 || draggedIdx < 0) return;
  let newOrder = above ? targetIdx : targetIdx + 1;
  if (draggedIdx < newOrder) newOrder -= 1;
  if (newOrder === draggedIdx) return;
  UpdateProject(draggedID, '', '', '', newOrder).catch(
    reportFailure('reorder project'),
  );
}

// reorderDroppedSession hands the drop to lib/reorder.ts's dropTargetIndex
// and forwards the result. The index math lives there, next to the keyboard
// path's reorderTarget, because both rest on the same invariant — a session's
// .order IS its index in the daemon's r.order — and because a pure function
// is the only way to table-test the off-by-one this used to have.
function reorderDroppedSession(
  draggedID: string,
  targetID: string,
  above: boolean,
) {
  const order = dropTargetIndex(appData().sessions, draggedID, targetID, above);
  if (order === null) return;
  UpdateSession(draggedID, '', '', order).catch(reportFailure('reorder'));
}

// ---------- session row ----------

interface SessionItemProps {
  session: SessionInfo;
  index: number | null;
  selected: boolean;
  minimized: boolean;
  // The whole prop bag rather than three bound callbacks: main.tsx builds
  // it once for the life of the app, so it is a referentially stable
  // prop, where `() => switchTo(s.id)` would be a fresh function on
  // every parent render and would defeat the memo below.
  sidebar: SidebarProps;
}

// memo, and every other prop a primitive or the session's own object
// reference: this is where the phase's performance goal is actually
// collected. `updateSession()` replaces the sessions ARRAY on every
// title/updated event — one per phase step, one per surviving session
// when a kill recompacts the order, one when the agent-session-id poll
// lands — so the sidebar re-renders on each of them. Without this, every
// row's markup would be rebuilt at the child program's redraw rate,
// which is exactly the cost `updateSidebarTitles()` was added (spec 248)
// to avoid. With it, only the row whose SessionInfo reference actually
// changed re-renders.
//
// ProjectItem is deliberately NOT memoized: a card is a header and five
// icon buttons, its attention count is derived from the whole attention
// set, and the rows underneath it are already insulated by this memo.
const SessionItem = memo(function SessionItem(p: SessionItemProps) {
  const id = p.session.id;
  const nameRef = useRef<HTMLSpanElement>(null);

  // The callbacks below outlive the SessionInfo they were built from, so
  // they capture the id and read the session at call time. killSession in
  // particular branches on `alive`, and a stale `false` there sends
  // force=true, which skips the dirty-worktree refusal entirely.
  const live = (): SessionInfo =>
    appData().sessions.find((x) => x.id === id) ?? p.session;

  // Same-project drops only; cross-project moves are not supported yet
  // (would require also updating project_id on the wire).
  //
  // Shared by the row's own drop handler and the placeholder's: an
  // "insert above" spacer sits under the cursor, so the release often
  // lands on the spacer rather than on any row.
  const commit = (target: HTMLElement, above: boolean, e: DragEvent) => {
    const sid = e.dataTransfer?.getData('text/x-hive-session');
    const targetSID = target.dataset.sid ?? '';
    if (!sid || !targetSID || sid === targetSID) return;
    const dragged = appData().sessions.find((x) => x.id === sid);
    const dropped = appData().sessions.find((x) => x.id === targetSID);
    if (!dragged || !dropped) return;
    if (readProjectId(dragged) !== readProjectId(dropped)) return;
    reorderDroppedSession(sid, targetSID, above);
  };

  return (
    <SessionRow
      session={p.session}
      state={sessionState(p.session)}
      selected={p.selected}
      minimized={p.minimized}
      index={p.index}
      nameRef={nameRef}
      onSelect={() => p.sidebar.switchTo(id)}
      onMinimize={() => p.sidebar.minimizeSession(id)}
      onRestore={() => p.sidebar.restoreSession(id)}
      onRestart={() =>
        RestartSession(id).catch(reportFailure('restart session'))
      }
      onKill={() => killSession(live())}
      onWorktrees={() => {
        const proj = appData().projects.find(
          (x) => x.id === readProjectId(live()),
        );
        if (proj) openWorktrees(proj);
      }}
      onColor={(hex) =>
        UpdateSession(id, '', hex, -1).catch(reportFailure('color change'))
      }
      onDoubleClick={() => {
        const el = nameRef.current;
        if (!el) return;
        const sess = live();
        beginInlineRename({
          className: 'name-input',
          value: sess.name ?? '',
          mount: (input) => el.replaceWith(input),
          unmount: (input) => input.replaceWith(el),
          onCommit: (next) =>
            UpdateSession(id, next, '', -1).catch(reportFailure('rename')),
          onDone: () => p.sidebar.refocusActiveTerm(),
        });
      }}
      onDragStart={(e: ReactDragEvent<HTMLLIElement>) => {
        const dt = e.dataTransfer;
        if (!dt) return;
        dt.effectAllowed = 'move';
        dt.setData('text/x-hive-session', id);
        beginDrag(e.currentTarget, commit);
      }}
      onDragEnd={() => endDrag()}
      onDragOver={(e: ReactDragEvent<HTMLLIElement>) => {
        const dt = e.dataTransfer;
        if (!dt?.types.includes('text/x-hive-session')) return;
        e.preventDefault();
        dt.dropEffect = 'move';
        const r = e.currentTarget.getBoundingClientRect();
        movePlaceholder(e.currentTarget, e.clientY - r.top < r.height / 2);
      }}
      onDrop={(e: ReactDragEvent<HTMLLIElement>) => {
        e.preventDefault();
        const li = e.currentTarget;
        const r = li.getBoundingClientRect();
        const above = e.clientY - r.top < r.height / 2;
        endDrag();
        commit(li, above, e.nativeEvent);
      }}
    />
  );
});

// ---------- project card ----------

interface ProjectItemProps {
  project: ProjectInfo;
  sessions: SessionInfo[];
  activePID: string;
  collapsed: boolean;
  props: SidebarProps;
  hints: Map<string, number>;
  minimizedSessions: ReadonlySet<string>;
  activeId: string | null;
}

function ProjectItem(o: ProjectItemProps) {
  const p = o.project;
  const nameRef = useRef<HTMLSpanElement>(null);
  const headerRef = useRef<HTMLDivElement>(null);
  // Same helper the minimized chip uses, so the collapsed card's
  // "k waiting on you" and the chip's alert count can never disagree.
  const attentionCount = attentionSummary(o.sessions).count;

  // dragstart bubbles, so a session-row drag fires here too after its own
  // handler runs. We must not preventDefault in that case (it would
  // cancel the session drag). For drags that originate on the project
  // chrome (action buttons, rename input) we DO want to abort, since the
  // card itself is the closest draggable.
  const commit = (target: HTMLElement, above: boolean, e: DragEvent) => {
    const pid = e.dataTransfer?.getData('text/x-hive-project');
    const targetPID = target.dataset.pid ?? '';
    if (!pid || !targetPID || pid === targetPID) return;
    reorderDroppedProject(pid, targetPID, above);
  };

  // Anchored on the header's bounds, not the whole card: with sessions
  // expanded the card is tall, the cursor is almost always above its
  // midpoint, and the placeholder would land far from the cursor.
  const aboveHeader = (clientY: number, fallback: HTMLElement): boolean => {
    const r = (headerRef.current ?? fallback).getBoundingClientRect();
    return clientY - r.top < r.height / 2;
  };

  return (
    <ProjectCard
      project={p}
      collapsed={o.collapsed}
      active={p.id === o.activePID}
      attention={attentionCount > 0}
      sessionCount={o.sessions.length}
      attentionCount={attentionCount}
      headerRef={headerRef}
      nameRef={nameRef}
      onSelect={() => o.props.switchToProject(p.id)}
      onToggleCollapse={() => toggleCollapsed(p.id)}
      onNewSession={() => openLauncher(p.id)}
      onMinimize={() => o.props.minimizeProject(p.id)}
      onWorktrees={() => openWorktrees(p)}
      onEdit={() => openProjectEditor(p)}
      onDelete={() => o.props.confirmAndDeleteProject(p)}
      onHeaderDoubleClick={(e: MouseEvent<HTMLDivElement>) => {
        const el = nameRef.current;
        if (!el) return;
        if (e.target !== el && e.target !== headerRef.current) return;
        beginInlineRename({
          className: 'project-name-input',
          value: p.name ?? '',
          mount: (input) => el.replaceWith(input),
          unmount: (input) => input.replaceWith(el),
          onCommit: (next) =>
            UpdateProject(p.id, next, '', '', -1).catch(
              reportFailure('rename project'),
            ),
          onDone: () => o.props.refocusActiveTerm(),
        });
      }}
      onDragStart={(e: ReactDragEvent<HTMLLIElement>) => {
        const t = e.target;
        if (t instanceof Element) {
          // Bubbled from an inner session drag — leave it alone.
          if (t.closest('.hv-session-row')) return;
          if (
            t.closest('.hv-project-card__actions') ||
            t.closest('.project-name-input')
          ) {
            e.preventDefault();
            return;
          }
        }
        const dt = e.dataTransfer;
        if (!dt) return;
        dt.effectAllowed = 'move';
        dt.setData('text/x-hive-project', p.id);
        beginDrag(e.currentTarget, commit);
      }}
      onDragEnd={(e: ReactDragEvent<HTMLLIElement>) => {
        // Bubbled from an inner session drag, which owns its own teardown.
        if (e.target instanceof Element && e.target.closest('.hv-session-row'))
          return;
        endDrag();
      }}
      onDragOver={(e: ReactDragEvent<HTMLLIElement>) => {
        const dt = e.dataTransfer;
        if (!dt?.types.includes('text/x-hive-project')) return;
        e.preventDefault();
        dt.dropEffect = 'move';
        movePlaceholder(
          e.currentTarget,
          aboveHeader(e.clientY, e.currentTarget),
        );
      }}
      onDrop={(e: ReactDragEvent<HTMLLIElement>) => {
        const dt = e.dataTransfer;
        if (!dt?.types.includes('text/x-hive-project')) return;
        e.preventDefault();
        const card = e.currentTarget;
        const above = aboveHeader(e.clientY, card);
        endDrag();
        commit(card, above, e.nativeEvent);
      }}
    >
      {o.sessions.map((s) => (
        <SessionItem
          key={s.id}
          session={s}
          index={o.hints.get(s.id) ?? null}
          selected={s.id === o.activeId}
          minimized={o.minimizedSessions.has(s.id)}
          sidebar={o.props}
        />
      ))}
    </ProjectCard>
  );
}

// ---------- the sidebar ----------

export function Sidebar(props: SidebarProps) {
  const projects = useAppStore((s) => s.projects);
  const sessions = useAppStore((s) => s.sessions);
  const activeId = useAppStore((s) => s.activeId);
  const collapsed = useAppStore((s) => s.collapsed);
  const minimizedProjects = useAppStore((s) => s.minimizedProjects);
  const minimized = useAppStore((s) => s.minimized);
  // activeProjectId() reads currentProjectId / view / gridProjectId /
  // activeId off the store rather than taking them as arguments,
  // so it is subscribed as a derived string: the selector re-runs on
  // every store change, but only a different id re-renders the sidebar.
  const activePID = useAppStore(() => activeProjectId());

  const hints = keyHints();
  const visible = projects.filter((p) => !minimizedProjects.has(p.id));
  // Chips render in project order — the same order the rows would have if
  // nothing were minimized — so restoring one is visibly a no-op on
  // ordering.
  const minimizedList = projects.filter((p) => minimizedProjects.has(p.id));

  const tray = props.trayEl;
  // useLayoutEffect, not useEffect: the class lives on the portal
  // CONTAINER, outside React's tree, and the tray carries a border-top and
  // padding. A passive effect applies it after paint, so restoring the last
  // minimized project flashes one frame of an empty bordered stripe. The
  // imperative renderMinimizedProjects() toggled it in the same block that
  // emptied the tray; this is the same synchrony.
  useLayoutEffect(() => {
    tray?.classList.toggle('hidden', minimizedList.length === 0);
  }, [tray, minimizedList.length]);

  return (
    <>
      {visible.map((p) => (
        <ProjectItem
          key={p.id}
          project={p}
          sessions={sessions
            .filter((s) => readProjectId(s) === p.id)
            .sort((a, b) => (a.order ?? 0) - (b.order ?? 0))}
          activePID={activePID}
          collapsed={collapsed.has(p.id)}
          props={props}
          hints={hints}
          minimizedSessions={minimized}
          activeId={activeId}
        />
      ))}
      {tray
        ? createPortal(
            minimizedList.map((p) => {
              const own = sessions.filter((s) => readProjectId(s) === p.id);
              // The chip is the only surface left carrying attention for a
              // project whose rows are gone (patterns.md › Attention
              // bubbling), and now the only one carrying its size too.
              const sum = attentionSummary(own);
              const restore = restoreChipLabel(
                p.name ?? '',
                own.length,
                sum.count,
              );
              return (
                <Chip
                  key={p.id}
                  pid={p.id}
                  label={p.name ?? ''}
                  color={p.color}
                  active={p.id === activePID}
                  title={p.cwd ? `${p.name} — ${p.cwd}` : (p.name ?? '')}
                  ariaLabel={restore}
                  count={own.length}
                  attention={
                    sum.state
                      ? { count: sum.count, state: sum.state }
                      : undefined
                  }
                  // Clicking the chip body restores the project — the same
                  // thing the restore control does. A minimized row is a
                  // thing you put away; the only reason to click it is to
                  // get it back.
                  onClick={() => props.restoreProject(p.id)}
                  onRestore={() => props.restoreProject(p.id)}
                  restoreLabel={restore}
                />
              );
            }),
            tray,
          )
        : null}
    </>
  );
}

// ---------- the sidebar header's icon controls ----------

// index.html owns the <header> and the #new-project-btn element itself —
// initProjectEditor() wires its click, the launcher uses it as a focus
// fallback and the dom tests reach it by id — so React fills in the icon
// and appends the sibling rather than owning the markup.
//
// "Check for updates" sits next to "New project" because until it existed
// the only manual trigger was the macOS app menu's "Check for Updates…"
// item, invisible on every other platform and undiscoverable on that one.
//
// Null-guarded on purpose, exactly as the imperative
// wireCheckUpdatesButton() was: dom tests mount scaffolds with no sidebar
// header at all (update-banner, restart-hive), and a missing header must
// render nothing rather than throw.
export function SidebarHeaderControls(): ReactNode {
  // Seeded once: the bundled list cannot change under a running app, so the
  // only thing that clears this is the click below.
  const [unread, setUnread] = useState(() =>
    hasUnread(latestVersion(), readSeenVersion()),
  );
  const newProjectBtn = document.getElementById('new-project-btn');
  const header = newProjectBtn?.parentElement;
  if (!newProjectBtn || !header) return null;
  return (
    <>
      {createPortal(<Icon name="plus" />, newProjectBtn)}
      {createPortal(
        <IconButton
          id="check-updates-btn"
          icon="download"
          label="Check for updates"
          size={22}
          onClick={() => void manualUpdateCheck()}
        />,
        header,
      )}
      {/* Third and rightmost: What's new. Portal order is DOM order here, so
          this one has to stay last in the fragment. */}
      {createPortal(
        <IconButton
          id="whats-new-btn"
          icon="gift"
          label="What's new"
          size={22}
          className={unread ? 'hv-unread' : undefined}
          onClick={() => {
            // Clear the dot in the same render pass. `unread` is state rather
            // than a localStorage read at render because a read at render
            // never re-renders: the dot would outlive the click and sit there
            // until a reload.
            setUnread(false);
            openWhatsNew();
          }}
        />,
        header,
      )}
    </>
  );
}
