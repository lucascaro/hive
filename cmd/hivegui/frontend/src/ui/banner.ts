// Full-width notice row above the app grid. Owns its own markup so no
// feature module hand-writes banner DOM; `banners.ts` drives it through
// the returned handle. docs/design-docs/ui/components.md > banner.
import { button } from './button.js';
import { iconButton } from './icon-button.js';

export type BannerKind = 'error' | 'info';

export interface BannerAction {
  id: string;
  label: string;
  onClick: () => void;
}

export interface BannerOpts {
  kind: BannerKind;
  text?: string;
  actions?: BannerAction[];
  onDismiss?: () => void;
  /** Stamped onto the root; keeps existing #daemon-banner selectors alive. */
  id?: string;
}

export interface Banner {
  el: HTMLDivElement;
  setText(text: string): void;
  action(id: string): HTMLButtonElement;
  show(): void;
  hide(): void;
}

export function banner({
  kind,
  text = '',
  actions = [],
  onDismiss,
  id,
}: BannerOpts): Banner {
  const el = document.createElement('div');
  el.className = 'hv-banner';
  el.dataset.kind = kind;
  // An error banner interrupts (assertive); an info banner reports.
  el.setAttribute('role', kind === 'error' ? 'alert' : 'status');
  if (id) el.id = id;
  // `hidden`, not a .hidden class: the property is the platform's own
  // channel and CSS can't accidentally lose to a later rule.
  el.hidden = true;

  const textEl = document.createElement('span');
  textEl.className = 'hv-banner__text';
  textEl.textContent = text;
  el.append(textEl);

  const byId = new Map<string, HTMLButtonElement>();
  for (const a of actions) {
    // kind 'ghost': the banner ground already carries the emphasis;
    // a filled button inside it reads as a second alert.
    const b = button({ label: a.label, kind: 'ghost', onClick: a.onClick });
    b.classList.add('hv-banner__action');
    // The id is on the element too, so e2e and DOM tests can address a
    // specific action without depending on child order.
    b.dataset.actionId = a.id;
    byId.set(a.id, b);
    el.append(b);
  }

  if (onDismiss) {
    const d = iconButton({ icon: 'x', label: 'Dismiss', onClick: onDismiss });
    d.classList.add('hv-banner__dismiss');
    el.append(d);
  }

  return {
    el,
    setText: (t: string) => {
      textEl.textContent = t;
      // The row is a fixed 36px with nowrap + ellipsis, so a long message
      // (`Restart failed: ${err}`) is cut at the window edge. The full
      // string has to stay reachable somewhere.
      textEl.title = t;
    },
    action: (aid: string) => {
      const b = byId.get(aid);
      if (!b) throw new Error(`banner: no action "${aid}"`);
      return b;
    },
    show: () => {
      el.hidden = false;
    },
    hide: () => {
      el.hidden = true;
    },
  };
}
