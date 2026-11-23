// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// ---------- choice dialog ----------
//
// React port of src/app/modals/choice-dialog.ts. That module still owns
// the promise every caller awaits (openChoiceDialog); this renders the
// question and reports the answer back through resolveChoiceDialog —
// the only path that may settle it, so Escape and the backdrop resolve
// through ModalShell's onClose rather than hiding the dialog themselves.

import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import {
  defaultChoice,
  focusVisible,
  resolveChoiceDialog,
  setChoiceEnterTarget,
  type ChoiceSpec,
} from '../../app/modals/choice-dialog.js';
import { useAppStore } from '../../store/store.js';
import { Button } from '../Button.js';
import { ModalShell } from './ModalShell.js';

export function ChoiceDialog({
  root,
}: {
  root: HTMLElement | null;
}): ReactNode {
  const entry = useAppStore((s) => s.choiceDialog);

  // #choice-dialog sits outside React's tree, so its open/closed classes
  // are applied here — see Settings.tsx's identical layout effect.
  //
  // Two classes, not one. `.choice-dialog` is the z-index deviation that
  // puts this dialog over the modal that asked the question, and it is
  // also what the e2e specs count to assert the question is gone: before
  // Phase 4 the element itself was built per question and removed on the
  // answer, so "no .choice-dialog in the DOM" meant "nothing is being
  // asked". The root is static now; the class carries that meaning.
  useLayoutEffect(() => {
    root?.classList.toggle('hidden', !entry);
    root?.classList.toggle('choice-dialog', !!entry);
  }, [root, entry]);

  if (!entry || !root) return null;
  // Remounted per question, which is what resets focus-restore capture
  // for a re-ask.
  return <ChoiceDialogBody key={entry.seq} root={root} spec={entry.spec} />;
}

function ChoiceDialogBody({
  root,
  spec,
}: {
  root: HTMLElement;
  spec: ChoiceSpec;
}): ReactNode {
  // What the scrim / close affordance answers with. A spec with no
  // choices is a caller bug, but it must not become a dialog nothing
  // can close: dismissChoiceDialog() falls back to the same literal,
  // so Escape still settles the promise. `dismissValue` lets a caller
  // whose first choice is destructive opt out of answering on a
  // dismissal (see ChoiceSpec) — kept in step with dismissChoiceDialog
  // so the scrim and Escape cannot disagree.
  const safeValue = spec.dismissValue ?? spec.choices[0]?.value ?? 'cancel';

  // Whatever had focus when the question was asked. Restored on
  // unmount, but only if it is still on the page: the button that
  // raised the question is often the one the answer removes (deleting a
  // worktree takes its row with it), and focusing a detached node
  // strands the keyboard.
  const openerRef = useRef<HTMLElement | null>(null);
  useLayoutEffect(() => {
    openerRef.current = document.activeElement as HTMLElement | null;
    return () => {
      if (openerRef.current?.isConnected) openerRef.current.focus();
    };
  }, []);

  // The default choice holds focus, so Enter answers what the footer's
  // `[enter]` hint names (see ChoiceSpec.defaultValue). Button is a plain
  // function component (no ref forwarding), so the button is found the
  // same way the shell finds anything outside React's tree: by the
  // id/data contract, off the root.
  //
  // A PASSIVE effect on purpose. The root's `hidden` class is dropped by
  // the parent modal's layout effect, and layout effects run child
  // first — so focusing from one here would run while the dialog was
  // still display:none, which the browser ignores outright, leaving
  // focus (and Tab) on the modal underneath.
  const initial = defaultChoice(spec);
  useEffect(() => {
    root
      .querySelector<HTMLButtonElement>(
        `.hv-dialog__actions button[data-choice="${CSS.escape(initial?.value ?? '')}"]`,
      )
      ?.focus();
  }, [root, initial]);

  // The hint names the choice Enter will answer, which is the focused
  // one once Tab moves it (confirmChoiceDialog), so it has to follow
  // focus to stay true.
  const [enterLabel, setEnterLabel] = useState(initial?.label ?? 'cancel');
  useEffect(() => {
    const onFocusIn = (e: FocusEvent) => {
      const el = e.target as HTMLElement | null;
      // Keyboard focus only. A mouse press focuses its button on
      // mousedown; re-labelling then reflows the footer, the button
      // moves out from under the pointer before mouseup, and the click
      // never fires. A pointer user is not reading the Enter hint.
      if (!focusVisible(el)) return;
      const value = el?.dataset?.choice;
      const c = spec.choices.find((x) => x.value === value);
      if (!c) return;
      setEnterLabel(c.label);
      setChoiceEnterTarget(c.value);
    };
    root.addEventListener('focusin', onFocusIn);
    return () => root.removeEventListener('focusin', onFocusIn);
  }, [root, spec]);
  // Escape answers safeValue. Name that choice; a dismissValue that is
  // no choice at all (the parked-worktree prompt's '') leaves the
  // question unanswered, which is "close", not "cancel".
  const escLabel =
    spec.choices.find((c) => c.value === safeValue)?.label ?? 'Close';

  return (
    <ModalShell
      id="choice-dialog"
      root={root}
      size="sm"
      title={spec.title}
      showCloseButton={false}
      // Escape and a backdrop click resolve to the FIRST choice, so a
      // stray key can never destroy anything — unless the spec set
      // dismissValue, for a question whose first choice is itself
      // destructive (see ChoiceSpec). safeValue already accounts for
      // both, so this stays a single path.
      onClose={() => resolveChoiceDialog(safeValue)}
      // Every overlay shows its confirm/cancel keys (AGENTS.md), named
      // by what they actually answer: Enter the focused choice (the
      // default until Tab moves it), Escape the safe one.
      hints={[
        // Labels as written: they can carry branch and ref names.
        ...(spec.enterHint === false
          ? []
          : [{ keys: '[enter]', label: enterLabel }]),
        { keys: '[esc]', label: escLabel },
      ]}
      actions={spec.choices.map((c) => (
        <Button
          key={c.value}
          label={c.label}
          kind={c.danger ? 'danger' : 'default'}
          className={c.danger ? 'danger' : undefined}
          extra={{ 'data-choice': c.value }}
          onClick={() => resolveChoiceDialog(c.value)}
        />
      ))}
    >
      {spec.detail ? (
        <p className="choice-dialog-detail">{spec.detail}</p>
      ) : null}
      {spec.bullets?.length ? (
        <ul className="choice-dialog-bullets">
          {spec.bullets.map((b) => (
            <li key={b}>{b}</li>
          ))}
        </ul>
      ) : null}
      {spec.note ? <p className="choice-dialog-note">{spec.note}</p> : null}
    </ModalShell>
  );
}
