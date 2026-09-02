// The icon primitive. One sprite, one <use>, colour from the parent's
// `color`. Feature modules never write SVG or a Unicode glyph.
// Spec: docs/design-docs/ui/icons.md, components.md > icon().
//
// The sprite is imported as a string (Vite's built-in `?raw`) and
// injected into <body> on first use rather than being pasted into
// index.html by a build plugin: `?raw` needs no plugin and resolves the
// same way under vitest, so DOM tests exercise the real sprite.
import sprite from './icons.svg?raw';
import { STATE_WORDS, type SessionState } from '../lib/session-state.js';

const SVG_NS = 'http://www.w3.org/2000/svg';
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

export function icon(
  name: IconName,
  { size = 14 }: { size?: 12 | 14 } = {},
): SVGSVGElement {
  ensureSprite();
  const svg = document.createElementNS(SVG_NS, 'svg');
  // SVGElement.className is a read-only SVGAnimatedString: setAttribute
  // is the only way to set a class on an SVG element.
  svg.setAttribute('class', 'hv-icon');
  svg.setAttribute('width', String(size));
  svg.setAttribute('height', String(size));
  svg.setAttribute('viewBox', '0 0 24 24');
  svg.setAttribute('aria-hidden', 'true');
  svg.setAttribute('focusable', 'false');
  if (size !== 14) svg.dataset.size = String(size);
  const use = document.createElementNS(SVG_NS, 'use');
  use.setAttribute('href', `#hv-${name}`);
  svg.appendChild(use);
  return svg;
}

// State icons (components.md > stateIcon). Used by the minimized-session
// chip and the grid tile header - nowhere else. (The sidebar row's state
// icon is components/Icon.tsx's StateIcon, which renders the same markup.) data-state
// drives colour and animation from icon.css; the <title> child is the
// "words" channel required by README principle 5.
export function stateIcon(state: SessionState): SVGSVGElement {
  const el = icon(`state-${state}` as IconName);
  el.setAttribute('class', 'hv-icon hv-state-icon');
  el.setAttribute('role', 'img');
  el.removeAttribute('aria-hidden');
  el.dataset.state = state;
  const title = document.createElementNS(SVG_NS, 'title');
  title.textContent = STATE_WORDS[state];
  el.prepend(title);
  return el;
}

export function updateStateIcon(el: SVGSVGElement, state: SessionState): void {
  if (el.dataset.state === state) return;
  el.dataset.state = state;
  el.querySelector('use')?.setAttribute('href', `#hv-state-${state}`);
  const title = el.querySelector('title');
  if (title) title.textContent = STATE_WORDS[state];
}
