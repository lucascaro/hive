// ---------- tabs ----------
//
// The ARIA tabs pattern, once, so no surface re-rolls it. Settings is the
// first caller; anything else that splits a body into sections uses this
// rather than hand-rolling a row of buttons.
//
// docs/design-docs/ui/components.md › tabs.
//
// The panels are NOT owned here. A caller renders them itself and hides
// the inactive ones — which is what keeps their state alive across a
// switch — so this component only needs to know which id is active.
//
// Button ids are `<id>-tab-<tabId>`. That is the selector contract the
// Playwright specs key off, the same way `.settings-agent-name` is
// (FRONTEND.md › no data-testid).

import { useRef, type KeyboardEvent, type ReactNode } from 'react';

export interface TabDef<T extends string = string> {
  id: T;
  label: string;
}

export interface TabsProps<T extends string = string> {
  /** Prefix for every generated id; also the tablist's own id. */
  id: string;
  tabs: TabDef<T>[];
  active: T;
  /** Typed with the caller's own id union, so no cast at the call site. */
  onChange: (id: T) => void;
  /** Names the strip for screen readers, e.g. "Settings sections". */
  label: string;
}

export function Tabs<T extends string>({
  id,
  tabs,
  active,
  onChange,
  label,
}: TabsProps<T>): ReactNode {
  const listRef = useRef<HTMLDivElement | null>(null);

  // Roving tabindex means the newly selected tab is the only tabbable
  // one, so focus has to be moved explicitly — the browser will not do
  // it, and leaving focus on a tab that is now tabindex="-1" strands the
  // keyboard user outside the strip's own tab order.
  function select(next: T) {
    onChange(next);
    listRef.current
      ?.querySelector<HTMLElement>(`#${CSS.escape(`${id}-tab-${next}`)}`)
      ?.focus();
  }

  // Left/Right wrap, Home/End jump. Selection follows focus, which is
  // the pattern's default for panels that are already mounted: there is
  // nothing to load, so an extra Enter to activate would be friction.
  function onKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    // An `active` that is not in `tabs` would otherwise leave the arrow
    // keys dead. Treat it as "before the first tab" so the strip is still
    // navigable; a caller should clamp, but the primitive should not go
    // inert when one forgets.
    const i = Math.max(
      0,
      tabs.findIndex((t) => t.id === active),
    );
    if (tabs.length === 0) return;
    let next = -1;
    if (e.key === 'ArrowLeft') next = (i - 1 + tabs.length) % tabs.length;
    else if (e.key === 'ArrowRight') next = (i + 1) % tabs.length;
    else if (e.key === 'Home') next = 0;
    else if (e.key === 'End') next = tabs.length - 1;
    if (next < 0) return;
    // Stopped as well as prevented: Settings' root listener turns a bare
    // key into dialog-wide behaviour, and keyboard.ts's window handler
    // reads arrows as grid navigation.
    e.preventDefault();
    e.stopPropagation();
    select(tabs[next].id);
  }

  return (
    <div
      className="hv-tabs"
      id={`${id}-tabs`}
      ref={listRef}
      role="tablist"
      aria-label={label}
      onKeyDown={onKeyDown}
    >
      {tabs.map((t) => {
        const selected = t.id === active;
        return (
          <button
            key={t.id}
            type="button"
            id={`${id}-tab-${t.id}`}
            className="hv-tab"
            role="tab"
            aria-selected={selected}
            aria-controls={`${id}-panel-${t.id}`}
            data-selected={selected ? '' : undefined}
            tabIndex={selected ? 0 : -1}
            onClick={() => select(t.id)}
          >
            {t.label}
          </button>
        );
      })}
    </div>
  );
}
