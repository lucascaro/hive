// @vitest-environment jsdom
//
// Differential test for spec 478: the binding data in app/key-scopes.ts
// against a FROZEN copy of the matchers it replaced.
//
// legacyResolve below is the pre-478 capture-phase listener
// (src/app/keyboard.ts, keydown handler) copied mechanically, with each
// action call replaced by the command id that now runs it. The helper
// predicates (isHelpOverlayKey, navHistoryKey, activityKey, findKey,
// cmdOrCtrl) are copied verbatim from lib/keymap.ts and lib/platform.ts.
// Do not "fix" this copy: it is the oracle.
//
// Two things the copy deliberately leaves out, because they are not the
// matcher's job any more: whether a scope is active (the precedence suite
// covers that), and whether a matched command then declines (⌘⏎ in single
// view, ⌘9 past the last session — covered in key-scopes.test.ts). So the
// oracle reports the id a matched key reaches, the way the new resolver
// does.
//
// Every cell of key × modifier × platform must agree, except the cells
// the operator approved changing (spec 478 › Non-goals, A / B / D), and
// each of those categories must actually occur — an allow-list that
// covers nothing would hide a regression.
import { describe, expect, it } from 'vitest';
import {
  matchBinding,
  scopeById,
  KEY_SCOPES,
} from '../../src/app/key-scopes.js';

interface Ev {
  key: string;
  code: string;
  metaKey: boolean;
  ctrlKey: boolean;
  altKey: boolean;
  shiftKey: boolean;
  repeat: boolean;
}

const RESERVED = 'RESERVED';
type Out = string | null;

// ---------- frozen helpers (verbatim) ----------

function cmdOrCtrl(e: Ev, mac: boolean): boolean {
  return mac ? e.metaKey && !e.ctrlKey : e.ctrlKey && !e.metaKey;
}
function isHelpOverlayKey(e: Ev): boolean {
  if (!(e.metaKey || e.ctrlKey)) return false;
  return e.key === '/' || e.key === '?';
}
function navHistoryKey(e: Ev, isMac: boolean): 'back' | 'forward' | null {
  if (e.metaKey || !e.ctrlKey) return null;
  if (isMac ? e.altKey : !e.altKey) return null;
  if (!(e.key === '-' || e.key === '_' || e.code === 'Minus')) return null;
  return e.shiftKey ? 'forward' : 'back';
}
function activityKey(e: Ev, isMac: boolean): 'toggle' | 'grid' | null {
  if (!(e.code === 'KeyJ' || e.key === 'j' || e.key === 'J')) return null;
  if (isMac) {
    if (!e.metaKey || e.ctrlKey || e.altKey) return null;
    return e.shiftKey ? 'grid' : 'toggle';
  }
  if (!e.ctrlKey || e.metaKey || !e.shiftKey) return null;
  return e.altKey ? 'grid' : 'toggle';
}
function findKey(e: Ev, isMac: boolean): boolean {
  if (!(e.code === 'KeyF' || e.key === 'f' || e.key === 'F')) return false;
  if (isMac) return false;
  return Boolean(e.ctrlKey && e.shiftKey && !e.metaKey && !e.altKey);
}

// ---------- frozen listener, per scope ----------

