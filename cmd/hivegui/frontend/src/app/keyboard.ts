// ---------- keyboard: the capture-phase dispatcher ----------
//
// One window keydown listener, registered capture-phase so it sees a key
// before any element does (an inline rename's stopPropagation cannot beat
// it). It decides nothing itself: it walks KEY_SCOPES (app/key-scopes.ts),
// the single ordered list of who owns a key, and runs the matched
// binding's command through the command bus (app/command-registry.ts).
//
// Importing this module also registers the core commands and subscribes
// the native menu events (app/commands.ts).

import { cmdOrCtrl, isMac } from '../lib/platform.js';
import { trapFocus } from '../lib/focus-trap.js';
import { appStore } from '../store/store.js';
import { runCommand } from './command-registry.js';
import './commands.js';
import { pageEl } from './el.js';
import { KEY_SCOPES, matchBinding, type Binding } from './key-scopes.js';
import { scrollTrace } from './trace.js';

// Live read of the store: this runs inside event handlers and must never
// cache a slice across a store write.
const appData = () => appStore.getState();

// A matched binding ends dispatch whatever happens next. Its key is
// consumed unless the chord is reserved (command null) or the command
// declines. The consume sits in `finally` so a command that throws still
// swallows its key, as the key did before it was a command.
function runBinding(b: Binding, e: KeyboardEvent) {
  if (b.command === null) return;
  let declined = false;
  try {
    declined = !runCommand(b.command);
  } finally {
    if (!declined) {
      e.preventDefault();
      e.stopPropagation();
    }
  }
}

export function dispatchKey(e: KeyboardEvent, mac: boolean = isMac) {
  for (const scope of KEY_SCOPES) {
    if (!scope.active()) continue;
    if (scope.owns === 'text-input') {
      if (e.key === 'Escape' || e.key === 'Enter' || !cmdOrCtrl(e, mac)) {
        return;
      }
      continue;
    }
    const b = matchBinding(scope, e, mac);
    if (b) {
      runBinding(b, e);
      return;
    }
    if (scope.owns === 'exclusive') {
      if (scope.trap && trapFocus(pageEl(scope.trap), e)) e.stopPropagation();
      return;
    }
  }
}

window.addEventListener(
  'keydown',
  (e) => {
    // Freeze probe: record every keydown that reaches the renderer, with
    // the view and focus target at arrival. This is the discriminator for
    // the "keys do nothing in grid mode" report:
    //   • keydown events keep arriving but `ae` is BODY (not a terminal
    //     textarea) → keyboard focus was lost; the thread is fine.
    //   • NO keydown events recorded during the freeze window → the event
    //     never reached the renderer (thread blocked, or the OS/menu layer
    //     swallowed it). Cross-check against heartbeat-stall gaps.
    if (scrollTrace.rec.enabled) {
      scrollTrace.count('keydown');
      const ae = document.activeElement;
      scrollTrace.rec('keydown', {
        // e.code (physical key: 'KeyA', 'ArrowDown', 'Enter'), NOT e.key — the
        // trace is copied to the clipboard and frozen into localStorage, so
        // logging the typed character would leak passwords / tokens into a
        // pasted bug report. The physical key is all the probe needs (did the
        // event arrive, was it a nav key, where was focus).
        code: e.code,
        mods: `${e.metaKey ? 'M' : ''}${e.ctrlKey ? 'C' : ''}${e.altKey ? 'A' : ''}${e.shiftKey ? 'S' : ''}`,
        view: appData().view,
        ae: ae ? `${ae.tagName}.${ae.className || ''}`.trim() : 'none',
      });
    }
    dispatchKey(e);
  },
  true,
);
