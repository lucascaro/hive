// Worktree group — docs/design-docs/ui/components.md › worktreeGroup.
//
// Sessions that share a git worktree share a working directory, and when
// the worktree is what named them they also share a NAME: registry
// create.go names a worktree session after its branch and does not
// uniquify, so two Claude sessions on feat/sidebar are byte-identical
// rows. The panel states the branch once, at the top, and lets each row
// be told apart by its window title.
//
// The panel is an <li> inside the project card's <ul> and holds its own
// <ul> of rows, so document order — which IS the painted order
// (lib/worktree-groups.ts › THE ONE ORDER) — is unchanged by the
// wrapping. Drag-reorder reads rows off the DOM by class and is
// indifferent to the nesting.
import {
  useEffect,
  useId,
  useRef,
  useState,
  type CSSProperties,
  type MouseEvent,
  type ReactNode,
} from 'react';
import { Icon, StateIcon } from './Icon.js';
import { cancelInlineRenameFor } from '../app/inline-rename.js';
import { useCollapseTransition } from '../lib/use-collapse.js';
import type { AttentionSummary } from '../lib/session-state.js';

export interface WorktreeGroupProps {
  /** Branch name, or '' for a detached worktree. */
  branch: string;
  /** How many sessions are in the group. Always ≥ 2. */
  count: number;
  /** The colour the members share — a session inherits the colour of the
      worktree it adopts (internal/registry/create.go), so the panel
      carries one bar for the group instead of one per row. */
  color: string;
  /** Attention across the group's members, from `attentionSummary()` —
      the same helper the project card and the minimized chip use, so the
      three cannot disagree. Collapsing hides the members' own state
      icons, and AGENTS.md does not allow a session to go silent to save
      space, so the count takes over while the body is hidden. */
  attention: AttentionSummary;
  /** The group's name, or '' when it has none. Persisted daemon-side on
      the owning project, NOT derived from the member names — a group is
      named on purpose, and its members keep whatever names they have. */
  label: string;
  /** Opens the rename editor over `el` (the title cell). Returns the
      input so the group can cancel it if the group unmounts first. */
  onRenameTitle?: (el: HTMLElement) => HTMLInputElement | undefined;
  children?: ReactNode;
}

export function WorktreeGroup(p: WorktreeGroupProps) {
  // Collapse is deliberately NOT the store's `collapsed` set: that set is
  // keyed by project id and pruned against the project list on every
  // project:list (lib/collapsed.ts), which would silently drop a worktree
  // key. Panel collapse is per-session-of-the-app state; the panel stays
  // mounted while the group exists, so useState survives every re-render
  // that matters.
  const [collapsed, setCollapsed] = useState(false);
  const animating = useCollapseTransition(collapsed);
  const bodyId = useId();
  const titleRef = useRef<HTMLSpanElement>(null);
  const editorRef = useRef<HTMLInputElement | null>(null);
  const label = p.branch || 'detached HEAD';
  // The <li> is keyed `wt:<path>` and React unmounts it outright the
  // moment the run drops below two members. Without this, a member
  // exiting mid-edit tears the input out of the DOM and the blur that
  // follows commits against a group that no longer exists. Identity-
  // checked (cancelInlineRenameFor, not cancelInlineRename) so this
  // cleanup cannot discard somebody else's open editor, and empty-dep
  // so it runs on unmount only — a dep list that changed would cancel
  // a live edit on every repaint, which a group gets on every broadcast.
  useEffect(
    () => () => {
      if (editorRef.current) cancelInlineRenameFor(editorRef.current);
    },
    [],
  );
  const openRename = (e: MouseEvent<HTMLDivElement>) => {
    if (!p.onRenameTitle) return;
    const el = titleRef.current;
    // closest(), not target identity: the branch name carries an <Icon>
    // svg, and a double-click landing on that icon is a double-click on
    // the title. The chevron, the count and the collapsed-state alert
    // all live OUTSIDE __title, so this excludes them without a deny
    // list — and a deny list would be dead code.
    if (!el || !(e.target instanceof Element)) return;
    if (!e.target.closest('.hv-worktree-group__title')) return;
    editorRef.current = p.onRenameTitle(el) ?? null;
  };
  const hidden = collapsed && p.attention.count > 0;
  const style = p.color
    ? ({ '--session-color': p.color } as CSSProperties)
    : undefined;
  return (
    <li
      className="hv-worktree-group"
      data-collapsed={collapsed ? '' : undefined}
      data-animating={animating ? '' : undefined}
      style={style}
    >
      {/* biome-ignore lint/a11y/noStaticElementInteractions: the same
          double-click-to-rename affordance the project header and the
          session row already carry, and it has the same gap they do:
          double-click is the only way in. The command palette has no
          rename command, and the Worktrees modal's button-triggered
          rename edits the BRANCH, not this name. Worth closing for all
          three at once, not with a fourth bespoke path here. */}
      <div className="hv-worktree-group__header" onDoubleClick={openRename}>
        <button
          type="button"
          className="hv-worktree-group__chevron"
          aria-expanded={!collapsed}
          aria-controls={bodyId}
          aria-label={`${collapsed ? 'Expand' : 'Collapse'} ${label}`}
          onClick={() => setCollapsed((c) => !c)}
        >
          <Icon name={collapsed ? 'chevron-right' : 'chevron-down'} />
        </button>
        {/* One cell for the whole title, so the rename editor has a
            single mount target whether or not a name exists. */}
        <span
          className="hv-worktree-group__title"
          ref={titleRef}
          title={
            p.label ? `${p.label} — worktree: ${label}` : `Worktree: ${label}`
          }
        >
          {p.label ? (
            <span className="hv-worktree-group__label">{p.label}</span>
          ) : null}
          {/* The branch stays visible even when the group is named: it
              is the one thing the panel exists to state, and the name
              is the operator's word for the work, not git's. */}
          <span className="hv-worktree-group__branch">
            <Icon name="branch" size={12} />
            {/* The text needs its own box: text-overflow is inert on the
                inline-flex container, whose text child is an anonymous
                flex item, so without this the branch clips mid-glyph
                instead of ellipsizing. */}
            <span className="hv-worktree-group__branch-name">{label}</span>
          </span>
        </span>
        {hidden ? (
          <span
            className="hv-worktree-group__alert"
            title={`${p.attention.count} waiting on you`}
          >
            <StateIcon state={p.attention.state ?? 'attention'} />
            {p.attention.count}
          </span>
        ) : null}
        {/* A title, not aria-label: a bare <span> has no role that
            supports one (biome a11y/useAriaPropsSupportedByRole), and the
            chevron's own label already names the branch. */}
        <span
          className="hv-worktree-group__count"
          title={`${p.count} sessions on ${label}`}
        >
          {p.count}
        </span>
      </div>
      {/* Clip wrapper; see ProjectCard and lib/use-collapse.ts. */}
      <div className="hv-worktree-group__body" id={bodyId}>
        <ul className="hv-worktree-group__rows">{p.children}</ul>
      </div>
    </li>
  );
}
