// The sidebar island — the React replacement for src/app/sidebar.ts.
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
// View/focus callbacks arrive as props from main.ts, the composition
// root, for the same reason initSidebar(deps) took them: importing
// view.ts / keyboard.ts here would close an import cycle.
import {
  useEffect,
  useRef,
  type DragEvent as ReactDragEvent,
  type MouseEvent,
} from 'react';
import { createPortal } from 'react-dom';
import {
  Confirm,
  KillSession,
  RestartSession,
  UpdateProject,
  UpdateSession,
} from '../bridge.js';
import { reportFailure } from '../app/dom.js';
import { beginInlineRename } from '../app/inline-rename.js';
import { openLauncher } from '../app/modals/launcher.js';
import { openProjectEditor } from '../app/modals/project-editor.js';
import { openWorktrees } from '../app/modals/worktrees.js';
import { activeProjectId, orderedSessions } from '../app/selectors.js';
import { state, type ProjectInfo, type SessionInfo } from '../app/state.js';
import { noteLocalClose } from '../app/undo-close.js';
import {
  beginDrag,
  endDrag,
  moveTo as movePlaceholder,
} from '../lib/drag-placeholder.js';
import { dropTargetIndex } from '../lib/reorder.js';
import { sessionState } from '../lib/session-state.js';
import { readProjectId } from '../lib/wire.js';
import { toggleCollapsed, useAppStore } from '../store/store.js';
import { Chip } from './Chip.js';
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

// projectHasAttention reports whether any session in the project is
// ringing. A minimized project has no session rows in the sidebar, so
// the chip is the only surface left to carry the bell — without this a
// BEL inside a minimized project is invisible until ⌘B finds it.
function projectHasAttention(
  pid: string,
  sessions: SessionInfo[],
  attention: ReadonlySet<string>,
): boolean {
  return sessions.some((s) => readProjectId(s) === pid && attention.has(s.id));
}

// killSession routes a live session through the native confirm (AGENTS.md:
// destructive actions never skip it) and a dead one straight through, which
// is the rule session-term.ts's _closeDead already follows — there is
// nothing left to lose once the process is gone.
//
// force=false on the live branch, like every other kill path (main.ts's
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
  const ordered = [...state.projects].sort(
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
  const order = dropTargetIndex(state.sessions, draggedID, targetID, above);
  if (order === null) return;
  UpdateSession(draggedID, '', '', order).catch(reportFailure('reorder'));
}

// ---------- session row ----------

interface SessionItemProps {
  session: SessionInfo;
  index: number | null;
  selected: boolean;
  minimized: boolean;
  attention: boolean;
  onSelect: () => void;
  onMinimize: () => void;
  onRestore: () => void;
  refocusActiveTerm: () => void;
}

function SessionItem(p: SessionItemProps) {
  const id = p.session.id;
  const nameRef = useRef<HTMLSpanElement>(null);

  // The callbacks below outlive the SessionInfo they were built from, so
  // they capture the id and read the session at call time. killSession in
  // particular branches on `alive`, and a stale `false` there sends
  // force=true, which skips the dirty-worktree refusal entirely.
  const live = (): SessionInfo =>
    state.sessions.find((x) => x.id === id) ?? p.session;

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
    const dragged = state.sessions.find((x) => x.id === sid);
    const dropped = state.sessions.find((x) => x.id === targetSID);
    if (!dragged || !dropped) return;
    if (readProjectId(dragged) !== readProjectId(dropped)) return;
    reorderDroppedSession(sid, targetSID, above);
  };

  return (
    <SessionRow
      session={p.session}
      state={sessionState(p.session, p.attention)}
      selected={p.selected}
      minimized={p.minimized}
      index={p.index}
      nameRef={nameRef}
      onSelect={p.onSelect}
      onMinimize={p.onMinimize}
      onRestore={p.onRestore}
      onRestart={() =>
        RestartSession(id).catch(reportFailure('restart session'))
      }
      onKill={() => killSession(live())}
      onWorktrees={() => {
        const proj = state.projects.find((x) => x.id === readProjectId(live()));
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
          onDone: () => p.refocusActiveTerm(),
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
}

// ---------- project card ----------

interface ProjectItemProps {
  project: ProjectInfo;
  sessions: SessionInfo[];
  activePID: string;
  attention: ReadonlySet<string>;
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
  const attentionCount = o.sessions.filter((s) => o.attention.has(s.id)).length;

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
          attention={o.attention.has(s.id)}
          onSelect={() => o.props.switchTo(s.id)}
          onMinimize={() => o.props.minimizeSession(s.id)}
          onRestore={() => o.props.restoreSession(s.id)}
          refocusActiveTerm={o.props.refocusActiveTerm}
        />
      ))}
    </ProjectCard>
  );
}

// ---------- the island ----------

export function Sidebar(props: SidebarProps) {
  const projects = useAppStore((s) => s.projects);
  const sessions = useAppStore((s) => s.sessions);
  const activeId = useAppStore((s) => s.activeId);
  const collapsed = useAppStore((s) => s.collapsed);
  const minimizedProjects = useAppStore((s) => s.minimizedProjects);
  const minimized = useAppStore((s) => s.minimized);
  const attention = useAppStore((s) => s.attention);
  // activeProjectId() reads currentProjectId / view / gridProjectId /
  // activeId off the state facade rather than taking them as arguments,
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
  useEffect(() => {
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
          attention={attention}
          collapsed={collapsed.has(p.id)}
          props={props}
          hints={hints}
          minimizedSessions={minimized}
          activeId={activeId}
        />
      ))}
      {tray
        ? createPortal(
            minimizedList.map((p) => (
              <Chip
                key={p.id}
                pid={p.id}
                label={p.name ?? ''}
                color={p.color}
                active={p.id === activePID}
                title={p.cwd ? `${p.name} — ${p.cwd}` : (p.name ?? '')}
                ariaLabel={`Restore ${p.name}`}
                // The chip is the only surface left carrying a bell for a
                // project whose rows are gone (patterns.md › Attention
                // bubbling).
                attention={projectHasAttention(p.id, sessions, attention)}
                // Clicking the chip body restores the project — the same
                // thing the restore control does. A minimized row is a
                // thing you put away; the only reason to click it is to
                // get it back.
                onClick={() => props.restoreProject(p.id)}
                onRestore={() => props.restoreProject(p.id)}
                restoreLabel={`Restore ${p.name}`}
              />
            )),
            tray,
          )
        : null}
    </>
  );
}
