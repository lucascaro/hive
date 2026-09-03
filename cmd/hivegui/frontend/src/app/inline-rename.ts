// Shared inline-rename control flow. SessionTerm's tile-name rename
// and the sidebar's session/project renames all did the same dance:
// drop an <input> in place of the label, focus+select it, commit on
// Enter/blur, cancel on Escape, clean up. Only how the input mounts/
// unmounts, its starting value, and what "commit" calls differ — those
// are the parameters here.

// beginInlineRename creates the <input>, wires it up, and returns it
// (callers that need a reentrancy guard, e.g. SessionTerm, can stash
// the returned element and check it before calling again).
//
//   mount(input)    — attach the input to the DOM (replace the label,
//                      insertBefore + hide the label, etc.)
//   unmount(input)  — detach the input and restore the label
//   value           — starting text
//   className       — input's CSS class
//   onCommit(next)  — called with the trimmed value when the user
//                      commits a real change (Enter, or blur with a
//                      non-empty, changed value)
//   onDone()        — called after every finish, commit or cancel
//   beforeFocus()   — optional, run after mount but before focus/select
export interface InlineRenameOpts {
  mount: (input: HTMLInputElement) => void;
  unmount: (input: HTMLInputElement) => void;
  value: string;
  className: string;
  onCommit: (next: string) => void;
  onDone?: () => void;
  beforeFocus?: () => void;
}

// The rename currently on screen, if any. An inline editor owns the
// keyboard while it is open — Escape cancels the edit, it does not
// close whatever is behind it — but keyboard.ts listens in the CAPTURE
// phase, so it sees every key before the input does and stopPropagation
// from the input cannot win the race. keyboard.ts therefore asks here
// first. (Sidebar renames never hit this because nothing global claims
// bare Escape at that level; a rename inside a modal does.)
let active: { input: HTMLInputElement; cancel: () => void } | null = null;

export function inlineRenameActive(): boolean {
  return active !== null;
}

// cancelInlineRenameFor aborts the rename owned by `input`, and only
// that one. React callers need the identity check: their cleanup runs
// after the rename may already have finished (and after an unrelated one
// may have started), and cancelling someone else's editor would discard
// an edit the user is still typing.
export function cancelInlineRenameFor(input: HTMLInputElement): boolean {
  if (active?.input !== input) return false;
  active.cancel();
  return true;
}

// cancelInlineRename aborts the open rename and reports whether there
// was one.
export function cancelInlineRename(): boolean {
  if (!active) return false;
  active.cancel();
  return true;
}

export function beginInlineRename({
  mount,
  unmount,
  value,
  className,
  onCommit,
  onDone,
  beforeFocus,
}: InlineRenameOpts): HTMLInputElement {
  // Whatever had focus when the edit began — usually the button or row
  // the user activated. An edit is a detour: finishing it should put
  // them back, not drop focus on <body> for the surrounding modal's
  // trap to grab at random on the next key.
  const opener = document.activeElement as HTMLElement | null;

  const input = document.createElement('input');
  input.type = 'text';
  input.className = className;
  input.value = value;
  mount(input);
  if (beforeFocus) beforeFocus();
  input.focus();
  input.select();
  let done = false;
  const finish = (commit: boolean) => {
    if (done) return;
    done = true;
    if (active?.input === input) active = null;
    const next = input.value.trim();
    unmount(input);
    // Restore before onCommit/onDone, never after: callers that want
    // focus somewhere specific say so there (the sidebar sends it back
    // to the active terminal), and their choice must win. Skipped when
    // the opener did not survive unmount — a rebuilt row takes its
    // buttons with it.
    if (opener?.isConnected) opener.focus();
    if (commit && next && next !== value) onCommit(next);
    if (onDone) onDone();
  };
  active = { input, cancel: () => finish(false) };
  // Single capture-phase listener handles Enter/Escape AND shields the
  // input from xterm / global hotkey handlers. It must be a single
  // listener: stopPropagation() from a capture listener at the target
  // also cancels the target's own bubble-phase listeners (DOM dispatch
  // skips the bubble invocation once the flag is set), so a separate
  // bubble-phase Enter handler would never run.
  input.addEventListener(
    'keydown',
    (e) => {
      e.stopPropagation();
      if (e.key === 'Enter') finish(true);
      else if (e.key === 'Escape') finish(false);
    },
    { capture: true },
  );
  input.addEventListener('blur', () => finish(true));
  return input;
}
