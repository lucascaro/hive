// @vitest-environment jsdom
//
// The empty-state pane (src/components/EmptyState.tsx, mounted on
// #empty-state). The pure model is already covered by
// test/unit/empty-state.test.ts — this file tests the projection: that
// the component renders whatever emptyStateModel returns, toggles
// .hidden + data-kind on the container the same way the old imperative
// renderer did, and that the primary action actually does something.
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render } from '@testing-library/react';
import { fireEvent } from '@testing-library/react';
import { EmptyState } from '../../src/components/EmptyState.js';
import { resetStore } from '../../src/store/store.js';

const launcher = vi.hoisted(() => ({ openLauncher: vi.fn() }));
const projectEditor = vi.hoisted(() => ({ openProjectEditor: vi.fn() }));
vi.mock('../../src/app/modals/launcher.js', () => launcher);
vi.mock('../../src/app/modals/project-editor.js', () => projectEditor);

function mount(): HTMLElement {
  const root = document.createElement('div');
  root.id = 'empty-state';
  document.body.appendChild(root);
  render(<EmptyState root={root} />, { container: root });
  return root;
}

beforeEach(() => {
  document.body.innerHTML = '';
  resetStore();
  launcher.openLauncher.mockClear();
  projectEditor.openProjectEditor.mockClear();
});

describe('EmptyState', () => {
  it('hides the pane and clears data-kind when there is nothing to show', () => {
    // A live session with nothing minimized: emptyStateModel returns
    // null, and the pane must not render or claim a kind.
    resetStore({
      sessions: [{ id: 's1', project_id: 'p1' } as never],
    });
    const root = mount();
    expect(root.classList.contains('hidden')).toBe(true);
    expect(root.dataset.kind).toBe('');
    expect(root.querySelector('.empty-title')).toBeNull();
  });

  it('shows the pane with the right data-kind and renders the model onto the DOM', () => {
    resetStore({ sessions: [], projects: [] });
    const root = mount();
    expect(root.classList.contains('hidden')).toBe(false);
    expect(root.dataset.kind).toBe('first-run');
    expect(root.querySelector('.empty-title')?.textContent).toBe(
      'No sessions yet',
    );
    expect(root.querySelector('.empty-hint')).not.toBeNull();
    const actions = root.querySelectorAll('.empty-actions .hv-button');
    // No sessions and no projects: both "new session" and "new project"
    // actions are offered.
    expect(actions.length).toBe(2);
  });

  it("the first-run pane's primary action opens the launcher", () => {
    resetStore({ sessions: [], projects: [] });
    const root = mount();
    const primary = root.querySelector(
      '.empty-actions .hv-button[data-kind="primary"]',
    ) as HTMLButtonElement;
    expect(primary).not.toBeNull();
    fireEvent.click(primary);
    expect(launcher.openLauncher).toHaveBeenCalledTimes(1);
    expect(projectEditor.openProjectEditor).not.toHaveBeenCalled();
  });
});
