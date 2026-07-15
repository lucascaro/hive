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
export function beginInlineRename({ mount, unmount, value, className, onCommit, onDone, beforeFocus }) {
  const input = document.createElement('input');
  input.type = 'text';
  input.className = className;
  input.value = value;
  mount(input);
  if (beforeFocus) beforeFocus();
  input.focus();
  input.select();
  let done = false;
  const finish = (commit) => {
    if (done) return;
    done = true;
    const next = input.value.trim();
    unmount(input);
    if (commit && next && next !== value) onCommit(next);
    if (onDone) onDone();
  };
  // Single capture-phase listener handles Enter/Escape AND shields the
  // input from xterm / global hotkey handlers. It must be a single
  // listener: stopPropagation() from a capture listener at the target
  // also cancels the target's own bubble-phase listeners (DOM dispatch
  // skips the bubble invocation once the flag is set), so a separate
  // bubble-phase Enter handler would never run.
  input.addEventListener('keydown', (e) => {
    e.stopPropagation();
    if (e.key === 'Enter') finish(true);
    else if (e.key === 'Escape') finish(false);
  }, { capture: true });
  input.addEventListener('blur', () => finish(true));
  return input;
}
