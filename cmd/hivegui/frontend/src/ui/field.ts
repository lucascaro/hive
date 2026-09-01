// ---------- form fields ----------
//
// Label-above-control pairs for the two forms this app has (project
// editor, settings) plus the error slot they share. See
// docs/design-docs/ui/components.md > Form fields.
//
// The label WRAPS its control rather than pointing at it with `for`.
// Both are correct HTML; wrapping means no id has to be minted for a
// control that is otherwise anonymous, and settings' agent rows are
// rebuilt on every render - ids there would either collide or need a
// counter.

export function field(
  label: string,
  control: HTMLElement,
  hint?: string,
): HTMLLabelElement {
  const l = document.createElement('label');
  l.className = 'hv-field';
  const span = document.createElement('span');
  span.className = 'hv-field__label';
  span.textContent = label;
  l.append(span, control);
  if (hint) {
    const h = document.createElement('span');
    h.className = 'hv-field__hint';
    h.textContent = hint;
    l.append(h);
  }
  return l;
}

function applyCommon(
  el: HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement,
  o: { ariaLabel?: string; className?: string; id?: string },
) {
  el.classList.add('hv-input');
  if (o.className)
    el.classList.add(...o.className.split(/\s+/).filter(Boolean));
  if (o.ariaLabel) el.setAttribute('aria-label', o.ariaLabel);
  if (o.id) el.id = o.id;
}

export function textInput(o: {
  value?: string;
  placeholder?: string;
  ariaLabel?: string;
  className?: string;
  id?: string;
  onInput?: (v: string) => void;
}): HTMLInputElement {
  const el = document.createElement('input');
  el.type = 'text';
  el.autocomplete = 'off';
  el.value = o.value ?? '';
  if (o.placeholder) el.placeholder = o.placeholder;
  applyCommon(el, o);
  if (o.onInput) el.addEventListener('input', () => o.onInput?.(el.value));
  return el;
}

export function selectInput(o: {
  options: { value: string; label: string }[];
  value?: string;
  ariaLabel?: string;
  className?: string;
  id?: string;
  onChange?: (v: string) => void;
}): HTMLSelectElement {
  const el = document.createElement('select');
  for (const opt of o.options) {
    const node = document.createElement('option');
    node.value = opt.value;
    node.textContent = opt.label;
    el.append(node);
  }
  if (o.value != null) el.value = o.value;
  applyCommon(el, o);
  if (o.onChange) el.addEventListener('change', () => o.onChange?.(el.value));
  return el;
}

export function textareaInput(o: {
  value?: string;
  placeholder?: string;
  rows?: number;
  ariaLabel?: string;
  className?: string;
  id?: string;
  onInput?: (v: string) => void;
}): HTMLTextAreaElement {
  const el = document.createElement('textarea');
  el.rows = o.rows ?? 4;
  el.spellcheck = false;
  el.value = o.value ?? '';
  if (o.placeholder) el.placeholder = o.placeholder;
  applyCommon(el, o);
  el.classList.add('hv-input--mono');
  if (o.onInput) el.addEventListener('input', () => o.onInput?.(el.value));
  return el;
}

// colorInput keeps the OS colour picker - there is no reason to build
// one - and hides its inconsistent native chrome behind a fixed-size
// swatch. The chosen colour is published as --swatch so the wrapper can
// paint itself from CSS instead of from a second style write per event.
export function colorInput(o: {
  value: string;
  ariaLabel: string;
  onInput?: (v: string) => void;
}): { el: HTMLElement; input: HTMLInputElement } {
  const el = document.createElement('span');
  el.className = 'hv-swatch';
  const input = document.createElement('input');
  input.type = 'color';
  input.value = o.value;
  input.setAttribute('aria-label', o.ariaLabel);
  el.style.setProperty('--swatch', o.value);
  input.addEventListener('input', () => {
    el.style.setProperty('--swatch', input.value);
    o.onInput?.(input.value);
  });
  el.append(input);
  return { el, input };
}

// errorSlot is the "errors that block a dialog go under the field that
// caused them" half of patterns.md > Errors. The other half is
// flashStatus(); a message must never go to both.
export function errorSlot(id?: string): {
  el: HTMLElement;
  show(msg: string): void;
  clear(): void;
} {
  const el = document.createElement('p');
  el.className = 'hv-field-error hidden';
  el.setAttribute('role', 'alert');
  if (id) el.id = id;
  return {
    el,
    show(msg: string) {
      // role="alert" re-announces on every write, so an unchanged
      // message must not be written again — a per-keystroke validator
      // would otherwise talk over the user for the whole line.
      if (el.textContent === msg) return;
      el.textContent = msg;
      el.classList.toggle('hidden', !msg);
    },
    clear() {
      el.textContent = '';
      el.classList.add('hidden');
    },
  };
}
