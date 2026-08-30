// Icon-only button. components.md > iconButton(): 24x24 (rows/bars) or
// 22x22 (sidebar header), 14px icon, aria-label REQUIRED and mirrored
// into title. Never build an icon-only <button> by hand.
import { icon, type IconName } from './icon.js';

export interface IconButtonOpts {
  icon: IconName;
  label: string;
  onClick?: (e: MouseEvent) => void;
  size?: 22 | 24;
  className?: string;
}

export function iconButton({
  icon: name,
  label,
  onClick,
  size = 24,
  className,
}: IconButtonOpts): HTMLButtonElement {
  // Accessibility is not a soft requirement here: the icon carries the
  // whole meaning, so an empty label is a bug, not a default.
  if (!label.trim()) throw new Error(`iconButton(${name}): label is required`);
  const btn = document.createElement('button');
  btn.type = 'button';
  btn.className = className ? `hv-icon-btn ${className}` : 'hv-icon-btn';
  btn.setAttribute('aria-label', label);
  btn.title = label;
  if (size !== 24) btn.dataset.size = String(size);
  btn.appendChild(icon(name));
  if (onClick) btn.addEventListener('click', onClick);
  return btn;
}
