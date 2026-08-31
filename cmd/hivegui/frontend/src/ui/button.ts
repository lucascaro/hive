// The only way a feature module makes a labelled button. Kinds are
// data attributes so a variant never needs a second class and CSS can
// select on [data-kind]. docs/design-docs/ui/components.md > button.
import { icon, type IconName } from './icon.js';

export type ButtonKind = 'default' | 'primary' | 'danger' | 'ghost';

export interface ButtonOpts {
  label: string;
  kind?: ButtonKind;
  icon?: IconName;
  onClick?: (e: MouseEvent) => void;
}

export function button({
  label,
  kind = 'default',
  icon: iconName,
  onClick,
}: ButtonOpts): HTMLButtonElement {
  const el = document.createElement('button');
  el.type = 'button';
  el.className = 'hv-button';
  el.dataset.kind = kind;
  if (iconName) el.append(icon(iconName, { size: 14 }));
  // The label lives in its own span so the icon can never be squeezed
  // by text-overflow, and so CSS can target the text alone.
  const text = document.createElement('span');
  text.className = 'hv-button__label';
  text.textContent = label;
  el.append(text);
  if (onClick) el.addEventListener('click', onClick);
  return el;
}
