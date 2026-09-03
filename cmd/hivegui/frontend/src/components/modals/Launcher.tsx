// ---------- agent launcher ----------
//
// React port of src/app/modals/launcher.ts. The island mounts on
// #launcher, which keeps its id, its `role="menu"` and its `.hidden`
// class — the class is the open/closed signal every keyboard gate and
// e2e assertion reads, so it is applied from a layout effect the same
// way Phase 2's chrome islands apply theirs.
//
// The launcher's own listeners (keydown, mousedown, focusout, and the
// document-level outside click) stay on the root element rather than on
// a React wrapper: #launcher's children are styled as direct children,
// so an extra <div> would change the layout, and keyboard.ts bails out
// for the whole window while #launcher is visible — these listeners are
// the only thing handling keys while it is up.
//
// Per-open state (query, selection, the fetched agent list) is the body
// component's own state and is reset by remounting it on every open:
// `key={req.seq}`. That is what guarantees a reopen — ⌘T over an already
// open launcher — starts with an empty query, which the imperative
// version got by wiping #launcher.

import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import {
  CreateSession,
  DuplicateSession,
  IsGitRepo,
  ListAgents,
} from '../../bridge.js';
import { flashStatus, reportFailure } from '../../app/dom.js';
import { activeProjectId } from '../../app/selectors.js';
import { state } from '../../app/state.js';
import { cmdOrCtrl } from '../../lib/platform.js';
import {
  bumpAgentUsage,
  closeLauncher,
  loadAgentUsage,
  openLauncher,
} from '../../app/modals/launcher.js';
import { useAppStore, type LauncherRequest } from '../../store/store.js';
import { Kbd } from '../Kbd.js';
// Type-only, so the generated module is erased before Vite resolves it.
import type { main } from '../../../wailsjs/go/models';

export interface LauncherProps {
  root: HTMLElement | null;
  /** Drops the active tile's visual focus — the modal owns the keyboard. */
  setFocusedTile: (id: string | null) => void;
}

export function Launcher({ root, setFocusedTile }: LauncherProps): ReactNode {
  const entry = useAppStore((s) => s.modals.find((m) => m.id === 'launcher'));
  const open = !!entry;

  // #launcher sits outside React's tree, so its open/closed class is
  // applied here — as a layout effect, so the popup is visible in the
  // same frame its contents first paint.
  useLayoutEffect(() => {
    root?.classList.toggle('hidden', !open);
  }, [root, open]);

  // Attached once and live even while the launcher is closed, exactly as
  // initLauncher() attached it: a click that OPENS the launcher must not
  // also close it, and this listener runs after the opener's own handler
  // on the same event.
  const openRef = useRef(open);
  openRef.current = open;
  useEffect(() => {
    function onDocClick(e: MouseEvent) {
      if (!openRef.current || !root) return;
      const target = e.target as Element | null;
      // The sidebar's project actions are exempt because their buttons
      // open the launcher; any other opener opts in with
      // data-opens-launcher (the worktree browser's "Open session" does).
      const inAction =
        target?.closest('.hv-project-card__actions') ??
        target?.closest('[data-opens-launcher]');
      if (!root.contains(target) && !inAction) closeLauncher();
    }
    document.addEventListener('click', onDocClick);
    return () => document.removeEventListener('click', onDocClick);
  }, [root]);

  if (!entry || !root) return null;
  return (
    <LauncherBody
      key={entry.seq}
      req={entry.req}
      root={root}
      setFocusedTile={setFocusedTile}
    />
  );
}

