// @vitest-environment jsdom
import { describe, it, expect, vi } from 'vitest';
import { projectCard, updateProjectCard } from '../../src/ui/project-card';
import type { ProjectInfo } from '../../src/app/state';

const noop = () => {};
const make = (over: Partial<Parameters<typeof projectCard>[0]> = {}) =>
  projectCard({
    project: { id: 'p1', name: 'hive', color: '#0af' } as ProjectInfo,
    collapsed: false,
    active: false,
    attention: false,
    sessionCount: 2,
    attentionCount: 0,
    onSelect: noop,
    onToggleCollapse: noop,
    onNewSession: noop,
    onMinimize: noop,
    onWorktrees: noop,
    onEdit: noop,
    onDelete: noop,
    ...over,
  });

describe('projectCard', () => {
  it('renders name, swatch colour and data-pid', () => {
    const { root, name } = make();
    expect(root.className).toBe('hv-project-card');
    expect(root.dataset.pid).toBe('p1');
    expect(name.textContent).toBe('hive');
    expect(root.style.getPropertyValue('--project-color')).toBe('#0af');
  });

  it('shows the session count expanded and "n sessions · k need you" collapsed', () => {
    expect(
      make().root.querySelector('.hv-project-card__count')?.textContent,
    ).toBe('2');
    const collapsed = make({ collapsed: true, attentionCount: 1 });
    expect(collapsed.root.dataset.collapsed).toBe('');
    expect(
      collapsed.root.querySelector('.hv-project-card__count')?.textContent,
    ).toBe('2 sessions · 1 needs you');
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
    document.body.append(root);
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
    root.remove();
  });

  it('returns a body list that is the append target for rows', () => {
    const { root, body } = make();
    expect(body.tagName).toBe('UL');
    expect(body.parentElement).toBe(root);
  });
});

describe('updateProjectCard', () => {
  // The build path and the patch path must reconcile the SAME set of
  // fields. Task 2's regression was a patch that covered only part of
  // what build renders; this pins every field of ProjectCardState.
  it('reconciles active, attention, collapsed, chevron and the count', () => {
    const { root } = make();
    expect(root.dataset.active).toBeUndefined();

    updateProjectCard(root, 'hive', {
      collapsed: true,
      active: true,
      attention: true,
      sessionCount: 3,
      attentionCount: 2,
    });
    expect(root.dataset.active).toBe('');
    expect(root.dataset.state).toBe('attention');
    expect(root.dataset.collapsed).toBe('');
    expect(root.querySelector('.hv-project-card__count')?.textContent).toBe(
      '3 sessions · 2 need you',
    );
    const chev = root.querySelector('.hv-project-card__chevron');
    expect(chev?.getAttribute('aria-expanded')).toBe('false');
    expect(chev?.getAttribute('aria-label')).toBe('Expand hive');
    expect(chev?.querySelector('use')?.getAttribute('href')).toBe(
      '#hv-chevron-right',
    );

    updateProjectCard(root, 'hive', {
      collapsed: false,
      active: false,
      attention: false,
      sessionCount: 3,
      attentionCount: 0,
    });
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
