// ---------- modal shell ----------
//
// The React half of what src/ui/dialog.ts used to build: same markup,
// same class names, same `hidden` contract. Phase 4 ported the last four
// imperative dialogs (worktrees, project editor, help overlay, choice
// dialog) and deleted the primitive, so this is now the only dialog
// shell in the app.
//
// The root element is NOT created here. It lives in index.html so the
// React root has something to mount on before the store says the modal
// is open, and so the id, `role` and `aria-modal` that the keyboard
// pipeline and the e2e specs key off exist from the first paint. This
// component owns everything inside it; the island that renders it owns
// the root's `hidden` class, because that class has to be right in the
// frame this component is no longer mounted in.
//
// What this does NOT own, for the same reasons dialog() does not:
//   * Where focus goes on open and close. Every modal has its own
//     answer; closing in particular must blur BEFORE the caller hands
//     focus back to the terminal, which only the close function can
//     order correctly (see closeSettings).
//   * Hiding itself. `onClose` is the module's close function, which has
//     bookkeeping — in-flight loads, drafts — that must run whichever
//     gesture closed the dialog.
//
// See docs/design-docs/ui/components.md › ModalShell.

import { useEffect, type ReactNode } from 'react';
import { trapFocus } from '../../lib/focus-trap.js';
import { IconButton } from '../IconButton.js';
import { Kbd } from '../Kbd.js';

export interface ModalHint {
  /** Rendered as-is, so it carries its own [] or () per AGENTS.md. */
  keys: string;
  label: string;
}

export interface ModalShellProps {
  id: string;
  /** The dialog root from index.html. Mounted only while open. */
  root: HTMLElement;
  title: string;
  onClose: () => void;
  size?: 'sm' | 'md' | 'lg';
  /** Confirm/cancel key hints. Every overlay must show them (AGENTS.md). */
  hints?: ModalHint[];
  /**
   * Rendered inside the <h3>, after the title. It rides there rather than
   * beside it so the accessible name stays one string ("Worktrees ·
   * hive"), which is what aria-labelledby reads.
   */
  titleSuffix?: ReactNode;
  /**
   * Default true. The choice dialog is the one caller that opts out: its
   * safe choice already covers "back out", and a second, unlabelled exit
   * next to a "delete the branch too" button is a worse affordance, not
   * a redundant one.
   */
  showCloseButton?: boolean;
  actions?: ReactNode;
  children?: ReactNode;
}

export function ModalShell({
  id,
  root,
  title,
  onClose,
  size = 'md',
  hints,
  titleSuffix,
  showCloseButton = true,
  actions,
  children,
}: ModalShellProps): ReactNode {
  // Acquired on mount, released on unmount: Escape, the backdrop, and
  // Tab containment. The trap is a fallback — keyboard.ts's window
  // listener is on the capture phase, so for a modal it knows about it
  // runs first and this never fires. It matters for the case that one
  // does not cover: focus already inside the dialog with the window
  // handler having bailed out.
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        e.preventDefault();
        e.stopPropagation();
        onClose();
        return;
      }
      if (trapFocus(root, e)) e.stopPropagation();
    }
    // Both ends of the gesture must land on the backdrop. A
    // text-selection drag that starts inside an input and releases
    // outside the panel dispatches its click on the nearest common
    // ancestor — the backdrop — so testing the click alone discards the
    // whole draft mid-edit.
    let downOnBackdrop = false;
    function onMouseDown(e: MouseEvent) {
      downOnBackdrop = e.target === root;
    }
    function onClick(e: MouseEvent) {
      const fire = downOnBackdrop && e.target === root;
      downOnBackdrop = false;
      if (fire) onClose();
    }
    root.addEventListener('keydown', onKeyDown);
    root.addEventListener('mousedown', onMouseDown);
    root.addEventListener('click', onClick);
    return () => {
      root.removeEventListener('keydown', onKeyDown);
      root.removeEventListener('mousedown', onMouseDown);
      root.removeEventListener('click', onClick);
    };
  }, [root, onClose]);

  const hintList = hints ?? [];
  return (
    <div className="hv-dialog__panel" id={`${id}-panel`} data-size={size}>
      <header className="hv-dialog__header">
        {/* The accessible name is this one string: the root points at it
            with aria-labelledby. */}
        <h3 className="hv-dialog__title" id={`${id}-title`}>
          {title}
          <span className="hv-dialog__title-suffix">{titleSuffix}</span>
        </h3>
        {showCloseButton ? (
          <IconButton
            icon="x"
            label="Close"
            className="hv-dialog__close"
            id={`${id}-close`}
            onClick={onClose}
          />
        ) : null}
      </header>
      <div className="hv-dialog__body">{children}</div>
      <footer className="hv-dialog__footer">
        {hintList.length ? (
          <div className="hv-dialog__hints">
            {hintList.map((h) => (
              <span key={h.keys} className="hv-dialog__hint">
                <Kbd>{h.keys}</Kbd> {h.label}
              </span>
            ))}
          </div>
        ) : null}
        {actions ? <div className="hv-dialog__actions">{actions}</div> : null}
      </footer>
    </div>
  );
}
