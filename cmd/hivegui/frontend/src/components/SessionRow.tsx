// Session row — docs/design-docs/ui/components.md › sessionRow.
//
// 40px, two lines: name over window title. Grid is
// [state 14px] [text 1fr] [meta auto]; the meta column (key hint,
// worktree, agent code) is swapped for the action buttons on hover or
// keyboard focus — see patterns.md › Hover-revealed actions.
//
// React port of src/ui/session-row.ts. The build path and the
// updateSessionRow() patch path collapse into this one render — the two
// could not disagree because there is only one of them now.
//
// Drag-reorder, inline rename and double-click stay with the caller
// (components/Sidebar.tsx), which owns that behaviour, and reach the row
// through the drag/dblclick props and `nameRef`.
import {
  useEffect,
  useRef,
  type CSSProperties,
  type DragEvent,
  type Ref,
} from 'react';
import { StateIcon } from './Icon.js';
import { IconButton } from './IconButton.js';
import { Kbd } from './Kbd.js';
import { isClosing, phaseOf } from '../lib/phase-steps.js';
import { type SessionState, stateTooltip } from '../lib/session-state.js';
import { displayTitle } from '../lib/term-title.js';
import type { SessionInfo } from '../app/state.js';

export interface SessionRowProps {
  session: SessionInfo;
  state: SessionState;
  selected: boolean;
  minimized: boolean;
  index: number | null;
  onSelect: () => void;
  onMinimize: () => void;
  onRestore: () => void;
  onRestart: () => void;
  onKill: () => void;
  onWorktrees: () => void;
  onColor: (hex: string) => void;
  onDoubleClick: () => void;
  nameRef: Ref<HTMLSpanElement>;
  onDragStart: (e: DragEvent<HTMLLIElement>) => void;
  onDragEnd: (e: DragEvent<HTMLLIElement>) => void;
  onDragOver: (e: DragEvent<HTMLLIElement>) => void;
  onDrop: (e: DragEvent<HTMLLIElement>) => void;
}

// Line 2 when the program has published no window title. One channel per
// fact (README principle 2): the row says what the session is doing, and
// when it is doing nothing it says why. Never both title and state words.
function subtitleFor(s: SessionInfo, state: SessionState): string {
  const t = displayTitle(s.title, s.name);
  if (t) return t;
  // A teardown is not a startup. sessionState() folds both into
  // 'starting' (neither is `ready`), which is the right call for the
  // status ICON — but the words have to tell them apart, or a session
  // being killed says "Starting…" for the seconds a worktree removal
  // takes. Display-layer only: session-state.ts's resolution is Phase 2
  // semantics with its own tests.
  if (isClosing(phaseOf(s))) return 'Closing…';
  if (state === 'starting') return 'Starting…';
  if (state === 'exited') return 'Exited';
  if (state === 'error') {
    const err = (s.last_error ?? s.lastError ?? '').trim();
    return err ? `Exited — ${err}` : 'Exited';
  }
  return '';
}

// Agent short code: two letters, mono, in the meta column. `cl`, `co`,
// `ge`, `sh` fall out of "first two letters" for the built-ins, so there
// is no table to keep in sync with settings' user-defined agents.
function agentCode(agent?: string): string {
  return (agent ?? '').trim().slice(0, 2).toLowerCase();
}

