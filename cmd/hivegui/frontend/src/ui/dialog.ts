// ---------- dialog ----------
//
// One implementation of the modal shell that Settings, the worktree
// browser, the project editor, the help overlay and the choice dialog
// each grew separately. Consolidating them is worth doing because the
// differences between the five were bugs, not choices: only Settings
// guarded the "click that merely ENDS on the backdrop" case, only three
// of them consumed Escape, and the ids the keyboard pipeline keys off
// were spelled out by hand in index.html.
//
// What this does NOT own:
//   - Focus containment. keyboard.ts calls trapFocus() on the open
//     modal's root, because a dialog opened over a terminal starts with
//     focus outside it and a listener on the dialog would never fire.
//   - Where focus goes on open/close. Every modal has its own answer and
//     its own reason; see the modules.
//   - Hiding itself. onClose is the module's close function, which has
//     bookkeeping (in-flight loads, drafts, dismissing sub-dialogs) that
//     must run whichever gesture closed the dialog.
//
// The `hidden` class is load-bearing: registry.ts's anyModalOpen(),
// keyboard.ts's per-modal gates and every e2e visibility assertion read
// it. It is the open/closed signal, not a styling detail.
//
// See docs/design-docs/ui/components.md > dialog.

import { registerModal } from '../app/modals/registry.js';
import { iconButton } from './icon-button.js';

export type DialogSize = 'sm' | 'md' | 'lg';

export interface DialogSpec {
  id: string;
  title: string;
  size?: DialogSize;
  role?: 'dialog' | 'alertdialog';
  body?: (Node | null)[];
  actions?: (Node | null)[];
  hints?: (Node | null)[];
  onClose: () => void;
  closeOnBackdrop?: boolean;
  showCloseButton?: boolean;
}

export interface DialogHandle {
  el: HTMLElement;
  panel: HTMLElement;
  body: HTMLElement;
  footer: HTMLElement;
  isOpen(): boolean;
  show(): void;
  hide(): void;
  setTitle(text: string): void;
  setTitleSuffix(node: Node | null): void;
}

function keep(nodes: (Node | null)[] | undefined): Node[] {
  return (nodes ?? []).filter((n): n is Node => n != null);
}

export function dialog(spec: DialogSpec): DialogHandle {
  const el = document.createElement('div');
  el.id = spec.id;
  el.className = 'hv-dialog hidden';
  el.setAttribute('role', spec.role ?? 'dialog');
  el.setAttribute('aria-modal', 'true');

  const panel = document.createElement('div');
  panel.className = 'hv-dialog__panel';
  panel.dataset.size = spec.size ?? 'md';

  const header = document.createElement('header');
  header.className = 'hv-dialog__header';

  const titleId = `${spec.id}-title`;
  const title = document.createElement('h3');
  title.className = 'hv-dialog__title';
  title.id = titleId;
  title.textContent = spec.title;
  el.setAttribute('aria-labelledby', titleId);

  // Suffix rides inside the <h3> so the accessible name stays one
  // string ("Worktrees - hive"), which is what aria-labelledby reads.
  const suffix = document.createElement('span');
  suffix.className = 'hv-dialog__title-suffix';
  title.append(suffix);
  header.append(title);

  if (spec.showCloseButton !== false) {
    const close = iconButton({
      icon: 'x',
      label: 'Close',
      onClick: spec.onClose,
    });
    close.classList.add('hv-dialog__close');
    header.append(close);
  }

  const body = document.createElement('div');
  body.className = 'hv-dialog__body';
  body.append(...keep(spec.body));

  const footer = document.createElement('footer');
  footer.className = 'hv-dialog__footer';
  const hints = keep(spec.hints);
  const actions = keep(spec.actions);
  if (hints.length) {
    const hintSlot = document.createElement('div');
    hintSlot.className = 'hv-dialog__hints';
    hintSlot.append(...hints);
    footer.append(hintSlot);
  }
  if (actions.length) {
    const actionSlot = document.createElement('div');
    actionSlot.className = 'hv-dialog__actions';
    actionSlot.append(...actions);
    footer.append(actionSlot);
  }
  footer.hidden = hints.length === 0 && actions.length === 0;

  panel.append(header, body, footer);
  el.append(panel);

  // Escape is consumed here as well as handled. keyboard.ts's window
  // listener runs after this one and would otherwise see an
  // already-hidden dialog, fall past its gate, and spend the same
  // Escape on whatever is behind the modal.
  el.addEventListener('keydown', (e) => {
    if (e.key !== 'Escape') return;
    e.preventDefault();
    e.stopPropagation();
    spec.onClose();
  });

  if (spec.closeOnBackdrop !== false) {
    // Both ends of the gesture must land on the backdrop. A
    // text-selection drag that starts inside an input and releases
    // outside the panel dispatches its click on the nearest common
    // ancestor - the backdrop - so testing the click alone discards the
    // whole draft mid-edit.
    let downOnBackdrop = false;
    el.addEventListener('mousedown', (e) => {
      downOnBackdrop = e.target === el;
    });
    el.addEventListener('click', (e) => {
      const fire = downOnBackdrop && e.target === el;
      downOnBackdrop = false;
      if (fire) spec.onClose();
    });
  }

  registerModal(el);

  return {
    el,
    panel,
    body,
    footer,
    isOpen: () => !el.classList.contains('hidden'),
    show: () => el.classList.remove('hidden'),
    hide: () => el.classList.add('hidden'),
    setTitle: (text) => {
      if (title.firstChild) {
        title.firstChild.nodeValue = text;
      } else {
        title.prepend(document.createTextNode(text));
      }
    },
    setTitleSuffix: (node) => suffix.replaceChildren(...(node ? [node] : [])),
  };
}
