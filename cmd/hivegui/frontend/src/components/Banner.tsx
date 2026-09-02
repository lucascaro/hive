// Full-width notice row above the app grid. React port of
// src/ui/banner.ts — same markup, same classes, same `hidden` property.
// docs/design-docs/ui/components.md › banner.
//
// `hidden`, not a .hidden class: the property is the platform's own
// channel and CSS can't accidentally lose to a later rule. (banner.css
// still carries `.hv-banner[hidden] { display: none }` because
// .hv-button's author-origin `display: inline-flex` outranks the UA
// rule — see test/e2e/banner-visibility.spec.ts.)
import type { ReactNode } from 'react';
import type { BannerActionData, BannerData } from '../store/store.js';
import { Button } from './Button.js';
import { IconButton } from './IconButton.js';

export type BannerKind = 'error' | 'info';

/** The static half of a banner: what never changes at runtime. */
export interface BannerActionDef {
  id: string;
  label: string;
  onClick: () => void;
}

export interface BannerProps {
  slot: string;
  kind: BannerKind;
  /** Stamped onto the root; keeps the #daemon-banner selectors alive. */
  id?: string;
  actions?: BannerActionDef[];
  onDismiss?: () => void;
  data: BannerData;
}

// dataset keys are camelCase (`el.dataset.daemonBuild`); the attribute
// they write is kebab-cased. The store keeps the dataset spelling that
// the imperative code used, so the conversion happens once, here.
function dataAttrs(
  rec: Record<string, string> | undefined,
): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(rec ?? {})) {
    out[`data-${k.replace(/[A-Z]/g, (c) => `-${c.toLowerCase()}`)}`] = v;
  }
  return out;
}

export function Banner({
  slot,
  kind,
  id,
  actions = [],
  onDismiss,
  data,
}: BannerProps): ReactNode {
  return (
    <div
      className="hv-banner"
      id={id}
      // An error banner interrupts (assertive); an info banner reports.
      role={kind === 'error' ? 'alert' : 'status'}
      data-kind={kind}
      data-slot={slot}
      hidden={!data.visible}
      {...dataAttrs(data.data)}
    >
      {/* The row is a fixed 36px with nowrap + ellipsis, so a long
          message (`Restart failed: ${err}`) is cut at the window edge.
          The title keeps the full string reachable. */}
      <span className="hv-banner__text" title={data.text || undefined}>
        {data.text}
      </span>
      {actions.map((a) => {
        const o: BannerActionData = data.actions?.[a.id] ?? {};
        return (
          <Button
            key={a.id}
            // kind 'ghost': the banner ground already carries the
            // emphasis; a filled button inside it reads as a second alert.
            kind="ghost"
            className="hv-banner__action"
            label={o.label ?? a.label}
            hidden={o.hidden}
            disabled={o.disabled}
            onClick={a.onClick}
            // The id is on the element too, so e2e and DOM tests can
            // address a specific action without depending on child order.
            extra={{ 'data-action-id': a.id, ...dataAttrs(o.data) }}
          />
        );
      })}
      {onDismiss ? (
        <IconButton
          icon="x"
          label="Dismiss"
          className="hv-banner__dismiss"
          onClick={onDismiss}
        />
      ) : null}
    </div>
  );
}
