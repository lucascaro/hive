// An actionable hint pane when the current scope has nothing to show
// (first run, empty project, everything minimized). Mounted on
// #empty-state; the model stays pure in lib/empty-state.ts and is called
// here rather than stored — it is a projection of state the store
// already holds, and a second copy could go stale.
//
// The imperative renderer keyed its rebuild off a JSON signature of the
// model (data-sig) so it could skip an innerHTML wipe. React reconciles,
// so the signature went with the wipe that needed it. The slice
// subscriptions below are what bounds the work instead: nothing else in
// the store can re-render this pane.
import { useLayoutEffect, type ReactNode } from 'react';
import { emptyStateModel } from '../lib/empty-state.js';
import { isMac } from '../lib/platform.js';
import { readProjectId } from '../lib/wire.js';
import { openLauncher } from '../app/modals/launcher.js';
import { openProjectEditor } from '../app/modals/project-editor.js';
import { useAppStore } from '../store/store.js';
import { Button } from './Button.js';

export function EmptyState({ root }: { root: HTMLElement | null }): ReactNode {
  const projects = useAppStore((s) => s.projects);
  const sessions = useAppStore((s) => s.sessions);
  const view = useAppStore((s) => s.view);
  const currentProjectId = useAppStore((s) => s.currentProjectId);
  const gridProjectId = useAppStore((s) => s.gridProjectId);
  const minimized = useAppStore((s) => s.minimized);
  const minimizedProjects = useAppStore((s) => s.minimizedProjects);

  // The union the model needs: it asks "is every session in scope
  // hidden?", which must count both a session minimized on its own and
  // one whose whole project was minimized.
  const hidden = new Set(minimized);
  for (const s of sessions) {
    if (minimizedProjects.has(readProjectId(s))) hidden.add(s.id);
  }

  const model = emptyStateModel({
    projects,
    sessions,
    view,
    // `?? undefined`, not a widening of EmptyStateInput to `| null`: its
    // `= ''` parameter defaults fire on undefined only, so accepting null
    // there would change which default applies.
    currentProjectId: currentProjectId ?? undefined,
    gridProjectId: gridProjectId ?? undefined,
    minimized: hidden,
    isMac,
  });

  // #empty-state is the container outside React's tree; it keeps the id,
  // the `.hidden` class and data-kind exactly as the renderer left them.
  const kind = model?.kind ?? '';
  useLayoutEffect(() => {
    if (!root) return;
    root.classList.toggle('hidden', kind === '');
    root.dataset.kind = kind;
  }, [root, kind]);

  if (!model) return null;
  return (
    <>
      <div className="empty-title">{model.title}</div>
      <div className="empty-hint">{model.hint}</div>
      {model.actions.length ? (
        <div className="empty-actions">
          {model.actions.map((a, i) => (
            <Button
              key={a.id}
              // patterns.md: one primary action, the rest default.
              kind={i === 0 ? 'primary' : 'default'}
              icon="plus"
              label={a.label}
              onClick={(e) => {
                // The launcher opens synchronously; without this, the
                // same click bubbles to the document-level outside-click
                // closer and shuts it in the same tick.
                e.stopPropagation();
                if (a.id === 'new-session') openLauncher();
                else if (a.id === 'new-project') openProjectEditor(null);
              }}
            />
          ))}
        </div>
      ) : null}
    </>
  );
}
