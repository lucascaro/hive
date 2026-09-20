// ---------- the Help modal (sidebar help icon) ----------
//
// The wider help surface: what the pieces of hive are, the way into the
// keyboard-shortcuts overlay, and the links out. It is NOT the ⌘/ overlay —
// that one (components/modals/HelpOverlay.tsx, modal id 'help') is the
// shortcut list, and this one hands off to it. See app/modals/help.ts for why
// the ids are separate.
//
// Content is a static array rather than fetched or generated: it is four
// sentences and four links, and the cheapest thing that cannot go out of sync
// with a build is a literal.

import { useEffect, useLayoutEffect, type ReactNode } from 'react';
import { OpenURL } from '../../bridge.js';
import { reportFailure } from '../../app/dom.js';
import { closeHelp, handOffToShortcuts } from '../../app/modals/help.js';
import { isMac } from '../../lib/platform.js';
import { mod } from '../../lib/shortcuts.js';
import { useAppStore } from '../../store/store.js';
import { Kbd } from '../Kbd.js';
import { ModalShell } from './ModalShell.js';

// One const, not four literals that drift apart the next time anything moves.
const REPO = 'https://github.com/lucascaro/hive';

const CONCEPTS: ReadonlyArray<{ term: string; def: string }> = [
  {
    term: 'Project',
    def: 'A git repository hive works in. Projects hold sessions and carry the numbered shortcut in the sidebar.',
  },
  {
    term: 'Session',
    def: 'One running agent with its own terminal. Sessions survive the GUI closing, because the daemon owns them.',
  },
  {
    term: 'Worktree',
    def: 'A separate checkout of a branch, so two sessions can work on the same repository without fighting over files.',
  },
  {
    term: 'Agent',
    def: 'The command a session runs — Claude Code, Codex, a plain shell. Add your own in Settings.',
  },
];

const LINKS: ReadonlyArray<{ label: string; url: string }> = [
  { label: 'README', url: `${REPO}#readme` },
  { label: 'Documentation', url: `${REPO}/tree/main/docs` },
  { label: 'Report an issue', url: `${REPO}/issues/new` },
  { label: 'Releases', url: `${REPO}/releases` },
];

export function Help({ root }: { root: HTMLElement | null }): ReactNode {
  const entry = useAppStore((s) => s.modals.find((m) => m.id === 'help-modal'));

  // #help-modal sits outside React's tree, so its open/closed class is
  // applied here — a passive effect would paint one stale frame with the
  // backdrop up before the class caught up.
  useLayoutEffect(() => {
    root?.classList.toggle('hidden', !entry);
  }, [root, entry]);

  if (!entry || !root) return null;
  // Remounted per opening, which is what re-runs the mount-focus effect.
  return <HelpBody key={entry.seq} root={root} />;
}

function HelpBody({ root }: { root: HTMLElement }): ReactNode {
  // Same modal-focus discipline as the other dialogs: pull focus onto the
  // dialog so keystrokes don't leak behind the backdrop.
  useEffect(() => {
    document.getElementById('help-modal-close')?.focus();
  }, []);

  return (
    <ModalShell
      id="help-modal"
      root={root}
      title="Help"
      size="lg"
      onClose={closeHelp}
      hints={[{ keys: '[esc]', label: 'close' }]}
    >
      <div id="help-modal-body">
        <section>
          <h4>Concepts</h4>
          <dl className="help-concepts">
            {CONCEPTS.map((c) => (
              <div key={c.term}>
                <dt>{c.term}</dt>
                <dd>{c.def}</dd>
              </div>
            ))}
          </dl>
        </section>

        <section>
          <h4>Keyboard</h4>
          {/* The binding rides next to the action it triggers, per AGENTS.md
              › Key Discoverability — the whole reason this row exists rather
              than a sentence telling people to remember ⌘/. */}
          <button
            type="button"
            className="help-row"
            id="help-shortcuts-row"
            onClick={handOffToShortcuts}
          >
            <span>Keyboard shortcuts</span>
            <Kbd>{mod(isMac, '/')}</Kbd>
          </button>
        </section>

        <section>
          <h4>Links</h4>
          <div className="help-links">
            {/* Buttons, not anchors: a real href would navigate the Wails
                webview out of the app. Same OpenURL path as app/banners.ts. */}
            {LINKS.map((l) => (
              <button
                key={l.url}
                type="button"
                className="help-row"
                onClick={() => {
                  OpenURL(l.url).catch(reportFailure('open link'));
                }}
              >
                {l.label}
              </button>
            ))}
          </div>
        </section>
      </div>
    </ModalShell>
  );
}
