// Project card — docs/design-docs/ui/components.md › projectCard.
//
// A raised card per project: 30px header (chevron, colour swatch, name,
// count, hover actions) over a body that holds the session rows. Attention
// on any child session bubbles to the header swatch and, when collapsed,
// to the count (patterns.md › Attention bubbling).
//
// Returns the nodes app/sidebar.ts needs to do its own job: the body to
// fill, the header to anchor the drag hit-test on, the name to mount an
// inline rename into.
import { icon } from './icon.js';
import { iconButton } from './icon-button.js';
import type { ProjectInfo } from '../app/state.js';

export interface ProjectCardState {
  collapsed: boolean;
  active: boolean;
  attention: boolean;
  sessionCount: number;
  attentionCount: number;
}

export interface ProjectCardOpts extends ProjectCardState {
  project: ProjectInfo;
  onSelect: () => void;
  onToggleCollapse: () => void;
  onNewSession: () => void;
  onMinimize: () => void;
  onWorktrees: () => void;
  onEdit: () => void;
  onDelete: () => void;
}

function countText(o: ProjectCardState): string {
  if (!o.collapsed) return String(o.sessionCount);
  const n = `${o.sessionCount} session${o.sessionCount === 1 ? '' : 's'}`;
  if (o.attentionCount === 0) return n;
  return `${n} · ${o.attentionCount} need${o.attentionCount === 1 ? 's' : ''} you`;
}

export function projectCard(o: ProjectCardOpts): {
  root: HTMLLIElement;
  header: HTMLElement;
  body: HTMLUListElement;
  name: HTMLElement;
} {
  const p = o.project;
  const root = document.createElement('li');
  root.className = 'hv-project-card';
  root.dataset.pid = p.id;
  root.draggable = true;
  if (p.color) root.style.setProperty('--project-color', p.color);

  const header = document.createElement('div');
  header.className = 'hv-project-card__header';

  // A real <button> so the chevron is keyboard-operable and can carry
  // aria-expanded.
  const chevron = document.createElement('button');
  chevron.type = 'button';
  chevron.className = 'hv-project-card__chevron';
  chevron.addEventListener('click', (e) => {
    e.stopPropagation();
    o.onToggleCollapse();
  });

  const swatch = document.createElement('span');
  swatch.className = 'hv-project-card__swatch';

  const name = document.createElement('span');
  name.className = 'hv-project-card__name';
  name.textContent = p.name ?? '';
  name.title = p.cwd ? `${p.name} — ${p.cwd}` : (p.name ?? '');

  const count = document.createElement('span');
  count.className = 'hv-project-card__count';

  const actions = document.createElement('span');
  actions.className = 'hv-project-card__actions';
  const act = (
    name_: string,
    ic: Parameters<typeof iconButton>[0]['icon'],
    label: string,
    fn: () => void,
  ): HTMLButtonElement => {
    const b = iconButton({
      icon: ic,
      label,
      onClick: (e) => {
        e.stopPropagation();
        fn();
      },
    });
    b.dataset.action = name_;
    return b;
  };
  actions.append(
    act('new', 'plus', `New session in ${p.name ?? 'project'}`, o.onNewSession),
    // The binding is shown inline, per AGENTS.md › Key Discoverability.
    act(
      'worktrees',
      'branch',
      `Worktrees in ${p.name ?? 'project'} (⌘E)`,
      o.onWorktrees,
    ),
    act('edit', 'settings', `Edit ${p.name ?? 'project'}`, o.onEdit),
    act('minimize', 'minus', `Minimize ${p.name ?? 'project'}`, o.onMinimize),
    act('delete', 'x', `Delete ${p.name ?? 'project'}`, o.onDelete),
  );

  header.append(chevron, swatch, name, count, actions);
  header.addEventListener('click', (e) => {
    // Only the row background, swatch or name selects. Every control stops
    // propagation in its own handler; this is the belt to that's braces.
    const t = e.target;
    if (!(t instanceof Element)) return;
    if (
      t.closest('.hv-project-card__actions') ||
      t.closest('.hv-project-card__chevron')
    ) {
      return;
    }
    o.onSelect();
  });

  const body = document.createElement('ul');
  body.className = 'hv-project-card__body';

  root.append(header, body);
  applyState(root, p.name ?? 'project', o);
  return { root, header, body, name };
}

// updateProjectCard patches an existing card. Exists for the same reason
// updateSessionRow does: app/sidebar.ts's updateSidebarSelection /
// updateSidebarTitles repaint without rebuilding, because a rebuild between
// two clicks eats the dblclick pair that starts a rename. Everything the
// build path derives from ProjectCardState is reconciled here — a bell that
// arrives without a rebuild changes the attention flag AND, on a collapsed
// card, the count line.
export function updateProjectCard(
  root: HTMLElement,
  projectName: string,
  next: ProjectCardState,
): void {
  applyState(root, projectName, next);
}

function applyState(
  root: HTMLElement,
  projectName: string,
  next: ProjectCardState,
): void {
  if (next.collapsed) root.dataset.collapsed = '';
  else delete root.dataset.collapsed;
  if (next.active) root.dataset.active = '';
  else delete root.dataset.active;
  if (next.attention) root.dataset.state = 'attention';
  else delete root.dataset.state;

  const chevron = root.querySelector<HTMLButtonElement>(
    '.hv-project-card__chevron',
  );
  if (chevron) {
    chevron.setAttribute('aria-expanded', String(!next.collapsed));
    chevron.setAttribute(
      'aria-label',
      `${next.collapsed ? 'Expand' : 'Collapse'} ${projectName}`,
    );
    chevron.replaceChildren(
      icon(next.collapsed ? 'chevron-right' : 'chevron-down'),
    );
  }

  const count = root.querySelector<HTMLElement>('.hv-project-card__count');
  if (count) count.textContent = countText(next);
}
