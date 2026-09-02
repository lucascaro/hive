// Icon-only button. components.md › iconButton(): 24x24 (rows/bars) or
// 22x22 (sidebar header), 14px icon, aria-label REQUIRED and mirrored
// into title. Never build an icon-only <button> by hand.
import type { MouseEvent, ReactNode } from 'react';
import { Icon, type IconName } from './Icon.js';

export interface IconButtonProps {
  icon: IconName;
  label: string;
  onClick?: (e: MouseEvent<HTMLButtonElement>) => void;
  size?: 22 | 24;
  className?: string;
  /** data-action, the hook the e2e specs and CSS select rows' controls by. */
  action?: string;
  children?: ReactNode;
}

export function IconButton({
  icon,
  label,
  onClick,
  size = 24,
  className,
  action,
}: IconButtonProps) {
  // Accessibility is not a soft requirement here: the icon carries the
  // whole meaning, so an empty label is a bug, not a default.
  if (!label.trim()) throw new Error(`IconButton(${icon}): label is required`);
  return (
    <button
      type="button"
      className={className ? `hv-icon-btn ${className}` : 'hv-icon-btn'}
      aria-label={label}
      title={label}
      data-size={size === 24 ? undefined : String(size)}
      data-action={action}
      onClick={onClick}
    >
      <Icon name={icon} />
    </button>
  );
}
