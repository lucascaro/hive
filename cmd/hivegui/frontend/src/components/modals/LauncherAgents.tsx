// Settings → Agents → "In the launcher": which agents the new-session
// launcher shows, and which it pins to the top in a user-chosen order.
//
// Controlled: Settings owns the draft (so Save/Cancel treat it like every
// other draft in the dialog) and this component only renders it and emits
// the next value. The ordering rule it feeds is lib/agent-order.ts.
//
// Two lists, not one: only pinned rows reorder, and the drag placeholder
// resolves a drop against its `data-drag-row` neighbours. Keeping unpinned
// rows in a separate <ul> with no `data-drag-row` means a drop can never
// resolve into them.

import {
  type CSSProperties,
  type ReactNode,
  useLayoutEffect,
  useRef,
  useState,
} from 'react';
import type { AgentPrefs } from '../../lib/agent-order.js';
import { moveIndex, moveItem } from '../../lib/agent-order.js';
import { dragRowProps } from '../../lib/drag-row.js';
import { isMac } from '../../lib/platform.js';
import { Kbd } from '../Kbd.js';
// Type-only, so the generated module is erased before Vite resolves it.
import type { main } from '../../../wailsjs/go/models';

const DRAG_MIME = 'text/x-hive-agent';

export function LauncherAgents({
  catalog,
  prefs,
  onChange,
}: {
  catalog: main.AgentInfo[];
  prefs: AgentPrefs;
  onChange: (next: AgentPrefs) => void;
}): ReactNode {
  const byId = new Map(catalog.map((a) => [a.id, a]));
  const pinned = prefs.pinned
    .map((id) => byId.get(id))
    .filter((a): a is main.AgentInfo => a !== undefined);
  const rest = catalog.filter((a) => !prefs.pinned.includes(a.id));
  const allHidden =
    catalog.length > 0 && catalog.every((a) => prefs.hidden.includes(a.id));

  // Pinning moves a row between the two lists (a remount), and a keyboard
  // move can make React re-insert the focused row; both blur it. The pin
  // box to refocus is remembered and restored after the commit.
  const [refocus, setRefocus] = useState<string | null>(null);
  const rootRef = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    if (!refocus) return;
    rootRef.current
      ?.querySelector<HTMLInputElement>(
        `li[data-agent-id="${CSS.escape(refocus)}"] .settings-launcher-pin input`,
      )
      ?.focus();
    setRefocus(null);
  }, [refocus]);

  function setShown(id: string, shown: boolean) {
    const hidden = prefs.hidden.filter((h) => h !== id);
    onChange({ ...prefs, hidden: shown ? hidden : [...hidden, id] });
  }
  function setPinned(id: string, pin: boolean) {
    const rest = prefs.pinned.filter((p) => p !== id);
    onChange({ ...prefs, pinned: pin ? [...rest, id] : rest });
    setRefocus(id);
  }
  function move(id: string, targetId: string, above: boolean) {
    const from = prefs.pinned.indexOf(id);
    const target = prefs.pinned.indexOf(targetId);
    if (from < 0 || target < 0) return;
    const to = moveIndex(from, target, above);
    if (to === from) return;
    onChange({ ...prefs, pinned: moveItem(prefs.pinned, from, to) });
  }

  function row(a: main.AgentInfo, isPinned: boolean) {
    const shown = !prefs.hidden.includes(a.id);
    const drag = isPinned
      ? {
          'data-drag-row': '',
          ...dragRowProps({
            mime: DRAG_MIME,
            id: a.id,
            onCommit: (dragged, target, above) =>
              move(dragged, target.dataset.agentId ?? '', above),
          }),
        }
      : {};
    return (
      <li
        key={a.id}
        className="settings-launcher-agent"
        data-agent-id={a.id}
        style={{ '--agent-color': a.color } as CSSProperties}
        {...drag}
        onKeyDown={
          isPinned
            ? (e) => {
                if (!e.altKey) return;
                const i = pinned.findIndex((p) => p.id === a.id);
                const j =
                  e.key === 'ArrowUp'
                    ? i - 1
                    : e.key === 'ArrowDown'
                      ? i + 1
                      : -1;
                if (j < 0 || j >= pinned.length) return;
                e.preventDefault();
                move(a.id, pinned[j].id, j < i);
                setRefocus(a.id);
              }
            : undefined
        }
      >
        <label className="settings-check">
          <input
            type="checkbox"
            checked={shown}
            aria-label={`Show ${a.name} in the launcher`}
            onChange={(e) => setShown(a.id, e.target.checked)}
          />
          <span className="settings-launcher-dot" />
          <span className="settings-launcher-name">{a.name}</span>
        </label>
        <label className="settings-check settings-launcher-pin">
          <input
            type="checkbox"
            checked={isPinned}
            aria-label={`Pin ${a.name} to the top of the launcher`}
            onChange={(e) => setPinned(a.id, e.target.checked)}
          />
          <span>Pin</span>
        </label>
      </li>
    );
  }

  return (
    <div id="settings-launcher-agents" ref={rootRef}>
      <p className="settings-hint">
        Untick an agent to hide it from the new-session menu. Pinned agents come
        first, in the order you drag them into (or{' '}
        <Kbd>{isMac ? '⌥↑' : 'Alt+↑'}</Kbd> /{' '}
        <Kbd>{isMac ? '⌥↓' : 'Alt+↓'}</Kbd>); the rest follow by how often you
        launch them. Agents you add above appear here once saved.
      </p>
      {allHidden ? (
        <p className="settings-hint" id="settings-launcher-empty">
          Every agent is hidden, so the new-session menu will be empty.
        </p>
      ) : null}
      <ul
        id="settings-launcher-pinned"
        className="settings-launcher-list"
        aria-label="Pinned agents"
      >
        {pinned.map((a) => row(a, true))}
      </ul>
      <ul
        id="settings-launcher-rest"
        className="settings-launcher-list"
        aria-label="Other agents"
      >
        {rest.map((a) => row(a, false))}
      </ul>
    </div>
  );
}
