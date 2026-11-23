// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

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
  // The FIRST choice is treated as the safe one: it is what Escape and
  // a scrim click resolve to.
  choices: Choice[];
  // The choice Enter answers: it takes focus when the dialog opens, and
  // the footer's `[enter]` hint names it. Defaults to choices[0].value.
  //
  // A user-raised confirmation sets it to its affirmative answer, so the
  // `[enter]` key on screen does what the user asked for (AGENTS.md:
  // Enter confirms, Esc cancels). With several affirmatives, pick the
  // least destructive one. A dialog that raises itself unprompted —
  // possibly while the user is typing — leaves it at a choice that
  // destroys nothing, so a stray Enter cannot lose work.
  defaultValue?: string;
  // What a dismissal resolves to, when the first choice is the wrong
  // answer to give on the user's behalf. Defaults to choices[0].value.
  //
  // dismissChoiceDialog() is called by unrelated code on paths that
  // have nothing to do with the question — every worktree:list repaint,
  // closing the worktree browser or the idea inbox, any other dialog
  // opening. For a question whose safe-looking first choice is
  // destructive (the parked-worktree prompt's Cancel discards a
  // worktree and deletes the session) that turns an unrelated click
  // into a silent, irreversible answer. Such a caller passes '' here
  // and treats it as "not answered — ask again".
  dismissValue?: string;
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
  enterTarget = null;
  setChoiceDialog(null);
  resolve?.(value);
}

// dismissChoiceDialog closes an open dialog as if the safe choice (not
// Enter's default) was picked, and reports whether there was one. Its key scope runs it so
// Escape backs out of the question rather than reaching the bindings
// underneath; the worktree browser calls it whenever the row being
// asked about may not survive the next repaint.
export function dismissChoiceDialog(): boolean {
  const entry = appStore.getState().choiceDialog;
  if (!entry) return false;
  // The FIRST choice is the safe one, so a stray key can never destroy
  // anything — unless the caller said otherwise, because for its
  // question the first choice is not safe to give unprompted.
  resolveChoiceDialog(
    entry.spec.dismissValue ?? entry.spec.choices[0]?.value ?? 'cancel',
  );
  return true;
}

// The choice Enter answers, once keyboard focus has moved off the
// default. ChoiceDialog.tsx sets it from the same focusin that relabels
// the `[enter]` hint, so the hint and the key read one value and cannot
// disagree. Not read from document.activeElement at keydown: a pointer
// press dragged off its button leaves that button focused without
// relabelling the hint, and Chromium turns it :focus-visible on the
// very keydown that would check it.
let enterTarget: string | null = null;

export function setChoiceEnterTarget(value: string): void {
  enterTarget = value;
}

// confirmChoiceDialog is Enter: the choice the `[enter]` hint names —
// the keyboard-focused one, else the spec's default. Bound in
// key-scopes.ts rather than left to the button's native activation so
// it holds wherever focus sits.
export function confirmChoiceDialog(): boolean {
  const entry = appStore.getState().choiceDialog;
  if (!entry) return false;
  resolveChoiceDialog(
    enterTarget ?? defaultChoice(entry.spec)?.value ?? 'cancel',
  );
  return true;
}

// focusVisible reports keyboard focus. An engine without :focus-visible
// (older jsdom) throws on the selector; there every focus counts.
export function focusVisible(el: Element | null | undefined): boolean {
  try {
    return !!el?.matches(':focus-visible');
  } catch {
    return true;
  }
}

// defaultChoice is the choice Enter answers until focus moves.
export function defaultChoice(spec: ChoiceSpec): Choice | undefined {
  return (
    spec.choices.find((c) => c.value === spec.defaultValue) ?? spec.choices[0]
  );
}

export function openChoiceDialog(spec: ChoiceSpec): Promise<string> {
  return new Promise((resolve) => {
    // Only one question at a time: a second would stack invisibly and
    // leave the first one's promise pending forever.
    dismissChoiceDialog();
    settle = resolve;
    enterTarget = null;
    setChoiceDialog(spec);
  });
}
