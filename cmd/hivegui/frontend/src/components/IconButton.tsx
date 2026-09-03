// Icon-only button. components.md › iconButton(): 24x24 (rows/bars) or
// 22x22 (sidebar header), 14px icon, aria-label REQUIRED and mirrored
// into title. Never build an icon-only <button> by hand.
import type { MouseEvent } from 'react';
import { Icon, type IconName } from './Icon.js';

export interface IconButtonProps {
  icon: IconName;
  label: string;
  id?: string;
  onClick?: (e: MouseEvent<HTMLButtonElement>) => void;
  /** Needed where a button sits inside a surface that also claims mousedown
   *  (the terminal tile header selects the tile on mousedown). */
  onMouseDown?: (e: MouseEvent<HTMLButtonElement>) => void;
  size?: 22 | 24;
  className?: string;
  /** data-action, the hook the e2e specs and CSS select rows' controls by. */
  action?: string;
  /** For a marker that is present but not always applicable — the tile's
   *  worktree glyph on a session with no worktree. */
  hidden?: boolean;
  /** Overrides the label as the tooltip where the control needs to say
   *  more at rest than its accessible name does. */
  title?: string;
}

export function IconButton({
  icon,
  label,
  id,
  onClick,
  onMouseDown,
  size = 24,
  className,
  action,
  hidden,
  title,
}: IconButtonProps) {
  // Accessibility is not a soft requirement here: the icon carries the
  // whole meaning, so an empty label is a bug, not a default.
  if (!label.trim()) throw new Error(`IconButton(${icon}): label is required`);
  return (
    <button
      type="button"
      id={id}
      className={className ? `hv-icon-btn ${className}` : 'hv-icon-btn'}
      aria-label={label}
      title={title ?? label}
      hidden={hidden}
      data-size={size === 24 ? undefined : String(size)}
      data-action={action}
      onClick={onClick}
      onMouseDown={onMouseDown}
    >
      <Icon name={icon} />
    </button>
  );
}