function legacyResolve(scope: string, e: Ev, mac: boolean): Out {
  const isI = e.key === 'i' || e.key === 'I';
  switch (scope) {
    case 'inline-rename':
      return e.key === 'Escape' ? 'inline-rename.cancel' : null;
    case 'choice-dialog':
      return e.key === 'Escape' ? 'choice-dialog.dismiss' : null;
    case 'launcher':
    case 'project-editor':
      return null;
    case 'command-palette':
      return e.key === 'Escape' ? 'command-palette.close' : null;
    case 'settings':
      return e.key === 'Escape' || (cmdOrCtrl(e, mac) && e.key === ',')
        ? 'settings.close'
        : null;
    case 'worktrees':
      return e.key === 'Escape' ||
        (cmdOrCtrl(e, mac) && (e.key === 'e' || e.key === 'E'))
        ? 'worktrees.close'
        : null;
    case 'quick-idea':
      if (e.key === 'Escape' || (cmdOrCtrl(e, mac) && !e.shiftKey && isI))
        return 'quick-idea.close';
      if (cmdOrCtrl(e, mac) && e.shiftKey && isI) return 'idea-inbox';
      return null;
    case 'idea-inbox':
      if (e.key === 'Escape' || (cmdOrCtrl(e, mac) && e.shiftKey && isI))
        return 'idea-inbox.close';
      if (cmdOrCtrl(e, mac) && !e.shiftKey && isI) return 'quick-idea';
      return null;
    case 'help-overlay':
      return e.key === 'Escape' || isHelpOverlayKey(e)
        ? 'help-overlay.close'
        : null;
    case 'help-modal':
      if (e.key === 'Escape') return 'help-modal.close';
      if (isHelpOverlayKey(e)) return 'help-modal.shortcuts';
      return null;
    case 'whats-new':
      return e.key === 'Escape' ? 'whats-new.close' : null;
    case 'plugin-view':
      return e.key === 'Escape' ? 'plugin-view.close' : null;
    case 'build-log':
      return e.key === 'Escape' ? 'build-log.close' : null;
    case 'blocked-tile':
      return e.key === 'Enter' && !e.metaKey && !e.ctrlKey && !e.altKey
        ? 'session.answer-worktree-question'
        : null;
    case 'dead-overlay':
      if (e.key === 'Enter') return 'dead-session.close';
      if (e.key === 'Escape') return 'dead-session.dismiss';
      if (
        (e.key === 'r' || e.key === 'R') &&
        !e.metaKey &&
        !e.ctrlKey &&
        !e.altKey &&
        !e.repeat
      )
        return 'dead-session.restart';
      return null;
    case 'app':
      return legacyApp(e, mac);
  }
  throw new Error(`no oracle for scope ${scope}`);
}

function legacyApp(e: Ev, isMac: boolean): Out {
  if (
    e.ctrlKey &&
    !e.metaKey &&
    !e.altKey &&
    !e.shiftKey &&
    e.code === 'Backquote'
  )
    return 'open-os-terminal';
  const navDir = navHistoryKey(e, isMac);
  if (navDir) return navDir === 'back' ? 'nav-back' : 'nav-forward';
  const act = activityKey(e, isMac);
  if (act) return act === 'toggle' ? 'toggle-activity' : 'activity-grid';
  if (findKey(e, isMac)) return 'find-in-session';

  if (!cmdOrCtrl(e, isMac)) return null;

  if (e.key === '=' || e.key === '+') return 'zoom-in';
  if (e.key === '-' || e.key === '_') return 'zoom-out';
  if (e.key === '0') return 'zoom-reset';
  if ((e.key === 'k' || e.key === 'K') && e.shiftKey) return 'command-palette';
  if (e.key === 'Enter') {
    if (e.shiftKey) return RESERVED;
    return 'focus-active-session'; // declines in single view
  }
  if (isHelpOverlayKey(e)) return 'keyboard-shortcuts';
  if (e.key === ',') return 'settings';
  if (e.key === 'p' || e.key === 'P')
    return e.shiftKey ? 'duplicate-session-choose-tool' : 'duplicate-session';
  if (e.key === 't' || e.key === 'T')
    return e.shiftKey ? 'new-session-worktree' : 'new-session';
  if (e.key === 'Backspace' && e.shiftKey) return 'delete-project';
  if (e.key === 'e' || e.key === 'E') return 'worktrees';
  if (e.key === 'i' || e.key === 'I')
    return e.shiftKey ? 'idea-inbox' : 'quick-idea';
  if (e.key === 's' || e.key === 'S') return 'toggle-sidebar';
  if (e.key === 'g' || e.key === 'G')
    return e.shiftKey ? 'toggle-all-grid' : 'toggle-project-grid';
  if (e.key === 'n' || e.key === 'N')
    return e.shiftKey ? 'new-window' : 'new-project';
  if (e.key === 'b' || e.key === 'B')
    return e.shiftKey ? 'jump-back' : 'next-attention';
  if (e.key === 'w' || e.key === 'W')
    return e.shiftKey ? 'close-window' : 'close-session';
  if (e.key === 'z' || e.key === 'Z')
    return e.shiftKey ? RESERVED : 'reopen-closed-session';
  if (/^[1-9]$/.test(e.key)) return `switch-${e.key}`; // declines past the end
  if (e.key === 'ArrowLeft') return 'grid-left'; // declines in single view
  if (e.key === 'ArrowRight') return 'grid-right';
  if (e.key === 'ArrowUp')
    return e.shiftKey ? 'arrow-shift-up' : 'prev-session';
  if (e.key === 'ArrowDown')
    return e.shiftKey ? 'arrow-shift-down' : 'next-session';
  if (e.key === '[') return 'prev-project';
  if (e.key === ']') return 'next-project';
  return null; // (plugin chords: a separate scope, not swept here)
}

// ---------- the new resolver ----------

