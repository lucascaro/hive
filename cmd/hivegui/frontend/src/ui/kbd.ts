// The only way to render a key hint. patterns.md > Keyboard hints:
// mono, --text-xs, --fg-subtle, no border, no fill; format ([1] for
// digits/symbols, (n) for letters) is the caller's, per AGENTS.md.
export function kbd(text: string): HTMLElement {
  const el = document.createElement('kbd');
  el.className = 'hv-kbd';
  el.textContent = text;
  return el;
}
