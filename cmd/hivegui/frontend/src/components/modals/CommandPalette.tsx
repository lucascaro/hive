// ---------- command palette ----------
//
// React port of src/app/modals/command-palette.ts. Not a `hv-dialog`
// (no ModalShell): index.html gives it `#command-palette` bare, and this
// renders its two static children — the input and the results list —
// directly, since they now live inside React's tree instead of the DOM.

import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import {
  closeCommandPalette,
  paletteCommands,
} from '../../app/modals/command-palette.js';
import { useAppStore } from '../../store/store.js';
import { Kbd } from '../Kbd.js';

export function CommandPalette({
  root,
}: {
  root: HTMLElement | null;
}): ReactNode {
  const entry = useAppStore((s) =>
    s.modals.find((m) => m.id === 'command-palette'),
  );

  // #command-palette sits outside React's tree, so its open/closed class
  // is applied here — a passive effect would paint one stale frame with
  // the backdrop up before the class caught up.
  useLayoutEffect(() => {
    root?.classList.toggle('hidden', !entry);
  }, [root, entry]);

  // The outside-click close is attached once and stays live even while
  // the palette is closed, exactly as initCommandPalette() attached it.
  // The open flag is read through a ref (same shape as Launcher's) so
  // the listener never has to be torn down and reattached per open.
  const openRef = useRef(!!entry);
  openRef.current = !!entry;
  useEffect(() => {
    if (!root) return;
    const el: HTMLElement = root;
    const onMouseDown = (e: MouseEvent) => {
      if (!openRef.current) return;
      if (!el.contains(e.target as Node)) closeCommandPalette();
    };
    document.addEventListener('mousedown', onMouseDown);
    return () => document.removeEventListener('mousedown', onMouseDown);
  }, [root]);

  if (!root) return null;
  // Rendered whether or not the palette is open: the search box and the
  // list were static children of #command-palette in index.html, and
  // a11y assertions (ux-polish.spec.ts) read the input's aria-label at
  // boot. Opening only drops the root's `hidden` class — exactly what
  // openCommandPalette() did.
  //
  // The key is the opening's generation, so a remount is what resets the
  // query and the selection — the per-open state the imperative version
  // got by clearing paletteInput.value and paletteState.selected.
  return (
    <CommandPaletteBody key={entry?.seq ?? 0} root={root} open={!!entry} />
  );
}

function CommandPaletteBody({
  root,
  open,
}: {
  root: HTMLElement;
  open: boolean;
}): ReactNode {
  const [query, setQuery] = useState('');
  const [selected, setSelected] = useState(0);
  const inputRef = useRef<HTMLInputElement | null>(null);

  // Focus only when this mount IS an opening. A passive effect, like the
  // other modals': the root's `hidden` class comes off in the parent's
  // layout effect, and focusing a display:none input does nothing.
  useEffect(() => {
    if (open) inputRef.current?.focus();
  }, [open]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    const commands = paletteCommands();
    if (!q) return commands;
    return commands.filter(
      (c) =>
        c.name.toLowerCase().includes(q) ||
        c.shortcut.toLowerCase().includes(q),
    );
  }, [query]);

  // Derived rather than chased with another effect: whatever `selected`
  // drifted to (a query narrowing the list is the only way it can),
  // this is the "reset to 0" the legacy renderPalette() did inline.
  const activeIndex = selected >= filtered.length ? 0 : selected;

  function activate(i: number) {
    const c = filtered[i];
    if (!c) return;
    closeCommandPalette();
    // Deferred: some commands open another modal that owns focus, and
    // running before the close finishes lets this one steal it right
    // back.
    setTimeout(() => c.run(), 0);
  }

  // Re-attached every render, same as Settings' Enter handler, so it
  // closes over the current filtered list and activeIndex rather than a
  // stale one from mount.
  //
  // Escape is NOT here. It belongs to keyboard.ts's ladder, which reads
  // the store rather than the focus location: this listener sits on
  // #command-palette and only fires for keys typed inside it, so once
  // anything moves focus out of the search box the palette would have no
  // way to close (that shipped as a CI failure). The ladder's handler is
  // capture-phase on window and stops propagation, so a branch here
  // could never run for Escape anyway.
  useEffect(() => {
    if (!open) return;
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'ArrowDown' || (e.key === 'Tab' && !e.shiftKey)) {
        e.preventDefault();
        e.stopPropagation();
        if (filtered.length === 0) return;
        setSelected((activeIndex + 1) % filtered.length);
        return;
      }
      if (e.key === 'ArrowUp' || (e.key === 'Tab' && e.shiftKey)) {
        e.preventDefault();
        e.stopPropagation();
        if (filtered.length === 0) return;
        setSelected((activeIndex - 1 + filtered.length) % filtered.length);
        return;
      }
      if (e.key === 'Enter') {
        e.preventDefault();
        e.stopPropagation();
        activate(activeIndex);
      }
    }
    root.addEventListener('keydown', onKeyDown);
    return () => root.removeEventListener('keydown', onKeyDown);
  });

  return (
    <>
      <input
        id="command-palette-input"
        ref={inputRef}
        type="text"
        placeholder="Search actions, shortcuts…"
        aria-label="Search commands"
        autoComplete="off"
        value={query}
        // Only the query moves. The selection is left alone and clamped
        // on render — renderPalette() reset it to 0 only when the
        // narrowed list no longer reached it, and a row the user had
        // already picked must survive a keystroke that still matches it.
        onChange={(e) => setQuery(e.target.value)}
      />
      <div id="command-palette-list" role="listbox">
        {filtered.map((c, i) => (
          // Keys for the whole list are handled on the palette root (a
          // row is never focused, the selection is a data attribute), so
          // per-row key handlers would double-fire the activation.
          // biome-ignore lint/a11y/noStaticElementInteractions: see above
          // biome-ignore lint/a11y/useKeyWithClickEvents: see above
          <div
            key={c.id}
            className="palette-item"
            data-selected={i === activeIndex ? '' : undefined}
            onMouseEnter={() => setSelected(i)}
            onClick={() => activate(i)}
          >
            <span className="palette-name">{c.name}</span>
            <span className="palette-shortcut">
              {c.shortcut ? <Kbd>{c.shortcut}</Kbd> : null}
            </span>
          </div>
        ))}
      </div>
    </>
  );
}
