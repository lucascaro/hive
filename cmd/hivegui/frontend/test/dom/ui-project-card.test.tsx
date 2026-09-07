// @vitest-environment jsdom
import { describe, it, expect, vi } from 'vitest';
import { render } from '@testing-library/react';
import {
  ProjectCard,
  type ProjectCardProps,
} from '../../src/components/ProjectCard';
import type { ProjectInfo } from '../../src/app/state';

const noop = () => {};

function props(over: Partial<ProjectCardProps> = {}): ProjectCardProps {
  return {
    project: { id: 'p1', name: 'hive', color: '#0af' } as ProjectInfo,
    collapsed: false,
    active: false,
    attention: false,
    sessionCount: 2,
    attentionCount: 0,
    ideaCount: 0,
    onSelect: noop,
    onToggleCollapse: noop,
    onNewSession: noop,
    onMinimize: noop,
    onWorktrees: noop,
    onIdeas: noop,
    onEdit: noop,
    onDelete: noop,
    onHeaderDoubleClick: noop,
    headerRef: null,
    nameRef: null,
    onDragStart: noop,
    onDragEnd: noop,
    onDragOver: noop,
    onDrop: noop,
    ...over,
  };
}

function make(over: Partial<ProjectCardProps> = {}) {
  const r = render(<ProjectCard {...props(over)} />, {
    container: document.body.appendChild(document.createElement('ul')),
  });
  const root = r.container.querySelector<HTMLLIElement>('.hv-project-card');
  if (!root) throw new Error('no card rendered');
  const part = <T extends Element>(sel: string): T => {
    const el = root.querySelector<T>(sel);
    if (!el) throw new Error(`no ${sel}`);
    return el;
  };
  return {
    root,
    header: part<HTMLElement>('.hv-project-card__header'),
    name: part<HTMLElement>('.hv-project-card__name'),
    body: part<HTMLUListElement>('.hv-project-card__body'),
    rerender: r.rerender,
  };
}

describe('ProjectCard', () => {
  it('renders name, swatch colour and data-pid', () => {
    const { root, name } = make();
    expect(root.className).toBe('hv-project-card');
    expect(root.dataset.pid).toBe('p1');
    expect(name.textContent).toBe('hive');
    expect(root.style.getPropertyValue('--project-color')).toBe('#0af');
  });

  it('shows the session count expanded and "n sessions · k waiting on you" collapsed', () => {
    expect(
      make().root.querySelector('.hv-project-card__count')?.textContent,
    ).toBe('2');
    const collapsed = make({ collapsed: true, attentionCount: 1 });
    expect(collapsed.root.dataset.collapsed).toBe('');
    expect(
      collapsed.root.querySelector('.hv-project-card__count')?.textContent,
    ).toBe('2 sessions · 1 waiting on you');
    expect(
      make({
        collapsed: true,
        sessionCount: 1,
        attentionCount: 0,
      }).root.querySelector('.hv-project-card__count')?.textContent,
    ).toBe('1 session');
  });

  it('carries attention as data-state and nothing else changes on the header', () => {
    const { root } = make({ attention: true });
    expect(root.dataset.state).toBe('attention');
    expect(root.dataset.selected).toBeUndefined();
  });

  it('marks the active project with data-active, a separate channel from attention', () => {
    expect(make({ active: true }).root.dataset.active).toBe('');
    expect(make().root.dataset.active).toBeUndefined();
  });

  it('gives the chevron aria-expanded and toggles through the callback', () => {
    const onToggleCollapse = vi.fn();
    const { root } = make({ onToggleCollapse });
    const chev = root.querySelector<HTMLButtonElement>(
      '.hv-project-card__chevron',
    );
    expect(chev?.getAttribute('aria-expanded')).toBe('true');
    chev?.click();
    expect(onToggleCollapse).toHaveBeenCalledTimes(1);
    expect(
      make({ collapsed: true })
        .root.querySelector('.hv-project-card__chevron')
        ?.getAttribute('aria-expanded'),
    ).toBe('false');
  });

  it('wires all five header actions and none of them selects the project', () => {
    const spies = {
      onSelect: vi.fn(),
      onNewSession: vi.fn(),
      onMinimize: vi.fn(),
      onWorktrees: vi.fn(),
      onEdit: vi.fn(),
      onDelete: vi.fn(),
    };
    const { root, header } = make(spies);
    for (const a of ['new', 'minimize', 'worktrees', 'edit', 'delete']) {
      root.querySelector<HTMLButtonElement>(`[data-action="${a}"]`)?.click();
    }
    expect(spies.onNewSession).toHaveBeenCalledTimes(1);
    expect(spies.onMinimize).toHaveBeenCalledTimes(1);
    expect(spies.onWorktrees).toHaveBeenCalledTimes(1);
    expect(spies.onEdit).toHaveBeenCalledTimes(1);
    expect(spies.onDelete).toHaveBeenCalledTimes(1);
    expect(spies.onSelect).not.toHaveBeenCalled();
    header.click();
    expect(spies.onSelect).toHaveBeenCalledTimes(1);
  });

  it('renders its rows into the body list', () => {
    const r = render(
      <ProjectCard {...props()}>
        <li className="hv-session-row" data-sid="s1" />
      </ProjectCard>,
      { container: document.body.appendChild(document.createElement('ul')) },
    );
    const body = r.container.querySelector('.hv-project-card__body');
    expect(body?.tagName).toBe('UL');
    expect(body?.querySelector('[data-sid="s1"]')).not.toBeNull();
  });

  // What updateProjectCard() used to be. The build path and the patch path
  // had to reconcile the same set of fields and once didn't; there is only
  // one path now, and this pins every field of the card's state through it.
  it('re-renders active, attention, collapsed, chevron and the count', () => {
    const { root, rerender } = make();
    expect(root.dataset.active).toBeUndefined();

    rerender(
      <ProjectCard
        {...props({
          collapsed: true,
          active: true,
          attention: true,
          sessionCount: 3,
          attentionCount: 2,
        })}
      />,
    );
    expect(root.dataset.active).toBe('');
    expect(root.dataset.state).toBe('attention');
    expect(root.dataset.collapsed).toBe('');
    expect(root.querySelector('.hv-project-card__count')?.textContent).toBe(
      '3 sessions · 2 waiting on you',
    );
    const chev = root.querySelector('.hv-project-card__chevron');
    expect(chev?.getAttribute('aria-expanded')).toBe('false');
    expect(chev?.getAttribute('aria-label')).toBe('Expand hive');
    expect(chev?.querySelector('use')?.getAttribute('href')).toBe(
      '#hv-chevron-right',
    );

    rerender(<ProjectCard {...props({ sessionCount: 3 })} />);
    expect(root.dataset.active).toBeUndefined();
    expect(root.dataset.state).toBeUndefined();
    expect(root.dataset.collapsed).toBeUndefined();
    expect(root.querySelector('.hv-project-card__count')?.textContent).toBe(
      '3',
    );
    expect(
      root
        .querySelector('.hv-project-card__chevron')
        ?.getAttribute('aria-expanded'),
    ).toBe('true');
  });
});