function newResolve(scope: string, e: Ev, mac: boolean): Out {
  const b = matchBinding(scopeById(scope), e, mac);
  if (!b) return null;
  return b.command ?? RESERVED;
}

// ---------- the sweep ----------

function codeFor(key: string): string {
  if (/^[a-z]$/i.test(key)) return `Key${key.toUpperCase()}`;
  if (/^[0-9]$/.test(key)) return `Digit${key}`;
  const named: Record<string, string> = {
    '-': 'Minus',
    _: 'Minus',
    '=': 'Equal',
    '+': 'Equal',
    ',': 'Comma',
    '/': 'Slash',
    '?': 'Slash',
    '[': 'BracketLeft',
    ']': 'BracketRight',
    '`': 'Backquote',
    '!': 'Digit1',
    ' ': 'Space',
  };
  return named[key] ?? key;
}

const KEYS = [
  ...'abcdefghijklmnopqrstuvwxyz',
  ...'ABCDEFGHIJKLMNOPQRSTUVWXYZ',
  ...'0123456789',
  ...'-_=+,/?[]`! ',
  'Enter',
  'Escape',
  'Backspace',
  'Tab',
  'ArrowLeft',
  'ArrowRight',
  'ArrowUp',
  'ArrowDown',
];
// Keys whose e.key a layout or ⌥ rewrites, so only e.code identifies them.
const CODE_ONLY: Array<[string, string]> = [
  ['Dead', 'Minus'],
  ['∆', 'KeyJ'],
  ['ƒ', 'KeyF'],
  ['†', 'KeyT'],
];

function* events(): Generator<Ev> {
  const pairs: Array<[string, string]> = [
    ...KEYS.map((k): [string, string] => [k, codeFor(k)]),
    ...CODE_ONLY,
  ];
  for (const [key, code] of pairs) {
    for (let m = 0; m < 16; m++) {
      for (const repeat of [false, true]) {
        yield {
          key,
          code,
          metaKey: !!(m & 1),
          ctrlKey: !!(m & 2),
          altKey: !!(m & 4),
          shiftKey: !!(m & 8),
          repeat,
        };
      }
    }
  }
}

// shortcut-capture (spec 477) postdates the frozen listener and binds
// nothing; the coverage and order tests in key-scopes.test.ts and
// every-shortcut.spec.ts hold it.
const SWEPT = KEY_SCOPES.map((s) => s.id).filter(
  (id) => id !== 'find-box' && id !== 'plugins' && id !== 'shortcut-capture',
);

// The approved deltas. Each returns true when this cell is one the spec
// allows to differ — and only in the direction "legacy fired, new does
// not".
const APPROVED = {
  // A: ⌥/Alt is no longer ignored on ⌘/Ctrl chords.
  A: (_s: string, e: Ev) => e.altKey && (e.metaKey || e.ctrlKey),
  // B: ⇧⌘E / ⇧⌘S no longer act as ⌘E / ⌘S (app, and closing worktrees).
  B: (s: string, e: Ev) =>
    e.shiftKey &&
    ((/^[eE]$/.test(e.key) && (s === 'app' || s === 'worktrees')) ||
      (/^[sS]$/.test(e.key) && s === 'app')),
  // D: the help chord needs exactly the platform modifier.
  D: (s: string, e: Ev, mac: boolean) =>
    (s === 'help-overlay' || s === 'help-modal') &&
    (e.key === '/' || e.key === '?') &&
    !cmdOrCtrl(e, mac),
};

// Spec 477's approved deltas, which change WHICH command a key runs
// rather than whether one runs, so they name both ends of the change.
const APPROVED_477 = {
  // D2: macOS ⌘F gains a keydown binding to find-in-session. The native
  // menu's ⌘F still takes the key first; the chord is in the data so the
  // menu accelerator is derived from it.
  F: (s: string, e: Ev, mac: boolean, was: string | null, now: string | null) =>
    mac &&
    s === 'app' &&
    /^f$/i.test(e.key) &&
    was === null &&
    now === 'find-in-session',
  // D1: ⇧⌘↑/↓ (⇧Ctrl↑/↓) run move-forward/backward in every view, as the
  // macOS menu always did; they ran arrow-shift-*, which moved spatially
  // in a grid.
  M: (
    s: string,
    _e: Ev,
    _mac: boolean,
    was: string | null,
    now: string | null,
  ) =>
    s === 'app' &&
    ((was === 'arrow-shift-up' && now === 'move-backward') ||
      (was === 'arrow-shift-down' && now === 'move-forward')),
};

