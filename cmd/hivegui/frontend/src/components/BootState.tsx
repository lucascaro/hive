// The full-pane boot overlay. Mounted on #boot-state, whose markup
// index.html still declares so the card is painted before any module
// script runs — on a cold machine the daemon can take seconds to bind
// its socket, and a black pane reads as a broken app. This component
// takes over the same ids on mount.
//
// The 5-attempt retry policy is not here: main.ts's retryBoot() owns it
// and arrives as the store's `onRetry`.
import { useLayoutEffect, useRef, type ReactNode } from 'react';
import { useAppStore } from '../store/store.js';
import { Button } from './Button.js';

export function BootState({ root }: { root: HTMLElement | null }): ReactNode {
  const view = useAppStore((s) => s.bootState);

  // The overlay is hidden by a class on the container, exactly as
  // setBootState(null) did — the container carries role="status" and
  // aria-live, which must survive being hidden.
  useLayoutEffect(() => {
    root?.classList.toggle('hidden', view === null);
  }, [root, view]);

  // Hiding the overlay left its last message in place before, and the
  // ids stayed in the document. Keeping the text lets this render the
  // same card while hidden instead of blanking it for one frame on the
  // way out.
  const last = useRef('');
  if (view) last.current = view.text;
  const onRetry = view?.onRetry ?? null;

  return (
    <div className="boot-state-card">
      {/* A card offering Retry is a card that has stopped waiting, so
          the spinner goes with it. */}
      <span
        className={onRetry ? 'phase-spinner hidden' : 'phase-spinner'}
        aria-hidden="true"
      />
      <span id="boot-state-text">{last.current}</span>
      {onRetry ? (
        // The id is what boot-overlay.spec.ts and boot-state.test.tsx
        // select on.
        <Button
          id="boot-state-retry"
          label="Retry"
          kind="primary"
          icon="rotate"
          onClick={onRetry}
        />
      ) : null}
    </div>
  );
}
