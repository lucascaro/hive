// ---------- choice dialog: the non-React half ----------
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
// The panel renders from components/modals/ChoiceDialog.tsx (Phase 4).
// What stays here is the promise: callers `await openChoiceDialog(spec)`
// from plain async code (events.ts's worktree-dirty kill, the worktree
// browser's two destructive flows), so the resolver has to live outside
// React. The component reports the answer through resolveChoiceDialog.
//
// Before Phase 4 the dialog was built per question and appended to
// <body>, which meant it had to be unregistered from modals/registry.ts
// on close — a detached element has no `hidden` class, so forgetting
// would make anyModalOpen() answer true forever and permanently strand
// the keyboard. The root is now static (index.html) and its visibility
// is a store field, so there is nothing left to forget.

import { appStore, setChoiceDialog } from '../../store/store.js';

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

// The open question's resolver. Module-scope because the promise
// outlives every render of the component that answers it.
let settle: ((value: string) => void) | null = null;

export function choiceDialogOpen(): boolean {
  return appStore.getState().choiceDialog !== null;
}

// resolveChoiceDialog closes the dialog and answers the promise. Called
// by the component for a button press, and by dismissChoiceDialog for
// Escape, the backdrop and every path that closes the question from
// underneath it.
export function resolveChoiceDialog(value: string): void {
  const resolve = settle;
  settle = null;
  setChoiceDialog(null);
  resolve?.(value);
}

// dismissChoiceDialog closes an open dialog as if the safe choice was
// picked, and reports whether there was one. keyboard.ts calls it so
// Escape backs out of the question rather than reaching the bindings
// underneath; the worktree browser calls it whenever the row being
// asked about may not survive the next repaint.
export function dismissChoiceDialog(): boolean {
  const entry = appStore.getState().choiceDialog;
  if (!entry) return false;
  // The FIRST choice is the safe one, so a stray key can never destroy
  // anything.
  resolveChoiceDialog(entry.spec.choices[0]?.value ?? 'cancel');
  return true;
}

export function openChoiceDialog(spec: ChoiceSpec): Promise<string> {
  return new Promise((resolve) => {
    // Only one question at a time: a second would stack invisibly and
    // leave the first one's promise pending forever.
    dismissChoiceDialog();
    settle = resolve;
    setChoiceDialog(spec);
  });
}