describe('key bindings vs the pre-478 matchers', () => {
  const seen: Record<
    keyof typeof APPROVED | keyof typeof APPROVED_477,
    number
  > = { A: 0, B: 0, D: 0, F: 0, M: 0 };
  const unexpected: string[] = [];

  for (const mac of [true, false]) {
    for (const scope of SWEPT) {
      for (const e of events()) {
        const was = legacyResolve(scope, e, mac);
        const now = newResolve(scope, e, mac);
        if (was === now) continue;
        const why =
          (Object.keys(APPROVED) as Array<keyof typeof APPROVED>).find(
            (k) => now === null && APPROVED[k](scope, e, mac),
          ) ??
          (Object.keys(APPROVED_477) as Array<keyof typeof APPROVED_477>).find(
            (k) => APPROVED_477[k](scope, e, mac, was, now),
          );
        if (why) seen[why]++;
        else
          unexpected.push(
            `${mac ? 'mac' : 'other'} ${scope} key=${JSON.stringify(e.key)} code=${e.code} ` +
              `m${+e.metaKey}c${+e.ctrlKey}a${+e.altKey}s${+e.shiftKey}r${+e.repeat}: ${was} → ${now}`,
          );
      }
    }
  }

  it('agrees in every cell outside the approved deltas', () => {
    expect(unexpected.slice(0, 20)).toEqual([]);
  });

  it('each approved delta actually occurs', () => {
    expect(seen.A).toBeGreaterThan(0);
    expect(seen.B).toBeGreaterThan(0);
    expect(seen.D).toBeGreaterThan(0);
    expect(seen.F).toBeGreaterThan(0);
    expect(seen.M).toBeGreaterThan(0);
  });

  it('pins one representative cell per delta', () => {
    const e = (o: Partial<Ev>): Ev => ({
      key: 't',
      code: 'KeyT',
      metaKey: false,
      ctrlKey: false,
      altKey: false,
      shiftKey: false,
      repeat: false,
      ...o,
    });
    // A: Ctrl+Alt+T opened a session on Windows/Linux.
    const a = e({ ctrlKey: true, altKey: true });
    expect(legacyResolve('app', a, false)).toBe('new-session');
    expect(newResolve('app', a, false)).toBeNull();
    // B: ⇧⌘E opened (and closed) the worktree browser.
    const b = e({ key: 'E', code: 'KeyE', metaKey: true, shiftKey: true });
    expect(legacyResolve('app', b, true)).toBe('worktrees');
    expect(newResolve('app', b, true)).toBeNull();
    expect(newResolve('worktrees', b, true)).toBeNull();
    // D: ⌃/ closed the shortcuts overlay on macOS.
    const d = e({ key: '/', code: 'Slash', ctrlKey: true });
    expect(legacyResolve('help-overlay', d, true)).toBe('help-overlay.close');
    expect(newResolve('help-overlay', d, true)).toBeNull();
    // F (spec 477): ⌘F has a keydown binding on macOS.
    const f = e({ key: 'f', code: 'KeyF', metaKey: true });
    expect(legacyResolve('app', f, true)).toBeNull();
    expect(newResolve('app', f, true)).toBe('find-in-session');
    // M (spec 477): ⇧Ctrl↓ reorders on Windows/Linux.
    const m = e({
      key: 'ArrowDown',
      code: 'ArrowDown',
      ctrlKey: true,
      shiftKey: true,
    });
    expect(legacyResolve('app', m, false)).toBe('arrow-shift-down');
    expect(newResolve('app', m, false)).toBe('move-forward');
  });
});