export function SessionRow(p: SessionRowProps) {
  const s = p.session;
  const name = s.name ?? 'session';
  const sub = subtitleFor(s, p.state);
  const code = agentCode(s.agent);
  const wtBranch = s.worktreeBranch ?? s.worktree_branch;
  const hint = p.index === null ? null : `[${p.index}]`;
  // Restart is only offered where it means something (exited/error): a
  // running session's restart is the tile's job, not a one-click sidebar
  // action. patterns.md › Exited sessions — rotate first, x second.
  const wantsRestart = p.state === 'exited' || p.state === 'error';

  // The colour picker keeps its native input (components.md › Form
  // fields) and stays UNCONTROLLED: a controlled `value` would snap the
  // swatch back on every unrelated re-render while the user is still
  // dragging inside the native picker. Written only when the session's
  // colour actually changes, which is what the imperative row did.
  const colorRef = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (colorRef.current) colorRef.current.value = s.color || '#888888';
  }, [s.color]);

  const style = s.color
    ? ({ '--session-color': s.color } as CSSProperties)
    : undefined;

  return (
    // Click-to-select on the row is a convenience, not the keyboard
    // path: ⌘1–⌘9 and ⌘↑/⌘↓ select sessions (app/keyboard.ts) and every
    // control in the row is a real <button>. Carried over verbatim from
    // src/ui/session-row.ts.
    // biome-ignore lint/a11y/useKeyWithClickEvents: see above
    <li
      className="hv-session-row"
      data-sid={s.id}
      data-pid={s.projectId ?? s.project_id ?? ''}
      data-state={p.state}
      data-selected={p.selected ? '' : undefined}
      data-minimized={p.minimized ? '' : undefined}
      draggable
      style={style}
      onClick={(e) => {
        // The swatch opens the native picker; it must not also switch
        // sessions.
        if (
          e.target instanceof Element &&
          e.target.closest('.hv-session-row__swatch')
        ) {
          return;
        }
        p.onSelect();
      }}
      onDoubleClick={p.onDoubleClick}
      onDragStart={p.onDragStart}
      onDragEnd={p.onDragEnd}
      onDragOver={p.onDragOver}
      onDrop={p.onDrop}
    >
      <StateIcon
        state={p.state}
        className="hv-session-row__state"
        detail={stateTooltip(s, p.state)}
      />
      <span className="hv-session-row__text">
        <span className="hv-session-row__name" ref={p.nameRef}>
          {s.name ?? ''}
        </span>
        <span className="hv-session-row__sub" title={sub}>
          {sub}
        </span>
      </span>
      {/* The worktree control is NOT in `meta`: meta is the half of the
          hover swap that disappears the moment the pointer enters the row
          (or focus lands in it), so a button living there could never be
          clicked, and tabbing to it would display:none the focused
          element out from under the browser. It is both an indicator and
          a control, so it gets its own always-on slot outside the swap. */}
      {wtBranch ? (
        <IconButton
          icon="branch"
          label={`Worktree: ${wtBranch} — manage worktrees`}
          className="hv-session-row__worktree"
          onClick={(e) => {
            e.stopPropagation();
            p.onWorktrees();
          }}
        />
      ) : null}
      <span className="hv-session-row__meta">
        {hint ? <Kbd>{hint}</Kbd> : null}
        {code ? <span className="hv-session-row__agent">{code}</span> : null}
      </span>
      <span className="hv-session-row__actions">
        <IconButton
          icon={p.minimized ? 'plus' : 'minus'}
          label={`${p.minimized ? 'Restore' : 'Minimize'} ${name}`}
          action="minimize"
          onClick={(e) => {
            e.stopPropagation();
            if (p.minimized) p.onRestore();
            else p.onMinimize();
          }}
        />
        {wantsRestart ? (
          <IconButton
            icon="rotate"
            label={`Restart ${name}`}
            action="restart"
            onClick={(e) => {
              e.stopPropagation();
              p.onRestart();
            }}
          />
        ) : null}
        <IconButton
          icon="x"
          label={`Kill ${name}`}
          action="kill"
          onClick={(e) => {
            e.stopPropagation();
            p.onKill();
          }}
        />
      </span>
      <span className="hv-session-row__swatch">
        <input
          type="color"
          ref={colorRef}
          defaultValue={s.color || '#888888'}
          aria-label={`Colour for ${name}`}
          onChange={(e) => p.onColor(e.target.value)}
        />
      </span>
    </li>
  );
}
