// The icon sprite: the symbol set every icon <use> resolves against, and
// the one-time injection that puts it in the document.
// Spec: docs/design-docs/ui/icons.md, components.md › icon().
//
// The sprite is imported as a string (Vite's built-in `?raw`) and
// injected into <body> on first use rather than being pasted into
// index.html by a build plugin: `?raw` needs no plugin and resolves the
// same way under vitest, so DOM tests exercise the real sprite.
//
// Lives in src/lib/ rather than src/ui/ because it is data plus a DOM
// side effect, not a component: components/Icon.tsx is the only way to
// draw an icon now that the imperative primitives are gone.
import sprite from './icons.svg?raw';

const SPRITE_ID = 'hv-icon-sprite';

export const ICON_NAMES = [
  'plus',
  'minus',
  'x',
  'rotate',
  'grid',
  'single',
  'branch',
  'chevron-down',
  'chevron-right',
  'settings',
  'search',
  'help',
  'arrow-left',
  'arrow-right',
  'external',
  'download',
  'check',
  'state-running',
  'state-attention',
  'state-starting',
  'state-exited',
  'state-error',
] as const;

export type IconName = (typeof ICON_NAMES)[number];

/** Inject the sprite once. Idempotent and safe to call per icon. */
export function ensureSprite(doc: Document = document): void {
  if (doc.getElementById(SPRITE_ID)) return;
  const host = doc.createElement('div');
  host.id = SPRITE_ID;
  host.setAttribute('aria-hidden', 'true');
  // The HTML parser puts <svg> in the SVG namespace here, so <use>
  // references resolve; the host is display:none via icon.css, which
  // does NOT break <use> (the symbol only has to be in the document).
  host.innerHTML = sprite;
  doc.body.prepend(host);
}
