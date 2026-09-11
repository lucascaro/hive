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
import { Icon, StateIcon } from './Icon.js';
import { IconButton } from './IconButton.js';
import { Kbd } from './Kbd.js';
import { isClosing, phaseOf } from '../lib/phase-steps.js';
import { type SessionState, stateTooltip } from '../lib/session-state.js';
import { displayTitle } from '../lib/term-title.js';
import { displayName } from '../lib/session-name.js';
import { useAppStore } from '../store/store.js';
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
  /** The idea this session was started from, when it came from one. */
  ideaText: string;
  /** Render the window title as the row's PRIMARY line and drop the
      name. Set for a session inside a worktree group whose name is the
      branch-derived default the panel header already states — a name the
      user chose is never hidden. */
  titleOnly: boolean;
  /** How many sessions share this row's worktree; 1 (or 0) when it is not
      shared. The row is linked to its group by colour — sessions inherit the
      colour of the worktree they adopt — and colour alone is not a signal
      everyone can read, so the count is the second channel. */
  worktreeShared: number;
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
  // The agent's own colour, from the catalog ListAgents() returned at
  // boot. Undefined before that reply lands and for a custom agent that
  // declares none — both render the plain code, never an invented hue.
  const agentColor = useAppStore((st) => st.agentColors.get(s.agent ?? ''));
  const wtBranch = s.worktreeBranch ?? s.worktree_branch;
  const shared = p.worktreeShared;
  // A detached worktree has no branch (internal/worktree: inventory), and the
  // glyph is where the count and the words live. Without this the cue on such
  // a row would be the colour bar alone — which is the one thing
  // patterns.md > Selection vs attention says it must never be.
  const wtLabel = wtBranch || 'detached HEAD';
  // A note can be 4 KiB. A tooltip is a glance and a screen reader
  // announces the label in full, so both take the first line's worth
  // and stop.
  const ideaLabel = p.ideaText
    ? `Started from an idea: ${
        p.ideaText.length > 80 ? `${p.ideaText.slice(0, 80)}…` : p.ideaText
      }`
    : '';
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
      data-wt-shared={shared > 1 ? '' : undefined}
      draggable
      style={style}
      onClick={(e) => {
        // The colour bar opens the native picker; it must not also
        // switch sessions.
        if (
          e.target instanceof Element &&
          e.target.closest('.hv-session-row__colour')
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
      {/* Name and title are direct grid children, not a stacked column:
          line 2 spans from the name's column to the row's right edge
          (session-row.css), which a wrapper confined to column 2 could
          never do. The wrapper was the reason the window title truncated
          ~70px early. */}
      <span
        className="hv-session-row__name"
        ref={p.nameRef}
        title={p.titleOnly && sub ? sub : undefined}
      >
        {/* subtitleFor() is empty for a running session that has published
            no window title, and displayTitle() suppresses one that just
            echoes the name — so titleOnly falls back to the name rather
            than rendering a row with no line at all. */}
        {p.titleOnly && sub ? sub : displayName(s)}
      </span>
      {p.titleOnly ? null : (
        <span className="hv-session-row__sub" title={sub}>
          {sub}
        </span>
      )}
      {/* The worktree control is NOT in `meta`: meta is the half of the
          hover swap that disappears the moment the pointer enters the row
          (or focus lands in it), so a button living there could never be
          clicked, and tabbing to it would display:none the focused
          element out from under the browser. It is both an indicator and
          a control, so it gets its own always-on slot outside the swap. */}
      {/* Where the session came from. An indicator, not a control —
          the inbox is reached from the project card's badge, and a
          second route to it from every row would put the same action
          in two places. Beside the worktree slot rather than in `meta`
          for the same reason that one is: `meta` is display:none the
          moment the pointer enters the row. */}
      {p.ideaText ? (
        <span
          className="hv-session-row__idea"
          role="img"
          title={ideaLabel}
          aria-label={ideaLabel}
        >
          <Icon name="idea" size={12} />
        </span>
      ) : null}
      {wtBranch || shared > 1 ? (
        <span className="hv-session-row__worktree-slot">
          <IconButton
            icon="branch"
            label={
              shared > 1
                ? `Worktree: ${wtLabel} — shared with ${shared - 1} other ${
                    shared === 2 ? 'session' : 'sessions'
                  } — manage worktrees`
                : `Worktree: ${wtLabel} — manage worktrees`
            }
            className="hv-session-row__worktree"
            onClick={(e) => {
              e.stopPropagation();
              p.onWorktrees();
            }}
          />
          {shared > 1 ? (
            <span className="hv-session-row__worktree-count" aria-hidden="true">
              {shared}
            </span>
          ) : null}
        </span>
      ) : null}
      <span className="hv-session-row__meta">
        {hint ? <Kbd>{hint}</Kbd> : null}
        {code ? (
          <span
            className={`hv-session-row__agent${
              agentColor ? '' : ' hv-session-row__agent--plain'
            }`}
            style={
              agentColor
                ? ({ '--agent-color': agentColor } as CSSProperties)
                : undefined
            }
          >
            {code}
          </span>
        ) : null}
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
      {/* The session colour, and its control, are the same thing: a 3px
          bar on the row's right edge that widens to 12px on hover or
          keyboard focus and opens the native picker. The row reserves
          that 12px permanently (session-row.css), so widening costs no
          reflow and never moves the hover-revealed actions. */}
      <span className="hv-session-row__colour">
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
