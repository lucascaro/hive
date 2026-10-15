// @vitest-environment jsdom
//
// Spec 338: the ⇧⌘M note sheet and the daemon's answers to it.
//
//   • Enter sends (queue=false); ⇧Enter does not
//   • the sheet stays up until the daemon answers, so a refused note is
//     not lost
//   • session_busy turns Send into "Queue until idle" with the text kept
//   • an ACP session is never offered a queue
//   • session:sent closes the sheet and focus returns to the terminal
//   • an empty or over-long note is not sent
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import type { Mock } from 'vitest';
import { act, fireEvent, render } from '@testing-library/react';
import { addSession, isModalOpen, resetStore } from '../../src/store/store.js';
import type { SessionInfo } from '../../src/app/state.js';

const SendToSession = vi.fn(
  (_id: string, _t: string, _q: boolean): Promise<void> => Promise.resolve(),
);
vi.mock('../../src/bridge.js', () => ({
  SendToSession: (...a: Parameters<typeof SendToSession>) =>
    SendToSession(...a),
}));

const flashStatus = vi.fn();
vi.mock('../../src/app/dom.js', () => ({
  flashStatus: (...a: unknown[]) => flashStatus(...a),
  setStatus: vi.fn(),
  reportFailure: () => () => {},
}));

const MARKUP = `
  <div id="app">
    <div id="message-session" class="hv-dialog hidden" role="dialog"
      aria-modal="true" aria-labelledby="message-session-title"></div>
  </div>`;

type Mod = typeof import('../../src/app/modals/message-session.js');
let m: Mod;
let MessageSession: typeof import('../../src/components/modals/MessageSession.js')['MessageSession'];
let refocusActiveTerm: Mock<() => void>;

beforeAll(async () => {
  document.body.innerHTML = MARKUP;
  m = await import('../../src/app/modals/message-session.js');
  ({ MessageSession } = await import(
    '../../src/components/modals/MessageSession.js'
  ));
  refocusActiveTerm = vi.fn();
  m.initMessageSession({ setFocusedTile: vi.fn(), refocusActiveTerm });
});

const el = <T extends HTMLElement = HTMLElement>(id: string) =>
  document.getElementById(id) as T | null;
const text = () =>
  el<HTMLTextAreaElement>('message-session-text') as HTMLTextAreaElement;

function session(over: Partial<SessionInfo> = {}): SessionInfo {
  return { id: 's1', name: 'refactor', alive: true, ...over } as SessionInfo;
}

beforeEach(() => {
  SendToSession.mockReset();
  SendToSession.mockResolvedValue(undefined);
  flashStatus.mockReset();
  refocusActiveTerm.mockReset();
  resetStore();
  render(<MessageSession root={el('message-session')} />, {
    container: el('message-session') as HTMLElement,
  });
});

async function open(over: Partial<SessionInfo> = {}) {
  addSession(session(over));
  await act(async () => {
    m.openMessageSession('s1');
  });
}

function type(v: string) {
  fireEvent.change(text(), { target: { value: v } });
}

