// Chip — minimized-session tray and minimized-project tray.
// docs/design-docs/ui/components.md › chip.
//
// A <span>, not a <button>: the chip body is one action and the restore
// control is another, and a button cannot contain a button. The trays are
// role="toolbar" divs, so a span is also the only valid child of both.
import { iconButton } from './icon-button.js';
import { stateIcon } from './icon.js';
import type { SessionState } from '../lib/session-state.js';

export interface ChipOpts {
  label: string;
  sublabel?: string;
  color?: string;
  state?: SessionState;
  active?: boolean;
  title?: string;
  ariaLabel: string;
  onClick: () => void;
  onRestore?: () => void;
  restoreLabel?: string;
}

export function chip(o: ChipOpts): HTMLSpanElement {
  const root = document.createElement('span');
  root.className = 'hv-chip';
  if (o.state) root.dataset.state = o.state;
  if (o.active) root.dataset.active = '';
  // Only set when the user actually picked a colour: the CSS falls back to
  // --fg-subtle, so an unset property is a themed default, while a literal
  // '#888' here would be an untokenised colour smuggled in from TS.
  if (o.color) root.style.setProperty('--chip-color', o.color);

  const open = document.createElement('button');
  open.type = 'button';
  open.className = 'hv-chip__open';
  open.setAttribute('aria-label', o.ariaLabel);
  open.title = o.title ?? o.ariaLabel;
  open.addEventListener('click', (e) => {
    e.stopPropagation();
    o.onClick();
  });

  // State icon when the chip stands for a session (it carries the bell for
  // a session that has no row on screen); a plain colour dot when it stands
  // for a project, whose state is the union of its sessions' and is carried
  // by the pulse on the dot instead.
  if (o.state) open.append(stateIcon(o.state));
  else {
    const dot = document.createElement('span');
    dot.className = 'hv-chip__swatch';
    open.append(dot);
  }

  const label = document.createElement('span');
  label.className = 'hv-chip__label';
  label.textContent = o.label;
  open.append(label);

  if (o.sublabel) {
    const sub = document.createElement('span');
    sub.className = 'hv-chip__sub';
    sub.textContent = o.sublabel;
    open.append(sub);
  }
  root.append(open);

  if (o.onRestore) {
    const restore = iconButton({
      icon: 'plus',
      label: o.restoreLabel ?? o.ariaLabel,
      onClick: (e) => {
        e.stopPropagation();
        o.onRestore?.();
      },
    });
    restore.classList.add('hv-chip__restore');
    root.append(restore);
  }
  return root;
}
