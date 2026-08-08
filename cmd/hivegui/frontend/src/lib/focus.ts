// Pure decision logic for setFocusedTile's gate.
//
// Splitting the gate out as a pure function keeps the only branchy
// piece of the focus pipeline unit-testable. The caller is still
// responsible for the DOM side-effects (sweep .term-focused, add it,
// call .focus()); this function just decides which side-effect to run.

export const ACTION_CLEAR = 'clear'; // sweep .term-focused, focus nothing
export const ACTION_PRESERVE = 'preserve'; // a real input owns the keyboard — leave focus alone, also clear .term-focused
export const ACTION_FOCUS = 'focus'; // focus the helper-textarea of `id`

// snapshot describes everything the gate needs to know:
//   - id: the requested focus target session id (state.activeId), or null
//   - modalOpen: a launcher / project-editor / palette modal is visible
//   - activeTag: document.activeElement.tagName (e.g. 'INPUT', 'BODY')
//   - activeClasses: a string or DOMTokenList containing the active element's classes
//   - knownTermIds: iterable of session ids that currently have a SessionTerm
//
// Returns { kind, id? }.
export interface FocusSnapshot {
  id?: string | null;
  modalOpen?: boolean;
  activeTag?: string;
  activeClasses?: string | { contains?: (name: string) => boolean } | null;
  knownTermIds?: { has(id: string): boolean } | string[] | null;
}

export type FocusAction =
  | { kind: typeof ACTION_CLEAR | typeof ACTION_PRESERVE }
  | { kind: typeof ACTION_FOCUS; id: string };

export function decideFocusAction(snapshot: FocusSnapshot): FocusAction {
  const { id, modalOpen, activeTag, activeClasses, knownTermIds } = snapshot;

  if (modalOpen) return { kind: ACTION_CLEAR };
  if (id == null) return { kind: ACTION_CLEAR };
  if (!hasId(knownTermIds, id)) return { kind: ACTION_CLEAR };

  // A real <input>/<textarea> owns the keyboard — for example an
  // inline rename or a launcher input that's still focused by the
  // time this gate runs. Leave focus where it is AND clear the
  // visual border, so the UI can't claim a tile is focused while
  // keystrokes go to a sibling DOM input. (contentEditable hosts
  // are not part of the v2 UI surface and are not covered here.)
  const isXtermHelper = hasClass(activeClasses, 'xterm-helper-textarea');
  if (!isXtermHelper && isRealInput(activeTag))
    return { kind: ACTION_PRESERVE };

  return { kind: ACTION_FOCUS, id };
}

function isRealInput(tag: string | undefined): boolean {
  return tag === 'INPUT' || tag === 'TEXTAREA';
}

function hasClass(
  classes: FocusSnapshot['activeClasses'],
  name: string,
): boolean {
  if (!classes) return false;
  if (typeof classes === 'string') {
    return classes.split(/\s+/).includes(name);
  }
  if (typeof classes.contains === 'function') return classes.contains(name);
  return false;
}

// Array check first so TS narrows the remaining branch to the Set-like;
// arrays have no `.has`, so the reorder is behavior-preserving.
function hasId(ids: FocusSnapshot['knownTermIds'], id: string): boolean {
  if (!ids) return false;
  if (Array.isArray(ids)) return ids.includes(id);
  if (typeof ids.has === 'function') return ids.has(id);
  return false;
}
