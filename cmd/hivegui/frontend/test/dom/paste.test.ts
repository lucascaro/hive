// @vitest-environment jsdom
//
// Regression test: Ctrl+Shift+V used to paste twice.
//
// The terminal's custom key handler (src/app/session-term.ts) owns
// Ctrl+Shift+V: it reads the system clipboard over the Wails bridge and
// writes the text to the PTY, then returns false. Returning false only
// tells xterm.js to skip its own *keydown* translation — it does NOT
// cancel the DOM default action. The webview therefore went on to
// perform its native paste, which fires a `paste` event on xterm's
// helper textarea, and xterm's own paste listener wrote the same
// clipboard text to the PTY a second time. One keypress, two pastes.
//
// The fix is e.preventDefault() on the branch that consumes the key
// (matching every other consuming branch in the same handler), plus
// routing our own write through term.paste() so the single surviving
// write keeps the newline normalisation and bracketed-paste framing
// that xterm's suppressed native handler used to provide.
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import * as store from '../../src/store/store.js';
import { clearTerms } from '../../src/store/terms.js';

const writes: string[] = [];
let clipboardText = 'CLIP';

vi.mock('../../src/bridge.js', () => {
  const fn = () => vi.fn(() => Promise.resolve());
  return {
    ConnectControl: fn(),
    OpenSession: fn(),
    CloseAttach: fn(),
    WriteStdin: vi.fn((_id: string, b64: string) => {
      writes.push(atob(b64));
      return Promise.resolve();
    }),
    ResizeSession: fn(),
    RequestScrollbackReplay: fn(),
    CreateSession: fn(),
    DuplicateSession: fn(),
    KillSession: fn(),
    RestartSession: fn(),
    UpdateSession: fn(),
    ListAgents: fn(),
    ListCustomAgents: fn(),
    SaveCustomAgents: fn(),
    CreateProject: fn(),
    KillProject: fn(),
    UpdateProject: fn(),
    LaunchDir: fn(),
    PickDirectory: fn(),
    OpenNewWindow: fn(),
    CloseWindow: fn(),
    IsGitRepo: fn(),
    OpenURL: fn(),
    OpenTerminalAt: fn(),
    Notify: fn(),
    Confirm: fn(),
    RestartDaemon: fn(),
    CheckForUpdate: fn(),
    SetClipboardText: fn(),
    LogFrontend: vi.fn(),
    EventsOn: vi.fn(),
    WindowSetTitle: vi.fn(),
    ClipboardGetText: vi.fn(() => Promise.resolve(clipboardText)),
  };
});

type SessionTermClass =
  typeof import('../../src/app/session-term.js').SessionTerm;
type Info = import('../../src/app/state.js').SessionInfo;

let SessionTerm: SessionTermClass;

beforeAll(async () => {
  // Same jsdom shims as test/dom/session-phase.test.ts: view.ts installs
  // a container ResizeObserver at module load, and xterm's DPR watcher
  // needs matchMedia.
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver;
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener() {},
    removeEventListener() {},
    addListener() {},
    removeListener() {},
    onchange: null,
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;
  document.body.innerHTML =
    '<div id="terms"></div><ul id="projects"></ul><div id="status"><span id="status-text"></span><span id="status-hint"></span></div>';
  ({ SessionTerm } = await import('../../src/app/session-term.js'));
});

let seq = 0;

beforeEach(() => {
  writes.length = 0;
  clipboardText = 'CLIP';
  clearTerms();
  store.resetStore();
});

function mount() {
  seq += 1;
  const info = { id: `paste-${seq}`, name: 'test', color: '#888' } as Info;
  const st = new SessionTerm(info);
  const textarea = st.body.querySelector('textarea');
  if (!textarea) throw new Error('xterm helper textarea not mounted');
  return { st, textarea };
}

// Let the ClipboardGetText promise chain settle.
const settle = () => new Promise((r) => setTimeout(r, 0));

function ctrlShiftKey(key: string) {
  return new KeyboardEvent('keydown', {
    key,
    ctrlKey: true,
    shiftKey: true,
    bubbles: true,
    cancelable: true,
  });
}

