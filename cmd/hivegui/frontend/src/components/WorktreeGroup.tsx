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
import { useState, type CSSProperties, type ReactNode } from 'react';
import { Icon } from './Icon.js';

export interface WorktreeGroupProps {
  /** Branch name, or '' for a detached worktree. */
  branch: string;
  /** How many sessions are in the group. Always ≥ 2. */
  count: number;
  /** The colour the members share — a session inherits the colour of the
      worktree it adopts (internal/registry/create.go), so the panel
      carries one bar for the group instead of one per row. */
  color: string;
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
  const label = p.branch || 'detached HEAD';
  const style = p.color
    ? ({ '--session-color': p.color } as CSSProperties)
    : undefined;
  return (
    <li
      className="hv-worktree-group"
      data-collapsed={collapsed ? '' : undefined}
      style={style}
    >
      <div className="hv-worktree-group__header">
        <button
          type="button"
          className="hv-worktree-group__chevron"
          aria-expanded={!collapsed}
          aria-label={`${collapsed ? 'Expand' : 'Collapse'} ${label}`}
          onClick={() => setCollapsed((c) => !c)}
        >
          <Icon name={collapsed ? 'chevron-right' : 'chevron-down'} />
        </button>
        <span
          className="hv-worktree-group__branch"
          title={`Worktree: ${label}`}
        >
          <Icon name="branch" size={12} />
          {label}
        </span>
        <span className="hv-worktree-group__count">{p.count}</span>
      </div>
      <ul className="hv-worktree-group__body">{p.children}</ul>
    </li>
  );
}