function LauncherBody({
  req,
  root,
  setFocusedTile,
}: {
  req: LauncherRequest;
  root: HTMLElement;
  setFocusedTile: (id: string | null) => void;
}): ReactNode {
  const [query, setQuery] = useState('');
  const [selected, setSelected] = useState(0);
  // The usage-ordered list ListAgents returned, kept so filtering
  // re-renders from memory instead of refetching.
  const [agents, setAgents] = useState<main.AgentInfo[]>([]);
  // True until the request settles. Without it an empty list is
  // indistinguishable from "the query excluded everything", and the
  // first character typed during the round trip would replace the
  // loading row with "No agents match".
  const [loading, setLoading] = useState(true);
  const [useWorktree, setUseWorktree] = useState(req.useWorktree);
  const [branch, setBranch] = useState('');
  // Null until the IsGitRepo probe answers; false disables the worktree
  // row. The row renders enabled meanwhile — the probe almost always
  // beats the user to the checkbox.
  const [isGit, setIsGit] = useState<boolean | null>(null);

  const searchRef = useRef<HTMLInputElement | null>(null);
  const branchRef = useRef<HTMLInputElement | null>(null);
  const selectedRef = useRef<HTMLDivElement | null>(null);

  // In duplicate mode the cwd is fixed to the source session, so the
  // worktree toggle is meaningless. In resume mode the worktree already
  // exists, so neither the toggle nor the branch box has anything to
  // decide.
  const canWorktree = !req.duplicateFrom && !req.worktreePath;
  const worktreeOff = canWorktree && isGit === false;

  // Two readings of the same box, on purpose. `query` decides whether
  // the user is typing — the digit handler tests the same raw value, or
  // a lone space would show [n] hints that no longer fire. `q` decides
  // what matches, where surrounding whitespace is just noise.
  const q = query.trim().toLowerCase();
  const matches = q
    ? agents.filter((a) => a.name.toLowerCase().includes(q))
    : agents;

  // Position and focus, before the first paint: the popup is anchored
  // under the resolved project's card header so the user can see which
  // project the new session lands in. The header, not its + button: the
  // card's actions are `display: none` until the header is hovered
  // (components/ProjectCard.tsx), and a display:none anchor measures as a
  // zero rect at the origin. Falls back to the global new-project button
  // if the project's card isn't in the DOM (a minimized project), and to
  // a fixed spot over the sidebar if neither anchor exists.
  // Mount-only: one opening, one anchor. A re-anchor mid-open would move
  // the popup out from under the pointer.
  // biome-ignore lint/correctness/useExhaustiveDependencies: see above
  useLayoutEffect(() => {
    const anchorEl =
      document.querySelector(
        `.hv-project-card[data-pid="${req.projectId}"] .hv-project-card__header`,
      ) ?? document.getElementById('new-project-btn');
    if (anchorEl) {
      const r = anchorEl.getBoundingClientRect();
      root.style.left = `${r.left}px`;
      root.style.top = `${r.bottom + 4}px`;
    } else {
      root.style.left = '16px';
      root.style.top = '64px';
    }
    setFocusedTile(null);
  }, []);

  // Focus is a PASSIVE effect, deliberately. Layout effects run
  // child-first, so at that point #launcher is still `.hidden` — and
  // focus() on a display:none element is a silent no-op, which is
  // exactly how it shipped broken to the browser while jsdom (no CSS)
  // stayed green. Passive effects run after the island's layout effect
  // has revealed the popup.
  useEffect(() => {
    searchRef.current?.focus();
  }, []);

  // The agent list, and the git probe that decides whether a worktree is
  // even possible. Both are per-open (this body is remounted per open),
  // so the "did the launcher get reopened under me?" generation check the
  // imperative version needed is now just the unmount guard.
  useEffect(() => {
    let live = true;
    ListAgents()
      .then((list) => {
        if (!live) return;
        setLoading(false);
        // Sort by recent usage (most-used first); ties preserve the
        // agent package's display order. Usage is persisted in
        // localStorage and incremented on activation.
        const usage = loadAgentUsage();
        setAgents(
          (list || [])
            .map((a, i) => ({ a, i }))
            .sort((x, y) => {
              const ux = usage[x.a.id] || 0,
                uy = usage[y.a.id] || 0;
              if (ux !== uy) return uy - ux;
              return x.i - y.i;
            })
            .map((e) => e.a),
        );
      })
      // Anything thrown in the chain above used to land here silently —
      // the user pressed ⌘T and nothing happened, with no trace. Close
      // the loading shell too: an empty popup with a stale "Loading
      // agents…" would be worse.
      .catch((err) => {
        reportFailure('launcher')(err);
        if (!live) return;
        setLoading(false);
        closeLauncher();
      });
    return () => {
      live = false;
    };
  }, []);

  const projCwd = state.projects.find((p) => p.id === req.projectId)?.cwd ?? '';
  useEffect(() => {
    if (!canWorktree || !projCwd) return;
    let live = true;
    IsGitRepo(projCwd)
      .then((ok) => {
        if (!live) return;
        setIsGit(!!ok);
        if (!ok) {
          setUseWorktree(false);
          setBranch('');
        }
      })
      .catch(() => {
        // Intentionally silent: the probe rejects only when the bridge
        // itself is down. Worst case the daemon later refuses worktree
        // creation via control:error, which IS surfaced.
      });
    return () => {
      live = false;
    };
  }, [canWorktree, projCwd]);

  // Narrowing the list invalidates the old index — the row that was
  // selected may not even be rendered any more. Always land on the top
  // match so Enter means "the obvious one". The deps ARE the trigger
  // here; the body reads neither, which is what the rule objects to.
  // biome-ignore lint/correctness/useExhaustiveDependencies: see above
  useEffect(() => {
    setSelected(0);
  }, [query, agents]);

  // Same shape: `selected` is the trigger, the ref is how the row is
  // reached.
  // biome-ignore lint/correctness/useExhaustiveDependencies: see above
  useEffect(() => {
    selectedRef.current?.scrollIntoView?.({ block: 'nearest' });
  }, [selected]);

  // launchSelected runs the create/duplicate-session sequence shared by
  // the keyboard-select path and the per-row click handler: bump usage,
  // flash status, call the daemon, close the launcher.
  function launchSelected(agentId: string) {
    bumpAgentUsage(agentId);
    flashStatus('creating session…');
    // Anchor the new session under the one it came from (duplicate) or
    // under the active one, so it lands next to its context instead of
    // at the bottom of the sidebar.
    const anchor = req.duplicateFrom?.id || state.activeId || '';
    if (req.duplicateFrom) {
      DuplicateSession(
        agentId,
        req.projectId || '',
        req.duplicateCwd,
        anchor,
      ).catch(reportFailure('duplicate session'));
    } else {
      CreateSession(
        agentId,
        req.projectId || activeProjectId(),
        '',
        '',
        0,
        0,
        !!useWorktree,
        anchor,
        // Trimmed here rather than on every keystroke so the box stays
        // typable; a blank name means "let the daemon generate one".
        branch.trim(),
        req.worktreePath,
        req.continueConversation,
      ).catch(reportFailure('new session'));
    }
    closeLauncher();
  }

  function activateAt(i: number) {
    const a = matches[i];
    if (a) launchSelected(a.id);
  }

  function moveSelection(delta: number) {
    const n = matches.length;
    if (n === 0) return;
    setSelected((cur) => (cur + delta + n) % n);
  }

  // ---------- the listeners that live on #launcher ----------
  //
  // Re-attached on every render so they always close over the current
  // query and match list; removal is exact, so nothing accumulates.
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      const handle = (fn: () => void) => {
        e.preventDefault();
        e.stopPropagation();
        fn();
      };
      if (e.key === 'ArrowDown' || (e.key === 'Tab' && !e.shiftKey))
        return handle(() => moveSelection(+1));
      if (e.key === 'ArrowUp' || (e.key === 'Tab' && e.shiftKey))
        return handle(() => moveSelection(-1));
      if (e.key === 'Enter') return handle(() => activateAt(selected));
      if (e.key === 'Escape') return handle(closeLauncher);
      if (cmdOrCtrl(e) && (e.key === 'n' || e.key === 'N'))
        return handle(closeLauncher);
      // ⌘T / ⇧⌘T while already open re-opens (and so clears the query).
      // keyboard.ts bails out for #launcher entirely, so the binding has
      // to be repeated here or it would silently stop working.
      if (cmdOrCtrl(e) && (e.key === 't' || e.key === 'T'))
        return handle(() => {
          // Passing undefined re-resolves the active project rather than
          // pinning the one this opening was anchored to.
          if (e.shiftKey) openLauncher(undefined, { forceWorktree: true });
          else openLauncher();
        });
      // Digit shortcut: 1–9 picks the corresponding row, but only while
      // the filter box is empty — past that the user is typing a query
      // and a digit is just a character. Raw value, not trimmed: a typed
      // space is already a query. Skipped when a modifier is held so ⌘1
      // and friends aren't swallowed, and inside the branch box, where a
      // digit is part of the branch name (`fix-2`).
      if (
        !e.metaKey &&
        !e.ctrlKey &&
        !e.altKey &&
        /^[1-9]$/.test(e.key) &&
        query === '' &&
        e.target !== branchRef.current
      ) {
        const i = parseInt(e.key, 10) - 1;
        if (i < matches.length) {
          return handle(() => {
            setSelected(i);
            activateAt(i);
          });
        }
      }
    }
    // Nothing but the two text boxes may take focus. Clicking anything
    // else would blur them, and the keydown listener above only fires
    // while focus is inside #launcher — so the search would silently
    // stop responding to typing. preventDefault on mousedown suppresses
    // only the focus shift and text selection: click still fires, so
    // agent rows still launch and the worktree checkbox still toggles.
    function onMouseDown(e: MouseEvent) {
      const target = e.target as Element | null;
      if (target === searchRef.current || target === branchRef.current) return;
      e.preventDefault();
    }
    // Focus leaving the launcher closes it: keyboard.ts bails out for the
    // whole window while #launcher is visible and this module's keydown
    // listener only fires while focus is inside it, so a launcher that
    // stays visible after focus moves away is one nobody is listening
    // for, Escape included. relatedTarget null means focus went nowhere —
    // that's closeLauncher's own blur, so ignore it rather than recursing.
    function onFocusOut(e: FocusEvent) {
      const next = e.relatedTarget as Node | null;
      if (next && !root.contains(next)) closeLauncher();
    }
    root.addEventListener('keydown', onKeyDown);
    root.addEventListener('mousedown', onMouseDown);
    root.addEventListener('focusout', onFocusOut);
    return () => {
      root.removeEventListener('keydown', onKeyDown);
      root.removeEventListener('mousedown', onMouseDown);
      root.removeEventListener('focusout', onFocusOut);
    };
  });

  return (
    <>
      <input
        ref={searchRef}
        type="text"
        className="launcher-search"
        placeholder="Filter agents…"
        aria-label="Filter agents"
        autoComplete="off"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
      />
      {/* Between the filter box and the list, and only once the agent
          list has landed — the same order and timing the imperative
          version inserted it with. */}
      {canWorktree && !loading ? (
        <>
          <label
            className={
              worktreeOff ? 'launcher-worktree disabled' : 'launcher-worktree'
            }
          >
            <input
              type="checkbox"
              checked={useWorktree}
              disabled={worktreeOff}
              onChange={(e) => {
                const on = e.target.checked;
                setUseWorktree(on);
                try {
                  localStorage.setItem('hive.worktree', on ? '1' : '0');
                } catch {}
                if (!on) setBranch('');
              }}
            />
            <span>
              {worktreeOff
                ? 'Worktree (project is not a git repo)'
                : 'Create in git worktree'}
            </span>
          </label>
          {/* Revealed only while the toggle is on: it is meaningless
              otherwise, and an always-visible field would push the agent
              list down on every open. */}
          <input
            ref={branchRef}
            type="text"
            className={
              useWorktree ? 'launcher-branch' : 'launcher-branch hidden'
            }
            placeholder="branch name (optional)"
            aria-label="Worktree branch name"
            value={branch}
            onChange={(e) => setBranch(e.target.value)}
          />
        </>
      ) : null}
      <div className="launcher-list">
        {matches.length === 0 ? (
          // Three different facts, and conflating any two of them
          // misreads as a broken agent list: the request is still in
          // flight, the daemon returned nothing, or the query excluded
          // everything.
          loading ? (
            <div className="launcher-loading">Loading agents…</div>
          ) : (
            <div className="launcher-empty">
              {q ? 'No agents match' : 'No agents found'}
            </div>
          )
        ) : null}
        {matches.map((a, idx) => (
          // The rows are the launcher's keyboard path already: 1-9 pick
          // one directly, arrows move the selection and Enter launches
          // it (the listener on #launcher above). A role and a key
          // handler here would put a second, undocumented way to do the
          // same thing in the tab order — and the popup deliberately
          // keeps focus in the filter box.
          // biome-ignore lint/a11y/noStaticElementInteractions: see above
          // biome-ignore lint/a11y/useKeyWithClickEvents: see above
          <div
            key={a.id}
            ref={idx === selected ? selectedRef : undefined}
            className="launcher-item"
            data-selected={idx === selected ? '' : undefined}
            data-available={a.available ? undefined : 'false'}
            style={{ ['--agent-color' as string]: a.color }}
            onClick={() => launchSelected(a.id)}
            onMouseEnter={() => setSelected(idx)}
          >
            {/* Number keys 1–9 select that row directly; 10+ rows show no
                number. While a query is active the digits type into it
                instead of selecting, so the hints come off — a visible
                [n] that does nothing is worse than none (AGENTS.md, Key
                Discoverability). */}
            <span className="agent-num">
              {!query && idx < 9 ? <Kbd>{`[${idx + 1}]`}</Kbd> : null}
            </span>
            <span className="agent-dot" />
            <span className="agent-name">{a.name}</span>
            {!a.available && a.installCmd?.length ? (
              <span className="install-tag" title={a.installCmd.join(' ')}>
                install?
              </span>
            ) : null}
          </div>
        ))}
      </div>
    </>
  );
}