describe('Ctrl+Shift+V paste', () => {
  it('cancels the keydown default so the webview cannot paste a second time', async () => {
    const { textarea } = mount();
    const ev = ctrlShiftKey('V');
    textarea.dispatchEvent(ev);
    await settle();

    // The whole bug in one assertion: an un-cancelled keydown lets the
    // webview run its native paste on top of ours.
    expect(ev.defaultPrevented).toBe(true);
    expect(writes).toEqual(['CLIP']);
  });

  it('writes the clipboard exactly once', async () => {
    const { textarea } = mount();
    textarea.dispatchEvent(ctrlShiftKey('v'));
    await settle();
    expect(writes).toHaveLength(1);
  });

  it('normalises newlines instead of writing the clipboard raw', async () => {
    clipboardText = 'one\r\ntwo';
    const { textarea } = mount();
    textarea.dispatchEvent(ctrlShiftKey('v'));
    await settle();
    // \r\n would submit twice in Claude/Codex; xterm's paste path folds
    // it to a single \r, which is what the native paste used to deliver.
    expect(writes).toEqual(['one\rtwo']);
  });

  it('brackets the paste when the running program enabled DECSET 2004', async () => {
    clipboardText = 'hello';
    const { st, textarea } = mount();
    await new Promise<void>((r) => st.term.write('\x1b[?2004h', r));
    textarea.dispatchEvent(ctrlShiftKey('v'));
    await settle();
    expect(writes).toEqual(['\x1b[200~hello\x1b[201~']);
  });
});

// Reattach / width-changing replay repaints a tile from the daemon's
// snapshot, which opens with DECSTR (\x1b[!p). DECSTR clears DEC private
// modes, including bracketed paste — and the running program never
// re-sends them, because it has no idea a new client attached. Unless
// the snapshot re-asserts them, the tile pastes unframed for the rest of
// the session, and a >1 KiB paste then splits at the macOS PTY
// input-buffer boundary into several "pasted text" chunks in the agent.
//
// These two tests are the receiving end of internal/session/decmodes.go:
// that one proves the daemon emits the restore bytes, this one proves
// xterm actually honours them.
describe('DEC private modes across a snapshot repaint', () => {
  // The shape of what the daemon sends on reattach: soft reset, erase,
  // home, then (with the fix) the restore of whatever was set.
  const SNAPSHOT_PREAMBLE = '\x1b[!p\x1b[3J\x1b[2J\x1b[H';

  it('loses bracketed paste when the snapshot does not re-assert it', async () => {
    clipboardText = 'hello';
    const { st, textarea } = mount();
    await new Promise<void>((r) => st.term.write('\x1b[?2004h', r));
    // A repaint with no mode restore — the pre-fix daemon behaviour.
    await new Promise<void>((r) => st.term.write(SNAPSHOT_PREAMBLE, r));
    textarea.dispatchEvent(ctrlShiftKey('v'));
    await settle();
    // Unframed. This is the bug, pinned so the mechanism stays visible.
    expect(writes).toEqual(['hello']);
  });

  it('keeps bracketed paste when the snapshot re-asserts it', async () => {
    clipboardText = 'hello';
    const { st, textarea } = mount();
    await new Promise<void>((r) => st.term.write('\x1b[?2004h', r));
    await new Promise<void>((r) =>
      st.term.write(SNAPSHOT_PREAMBLE + '\x1b[?2004h', r),
    );
    textarea.dispatchEvent(ctrlShiftKey('v'));
    await settle();
    expect(writes).toEqual(['\x1b[200~hello\x1b[201~']);
  });
});

describe('Ctrl+Shift+C / Ctrl+Shift+A', () => {
  it('cancel their keydown default too', () => {
    const { textarea } = mount();
    const copy = ctrlShiftKey('C');
    const selectAll = ctrlShiftKey('A');
    textarea.dispatchEvent(copy);
    textarea.dispatchEvent(selectAll);
    expect(copy.defaultPrevented).toBe(true);
    expect(selectAll.defaultPrevented).toBe(true);
  });

  it('leaves other Ctrl+Shift keys to xterm', () => {
    const { textarea } = mount();
    const other = ctrlShiftKey('Z');
    textarea.dispatchEvent(other);
    expect(other.defaultPrevented).toBe(false);
  });
});

describe('xterm native paste path (evidence for the duplicate)', () => {
  it('writes to the PTY on a bare paste event, with no keydown involved', () => {
    const { textarea } = mount();
    const ev = new Event('paste', {
      bubbles: true,
      cancelable: true,
    }) as ClipboardEvent;
    Object.defineProperty(ev, 'clipboardData', {
      value: { getData: () => 'CLIP' },
    });
    textarea.dispatchEvent(ev);
    // This is the second write the user saw. It is reachable only when
    // the Ctrl+Shift+V keydown leaves its default action intact.
    expect(writes).toEqual(['CLIP']);
  });
});
