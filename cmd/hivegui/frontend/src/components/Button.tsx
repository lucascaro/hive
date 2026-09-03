// The only way a feature component makes a labelled button (it began as
// a port of the deleted src/ui/button.ts). Kinds are data attributes so a
// variant never needs a second class and CSS can select on [data-kind].
// docs/design-docs/ui/components.md › button.
import type { MouseEvent, ReactNode } from 'react';
import { Icon, type IconName } from './Icon.js';

export type ButtonKind = 'default' | 'primary' | 'danger' | 'ghost';

export interface ButtonProps {
  label: string;
  id?: string;
  kind?: ButtonKind;
  icon?: IconName;
  onClick?: (e: MouseEvent<HTMLButtonElement>) => void;
  className?: string;
  disabled?: boolean;
  hidden?: boolean;
  /** Already-prefixed extra attributes, e.g. `{'data-action-id': 'undo'}`. */
  extra?: Record<string, string | undefined>;
}

export function Button({
  label,
  id,
  kind = 'default',
  icon,
  onClick,
  className,
  disabled,
  hidden,
  extra,
}: ButtonProps): ReactNode {
  return (
    <button
      type="button"
      id={id}
      className={className ? `hv-button ${className}` : 'hv-button'}
      data-kind={kind}
      disabled={disabled}
      hidden={hidden}
      onClick={onClick}
      {...extra}
    >
      {icon ? <Icon name={icon} size={14} /> : null}
      {/* The label lives in its own span so the icon can never be
          squeezed by text-overflow, and so CSS can target the text
          alone. */}
      <span className="hv-button__label">{label}</span>
    </button>
  );
}
