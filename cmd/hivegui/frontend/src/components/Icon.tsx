// The icon primitive. One sprite, one <use>, colour from the parent's
// `color` — docs/design-docs/ui/icons.md, components.md › icon().
// Feature components never write SVG or a Unicode glyph.
//
// The sprite injection itself lives in lib/icon-sprite.ts and is called
// here on every render, exactly as the imperative icon() used to call it
// on every construction. This component is now the only way to draw an
// icon: src/ui/ is gone.
import { ensureSprite, type IconName } from '../lib/icon-sprite.js';
import { STATE_WORDS, type SessionState } from '../lib/session-state.js';

export type { IconName };

export function Icon({ name, size = 14 }: { name: IconName; size?: 12 | 14 }) {
  ensureSprite();
  return (
    <svg
      className="hv-icon"
      width={size}
      height={size}
      viewBox="0 0 24 24"
      aria-hidden="true"
      focusable="false"
      data-size={size === 14 ? undefined : String(size)}
    >
      <use href={`#hv-${name}`} />
    </svg>
  );
}

// State icons (components.md › stateIcon). data-state drives colour and
// animation from icon.css; the <title> child is the "words" channel
// required by README principle 5.
export function StateIcon({
  state,
  className,
}: {
  state: SessionState;
  className?: string;
}) {
  ensureSprite();
  return (
    <svg
      className={
        className
          ? `hv-icon hv-state-icon ${className}`
          : 'hv-icon hv-state-icon'
      }
      width={14}
      height={14}
      viewBox="0 0 24 24"
      role="img"
      focusable="false"
      data-state={state}
    >
      <title>{STATE_WORDS[state]}</title>
      <use href={`#hv-state-${state}`} />
    </svg>
  );
}