// The named cases from the suites of the predicates this data replaced
// (lib/keymap.ts isHelpOverlayKey / navHistoryKey / activityKey, and
// findKey), restated over the app bindings. The sweep above proves the
// same thing exhaustively; these say WHY a cell matters.
describe('named cases from the replaced predicates', () => {
  const app = (o: Partial<Ev>, mac: boolean) =>
    newResolve(
      'app',
      {
        key: 'x',
        code: '',
        metaKey: false,
        ctrlKey: false,
        altKey: false,
        shiftKey: false,
        repeat: false,
        ...o,
      },
      mac,
    );

  it('help: ⌘/ and ⌘? on macOS, Ctrl+/ and Ctrl+? elsewhere', () => {
    expect(app({ metaKey: true, key: '/' }, true)).toBe('keyboard-shortcuts');
    expect(app({ metaKey: true, key: '?' }, true)).toBe('keyboard-shortcuts');
    expect(app({ metaKey: true, shiftKey: true, key: '?' }, true)).toBe(
      'keyboard-shortcuts',
    );
    expect(app({ ctrlKey: true, key: '/' }, false)).toBe('keyboard-shortcuts');
    expect(app({ ctrlKey: true, key: '?' }, false)).toBe('keyboard-shortcuts');
  });

  it('help: a bare "?" or "/" is typing and must reach the terminal', () => {
    for (const mac of [true, false]) {
      expect(app({ key: '?' }, mac)).toBeNull();
      expect(app({ shiftKey: true, key: '?' }, mac)).toBeNull();
      expect(app({ key: '/' }, mac)).toBeNull();
    }
  });

  it('nav (macOS): ⌃- back, ⌃⇧- / ⌃_ forward, physical Minus on other layouts', () => {
    expect(app({ ctrlKey: true, key: '-' }, true)).toBe('nav-back');
    expect(app({ ctrlKey: true, shiftKey: true, key: '-' }, true)).toBe(
      'nav-forward',
    );
    expect(app({ ctrlKey: true, shiftKey: true, key: '_' }, true)).toBe(
      'nav-forward',
    );
    expect(app({ ctrlKey: true, key: 'Dead', code: 'Minus' }, true)).toBe(
      'nav-back',
    );
  });

  it('nav (macOS): ⌘- is zoom, ⌃⌥- and ⌃⌘- are neither', () => {
    expect(app({ metaKey: true, key: '-' }, true)).toBe('zoom-out');
    expect(app({ ctrlKey: true, altKey: true, key: '-' }, true)).toBeNull();
    expect(app({ ctrlKey: true, metaKey: true, key: '-' }, true)).toBeNull();
  });

  it('nav (elsewhere): plain Ctrl+- stays zoom out; Ctrl+Alt+- is back', () => {
    // Load-bearing: claiming Ctrl+- for nav would silently remove zoom
    // out on two of three platforms.
    expect(app({ ctrlKey: true, key: '-' }, false)).toBe('zoom-out');
    expect(app({ ctrlKey: true, shiftKey: true, key: '_' }, false)).toBe(
      'zoom-out',
    );
    expect(app({ ctrlKey: true, altKey: true, key: '-' }, false)).toBe(
      'nav-back',
    );
    expect(
      app({ ctrlKey: true, altKey: true, shiftKey: true, key: '-' }, false),
    ).toBe('nav-forward');
    expect(app({ altKey: true, key: '-' }, false)).toBeNull();
  });

  it('nav: a bare "-" reaches the terminal', () => {
    expect(app({ key: '-' }, true)).toBeNull();
    expect(app({ key: '-' }, false)).toBeNull();
  });

  it('activity: ⌘J / ⌘⇧J on macOS; Ctrl+Shift+J / Ctrl+Alt+Shift+J elsewhere', () => {
    const j = { key: 'j', code: 'KeyJ' };
    expect(app({ ...j, metaKey: true }, true)).toBe('toggle-activity');
    expect(app({ ...j, metaKey: true, shiftKey: true, key: 'J' }, true)).toBe(
      'activity-grid',
    );
    expect(app({ ...j, ctrlKey: true, shiftKey: true, key: 'J' }, false)).toBe(
      'toggle-activity',
    );
    expect(
      app(
        { ...j, ctrlKey: true, altKey: true, shiftKey: true, key: 'J' },
        false,
      ),
    ).toBe('activity-grid');
  });

  it('activity: plain Ctrl+J (0x0a newline) is left to the terminal', () => {
    expect(app({ key: 'j', code: 'KeyJ', ctrlKey: true }, true)).toBeNull();
    expect(app({ key: 'j', code: 'KeyJ', ctrlKey: true }, false)).toBeNull();
  });

  it('find: Ctrl+Shift+F off macOS; never plain Ctrl+F; no macOS keydown', () => {
    const f = { key: 'f', code: 'KeyF' };
    expect(app({ ...f, ctrlKey: true, shiftKey: true }, false)).toBe(
      'find-in-session',
    );
    // Plain Ctrl+F is 0x06 — readline's forward-char.
    expect(app({ ...f, ctrlKey: true }, false)).toBeNull();
    expect(app({ ...f, ctrlKey: true }, true)).toBeNull();
    // macOS: the native menu accelerator owns ⌘F.
    expect(app({ ...f, metaKey: true, shiftKey: true }, true)).toBeNull();
    expect(
      app({ ...f, ctrlKey: true, shiftKey: true, altKey: true }, false),
    ).toBeNull();
    expect(
      app({ ...f, ctrlKey: true, shiftKey: true, metaKey: true }, false),
    ).toBeNull();
  });
});
