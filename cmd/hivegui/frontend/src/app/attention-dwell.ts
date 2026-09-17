// A Pi session you are looking at stops asking for attention after a
// few seconds of looking (spec 423). Only the status clears: the
// question itself stays on screen, so nothing is lost.
//
// Pi only, deliberately. Everywhere else a keystroke, a click on the
// active tile or a switch is what answers attention (see noteUserInput)
// — a focused window can sit untouched while nobody is in front of it.

export const ATTENTION_DWELL_MS = 3000;

export interface AttentionDwellDeps {
  delayMs: number;
  activeId: () => string | null;
  hasFocus: () => boolean;
  /** Whether this session wants the user AND dwell applies to it. */
  eligible: (id: string) => boolean;
  clear: (id: string) => void;
  setTimer: (fn: () => void, ms: number) => unknown;
  clearTimer: (handle: unknown) => void;
}

export function createAttentionDwell(deps: AttentionDwellDeps) {
  let armedId: string | null = null;
  let handle: unknown = null;

  const holds = (): string | null => {
    const id = deps.activeId();
    return id && deps.hasFocus() && deps.eligible(id) ? id : null;
  };

  const cancel = () => {
    if (handle !== null) deps.clearTimer(handle);
    handle = null;
    armedId = null;
  };

  // poke re-evaluates after anything that could change the answer: a
  // state or attention event, a switch, the window gaining focus. Cheap
  // when nothing wants attention, so callers need not filter. An armed
  // timer for the same session is kept — re-renders poke constantly and
  // would otherwise postpone the clear forever — but one whose
  // conditions stopped holding is dropped, so an answer followed by a
  // new request waits the full delay again.
  const poke = () => {
    const id = holds();
    if (id === armedId && handle !== null) return;
    cancel();
    if (!id) return;
    armedId = id;
    handle = deps.setTimer(() => {
      handle = null;
      armedId = null;
      if (holds() === id) deps.clear(id);
    }, deps.delayMs);
  };

  return { poke, cancel };
}
