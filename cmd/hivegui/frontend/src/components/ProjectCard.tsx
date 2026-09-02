// Project card — docs/design-docs/ui/components.md › projectCard.
//
// A raised card per project: 30px header (chevron, colour swatch, name,
// count, hover actions) over a body that holds the session rows.
// Attention on any child session bubbles to the header swatch and, when
// collapsed, to the count (patterns.md › Attention bubbling).
//
// React port of src/ui/project-card.ts. The rows are `children`, the
// header is exposed through `headerRef` (the drag hit-test anchors on its
// bounds) and the name through `nameRef` (inline rename mounts there) —
// the three things the imperative projectCard() returned.
import type {
  CSSProperties,
  DragEvent,
  MouseEvent,
  ReactNode,
  Ref,
} from 'react';
import { Icon } from './Icon.js';
import { IconButton } from './IconButton.js';
import type { ProjectInfo } from '../app/state.js';

export interface ProjectCardProps {
  project: ProjectInfo;
  collapsed: boolean;
  active: boolean;
  attention: boolean;
  sessionCount: number;
  attentionCount: number;
  onSelect: () => void;
  onToggleCollapse: () => void;
  onNewSession: () => void;
  onMinimize: () => void;
  onWorktrees: () => void;
  onEdit: () => void;
  onDelete: () => void;
  onHeaderDoubleClick: (e: MouseEvent<HTMLDivElement>) => void;
  headerRef: Ref<HTMLDivElement>;
  nameRef: Ref<HTMLSpanElement>;
  onDragStart: (e: DragEvent<HTMLLIElement>) => void;
  onDragEnd: (e: DragEvent<HTMLLIElement>) => void;
  onDragOver: (e: DragEvent<HTMLLIElement>) => void;
  onDrop: (e: DragEvent<HTMLLIElement>) => void;
  children?: ReactNode;
}

function countText(p: ProjectCardProps): string {
  if (!p.collapsed) return String(p.sessionCount);
  const n = `${p.sessionCount} session${p.sessionCount === 1 ? '' : 's'}`;
  if (p.attentionCount === 0) return n;
  return `${n} · ${p.attentionCount} need${p.attentionCount === 1 ? 's' : ''} you`;
}

export function ProjectCard(p: ProjectCardProps) {
  const proj = p.project;
  const name = proj.name ?? 'project';
  const style = proj.color
    ? ({ '--project-color': proj.color } as CSSProperties)
    : undefined;

  return (
    <li
      className="hv-project-card"
      data-pid={proj.id}
      data-collapsed={p.collapsed ? '' : undefined}
      data-active={p.active ? '' : undefined}
      data-state={p.attention ? 'attention' : undefined}
      draggable
      style={style}
      onDragStart={p.onDragStart}
      onDragEnd={p.onDragEnd}
      onDragOver={p.onDragOver}
      onDrop={p.onDrop}
    >
      {/* Click-to-select on the header background is a convenience, not
          the keyboard path: every control inside it is a real <button>,
          and selecting a project from the keyboard is ⌘[ / ⌘] (see
          app/keyboard.ts). Adding a role or a key handler here would put
          a second, undocumented way to do the same thing in the tab
          order. Carried over verbatim from src/ui/project-card.ts. */}
      {/* biome-ignore lint/a11y/noStaticElementInteractions: see above */}
      {/* biome-ignore lint/a11y/useKeyWithClickEvents: see above */}
      <div
        className="hv-project-card__header"
        ref={p.headerRef}
        onClick={(e) => {
          // Only the row background, swatch or name selects. Every
          // control stops propagation in its own handler; this is the
          // belt to that's braces.
          if (!(e.target instanceof Element)) return;
          if (
            e.target.closest('.hv-project-card__actions') ||
            e.target.closest('.hv-project-card__chevron')
          ) {
            return;
          }
          p.onSelect();
        }}
        onDoubleClick={p.onHeaderDoubleClick}
      >
        {/* A real <button> so the chevron is keyboard-operable and can
            carry aria-expanded. */}
        <button
          type="button"
          className="hv-project-card__chevron"
          aria-expanded={!p.collapsed}
          aria-label={`${p.collapsed ? 'Expand' : 'Collapse'} ${name}`}
          onClick={(e) => {
            e.stopPropagation();
            p.onToggleCollapse();
          }}
        >
          <Icon name={p.collapsed ? 'chevron-right' : 'chevron-down'} />
        </button>
        <span className="hv-project-card__swatch" />
        <span
          className="hv-project-card__name"
          ref={p.nameRef}
          title={proj.cwd ? `${proj.name} — ${proj.cwd}` : (proj.name ?? '')}
        >
          {proj.name ?? ''}
        </span>
        <span className="hv-project-card__count">{countText(p)}</span>
        <span className="hv-project-card__actions">
          <IconButton
            icon="plus"
            label={`New session in ${name}`}
            action="new"
            onClick={(e) => {
              e.stopPropagation();
              p.onNewSession();
            }}
          />
          {/* The binding is shown inline, per AGENTS.md › Key
              Discoverability. */}
          <IconButton
            icon="branch"
            label={`Worktrees in ${name} (⌘E)`}
            action="worktrees"
            onClick={(e) => {
              e.stopPropagation();
              p.onWorktrees();
            }}
          />
          <IconButton
            icon="settings"
            label={`Edit ${name}`}
            action="edit"
            onClick={(e) => {
              e.stopPropagation();
              p.onEdit();
            }}
          />
          <IconButton
            icon="minus"
            label={`Minimize ${name}`}
            action="minimize"
            onClick={(e) => {
              e.stopPropagation();
              p.onMinimize();
            }}
          />
          <IconButton
            icon="x"
            label={`Delete ${name}`}
            action="delete"
            onClick={(e) => {
              e.stopPropagation();
              p.onDelete();
            }}
          />
        </span>
      </div>
      <ul className="hv-project-card__body">{p.children}</ul>
    </li>
  );
}
