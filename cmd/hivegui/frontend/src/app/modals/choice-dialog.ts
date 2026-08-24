// ---------- choice dialog ----------
//
// A small in-DOM replacement for the native Confirm on paths where the
// question has more than two answers, or where the consequence needs
// spelling out before the user commits.
//
// Confirm() can only ask yes/no. Where the real question was three-way
// ("delete the worktree and the branch", "delete only the worktree",
// "do nothing") that forced two sequential prompts with no way back out
// of the second — and where the answer risks losing work, a one-line
// OS alert is a poor place to explain what is at stake.
//
// Registered with modals/registry.ts for its lifetime (and
// unregistered on close) so the focus pipeline leaves the terminal
// alone while a question is up — otherwise the active terminal grabs
// focus straight back and the dialog's buttons never hold it.
// keyboard.ts additionally consults choiceDialogOpen().

import { registerModal, unregisterModal } from './registry.js';

export interface Choice {
  label: string;
  // Returned from openChoiceDialog when this button is pressed.
  value: string;
  danger?: boolean;
}

export interface ChoiceSpec {
  title: string;
  // Secondary identifying line — a path, a session name.
  detail?: string;
  // What stands to be lost. Rendered as a warning list.
  bullets?: string[];
  // Closing explanation, e.g. what is recoverable afterwards.
  note?: string;
  // The FIRST choice is treated as the safe one: it takes focus, and
  // it is what Escape and a scrim click resolve to.
  choices: Choice[];
}

let dismiss: (() => void) | null = null;
let current: HTMLElement | null = null;

// choiceDialogEl is the open dialog's root, or null. keyboard.ts needs
// it to trap Tab: the dialog's own listener only sees keys once focus
// is already inside it, and when a dialog opens over a terminal the
// focus is emphatically not inside it yet.
export function choiceDialogEl(): HTMLElement | null {
  return current;
}

export function choiceDialogOpen(): boolean {
  return dismiss !== null;
}

// dismissChoiceDialog closes an open dialog as if the safe choice was
// picked, and reports whether there was one. keyboard.ts calls it so
// Escape backs out of the question rather than reaching the bindings
// underneath.
export function dismissChoiceDialog(): boolean {
  if (!dismiss) return false;
  dismiss();
  return true;
}

export function openChoiceDialog(spec: ChoiceSpec): Promise<string> {
  return new Promise((resolve) => {
    // Only one question at a time: a second would stack invisibly and
    // leave the first one's promise pending forever.
    dismiss?.();

    // Whatever had focus when the question was asked. A dialog is a
    // detour, so closing it should put the user back where they were —
    // on the Delete button of the row they were acting on, not adrift
    // on <body> with the surrounding panel's trap about to pull focus
    // to its first control.
    const opener = document.activeElement as HTMLElement | null;

    const overlay = document.createElement('div');
    overlay.className = 'choice-dialog';
    overlay.setAttribute('role', 'alertdialog');
    overlay.setAttribute('aria-modal', 'true');

    const box = document.createElement('div');
    box.className = 'choice-dialog-box';

    const title = document.createElement('h4');
    title.textContent = spec.title;
    box.appendChild(title);

    if (spec.detail) {
      const detail = document.createElement('p');
      detail.className = 'choice-dialog-detail';
      detail.textContent = spec.detail;
      box.appendChild(detail);
    }

    if (spec.bullets?.length) {
      const list = document.createElement('ul');
      list.className = 'choice-dialog-bullets';
      for (const b of spec.bullets) {
        const li = document.createElement('li');
        li.textContent = b;
        list.appendChild(li);
      }
      box.appendChild(list);
    }

    if (spec.note) {
      const note = document.createElement('p');
      note.className = 'choice-dialog-note';
      note.textContent = spec.note;
      box.appendChild(note);
    }

    const actions = document.createElement('div');
    actions.className = 'choice-dialog-actions';

    let settled = false;
    const finish = (value: string) => {
      if (settled) return;
      settled = true;
      dismiss = null;
      current = null;
      unregisterModal(overlay);
      overlay.remove();
      // Restore only if the opener is still on the page: the button
      // that raised the question is often the one the answer removes
      // (deleting a worktree takes its row with it). When it is gone,
      // leave focus alone rather than guessing — whatever is still
      // open owns the keyboard and will claim it on the next key.
      if (opener?.isConnected) opener.focus();
      resolve(value);
    };

    const safe = spec.choices[0];
    let safeBtn: HTMLButtonElement | null = null;
    for (const c of spec.choices) {
      const btn = document.createElement('button');
      btn.type = 'button';
      btn.textContent = c.label;
      btn.dataset.choice = c.value;
      if (c.danger) btn.className = 'danger';
      btn.addEventListener('click', () => finish(c.value));
      actions.appendChild(btn);
      if (c === safe) safeBtn = btn;
    }

    box.appendChild(actions);
    overlay.appendChild(box);

    overlay.addEventListener('keydown', (e) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        e.stopPropagation();
        finish(safe.value);
        return;
      }
    });
    // Clicking the scrim means the same as Escape.
    overlay.addEventListener('mousedown', (e) => {
      if (e.target === overlay) finish(safe.value);
    });

    dismiss = () => finish(safe.value);
    current = overlay;
    registerModal(overlay);
    document.body.appendChild(overlay);
    // The safe option holds focus, so a stray Enter can never destroy
    // anything.
    safeBtn?.focus();
  });
}