describe('message sheet', () => {
  it('names the target and focuses the field', async () => {
    await open();
    expect(el('message-session-title')?.textContent).toContain('refactor');
    expect(document.activeElement).toBe(text());
  });

  it('Enter sends without queueing and keeps the sheet up', async () => {
    await open();
    type('rebase onto main');
    await act(async () => {
      fireEvent.keyDown(text(), { key: 'Enter' });
    });
    expect(SendToSession).toHaveBeenCalledWith('s1', 'rebase onto main', false);
    // Not closed until the daemon answers (session:sent).
    expect(isModalOpen('message-session')).toBe(true);
  });

  it('⇧Enter does not send', async () => {
    await open();
    type('line one');
    await act(async () => {
      fireEvent.keyDown(text(), { key: 'Enter', shiftKey: true });
    });
    expect(SendToSession).not.toHaveBeenCalled();
  });

  it('does not send an empty note', async () => {
    await open();
    type('   ');
    await act(async () => {
      fireEvent.keyDown(text(), { key: 'Enter' });
    });
    expect(SendToSession).not.toHaveBeenCalled();
    expect((el('message-session-send') as HTMLButtonElement).disabled).toBe(
      true,
    );
  });

  it('refuses an over-long note and says how long the limit is', async () => {
    await open();
    type('x'.repeat(m.MAX_MESSAGE_BYTES + 1));
    expect(el('message-session-count')?.textContent).toContain('/');
    expect((el('message-session-send') as HTMLButtonElement).disabled).toBe(
      true,
    );
    await act(async () => {
      fireEvent.keyDown(text(), { key: 'Enter' });
    });
    expect(SendToSession).not.toHaveBeenCalled();
  });

  it('session:sent closes the sheet and returns focus to the terminal', async () => {
    await open();
    type('hello');
    await act(async () => {
      fireEvent.keyDown(text(), { key: 'Enter' });
    });
    await act(async () => {
      m.onMessageSent('s1', 'typed');
    });
    expect(isModalOpen('message-session')).toBe(false);
    expect(refocusActiveTerm).toHaveBeenCalled();
    expect(flashStatus).toHaveBeenCalledWith(
      expect.stringMatching(/Typed into refactor/),
    );
  });

  it('says "queued" when the daemon queued it', async () => {
    await open();
    await act(async () => {
      m.onMessageSent('s1', 'queued');
    });
    expect(flashStatus).toHaveBeenCalledWith(
      expect.stringMatching(/Queued for refactor/),
    );
  });

  it('says "sent" for the Pi extension inbox, never "typed"', async () => {
    await open();
    await act(async () => {
      m.onMessageSent('s1', 'extension');
    });
    expect(flashStatus).toHaveBeenCalledWith('Sent to refactor');
    expect(flashStatus).not.toHaveBeenCalledWith(
      expect.stringMatching(/Typed into|held for approval/),
    );
  });

  it('says "sent" for the agent inbox, and mentions the bypass hold', async () => {
    await open();
    await act(async () => {
      m.onMessageSent('s1', 'socket');
    });
    expect(flashStatus).toHaveBeenCalledWith(
      expect.stringMatching(/Sent to refactor.*held for approval/),
    );
    expect(flashStatus).not.toHaveBeenCalledWith(
      expect.stringMatching(/Typed into/),
    );
  });

  it('session_busy keeps the text and offers to queue', async () => {
    await open();
    type('later please');
    await act(async () => {
      fireEvent.keyDown(text(), { key: 'Enter' });
    });
    let handled = false;
    await act(async () => {
      handled = m.onMessageError('session_busy', 's1');
    });
    expect(handled).toBe(true);
    expect(text().value).toBe('later please');
    expect(el('message-session-busy')).not.toBeNull();
    expect(el('message-session-send')).toBeNull();
    // Enter no longer sends: the sheet is asking a question.
    SendToSession.mockClear();
    await act(async () => {
      fireEvent.keyDown(text(), { key: 'Enter' });
    });
    expect(SendToSession).not.toHaveBeenCalled();
    await act(async () => {
      fireEvent.click(el('message-session-queue') as HTMLElement);
    });
    expect(SendToSession).toHaveBeenCalledWith('s1', 'later please', true);
  });

  it('never offers to queue for an ACP session', async () => {
    await open({ kind: 'acp' });
    type('hi');
    await act(async () => {
      m.onMessageError('session_busy', 's1');
    });
    expect(el('message-session-queue')).toBeNull();
    expect((el('message-session-send') as HTMLButtonElement).disabled).toBe(
      true,
    );
    expect(el('message-session-busy')?.textContent).toMatch(/mid-turn/);
  });

  it('other refusals are reported on the status line and keep the sheet', async () => {
    await open();
    type('hi');
    for (const code of [
      'session_dead',
      'delivery_failed',
      'no_such_session',
      'bad_payload',
    ]) {
      flashStatus.mockClear();
      let handled = false;
      await act(async () => {
        handled = m.onMessageError(code, 's1');
      });
      expect(handled, code).toBe(true);
      expect(flashStatus, code).toHaveBeenCalledWith(expect.any(String), true);
      expect(isModalOpen('message-session')).toBe(true);
    }
  });

  it('ignores errors for another session or with the sheet closed', async () => {
    await open();
    expect(m.onMessageError('session_busy', 'someone-else')).toBe(false);
    expect(m.onMessageError('worktree_dirty', 's1')).toBe(false);
    await act(async () => {
      m.closeMessageSession();
    });
    expect(m.onMessageError('session_busy', 's1')).toBe(false);
  });

  it('Cancel sends nothing and returns focus', async () => {
    await open();
    type('never mind');
    await act(async () => {
      fireEvent.click(el('message-session-cancel') as HTMLElement);
    });
    expect(SendToSession).not.toHaveBeenCalled();
    expect(isModalOpen('message-session')).toBe(false);
    expect(refocusActiveTerm).toHaveBeenCalled();
  });
});
