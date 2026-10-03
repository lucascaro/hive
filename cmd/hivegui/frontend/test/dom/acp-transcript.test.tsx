// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, cleanup, fireEvent, render } from '@testing-library/react';

vi.mock('../../src/bridge.js', () => ({
  GetAcpTranscript: vi.fn(() => Promise.resolve()),
  PromptAcp: vi.fn(() => Promise.resolve()),
  AnswerPermission: vi.fn(() => Promise.resolve()),
}));

// app/dom.js resolves the app shell's elements at import; the view only
// needs its failure reporter.
vi.mock('../../src/app/dom.js', () => ({
  reportFailure: () => () => {},
}));

import * as bridge from '../../src/bridge.js';
import { AcpTranscript } from '../../src/components/acp/AcpTranscript.js';
import type { AcpTranscriptMsg } from '../../src/lib/acp.js';
import {
  acpStore,
  applyAcpError,
  applyAcpFrame,
  resetAcpOnSessionList,
} from '../../src/store/acp.js';
import { resetStore } from '../../src/store/store.js';
import type { SessionInfo } from '../../src/app/state.js';

const GetAcpTranscript = vi.mocked(bridge.GetAcpTranscript);
const PromptAcp = vi.mocked(bridge.PromptAcp);
const AnswerPermission = vi.mocked(bridge.AnswerPermission);
const SID = 's1';

const session = (over: Partial<SessionInfo> = {}): SessionInfo =>
  ({
    id: SID,
    name: 'a',
    kind: 'acp',
    state_source: 'acp',
    ...over,
  }) as SessionInfo;

const frame = (m: Partial<AcpTranscriptMsg>) =>
  act(() => applyAcpFrame({ session_id: SID, epoch: 1, ...m }));

const flush = () =>
  act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });

const PERM = {
  request_id: '1-3',
  title: 'Read file',
  options: [
    { option_id: 'allow', name: 'Allow', kind: 'allow_once' },
    { option_id: 'reject', name: 'Deny', kind: 'reject_once' },
  ],
};

beforeEach(() => {
  resetStore({ sessions: [session()] });
  acpStore.setState({ byId: new Map() });
  vi.clearAllMocks();
});
afterEach(cleanup);

describe('AcpTranscript', () => {
  it('asks for the snapshot once and renders it', async () => {
    const { container } = render(<AcpTranscript sessionId={SID} />);
    await flush();
    expect(GetAcpTranscript).toHaveBeenCalledTimes(1);
    expect(container.textContent).toContain('Loading');
    frame({
      reset: true,
      items: [
        { id: 1, kind: 'user', text: 'do it', origin: 'user' },
        { id: 2, kind: 'agent', text: '**bold** reply' },
        {
          id: 3,
          kind: 'plan',
          plan: [{ content: 'step one', status: 'completed' }],
        },
        {
          id: 4,
          kind: 'tool',
          title: 'Read file',
          tool_kind: 'read',
          status: 'completed',
        },
      ],
    });
    expect(
      container.querySelector('.acp-item--agent strong')?.textContent,
    ).toBe('bold');
    expect(
      container.querySelector('.acp-item--plan [data-status="done"]')
        ?.textContent,
    ).toContain('step one');
    expect(container.querySelector('.acp-item--tool')?.textContent).toContain(
      'Read file',
    );
    expect(GetAcpTranscript).toHaveBeenCalledTimes(1);
  });

  it('streams appended chunks into one message', async () => {
    const { container } = render(<AcpTranscript sessionId={SID} />);
    frame({ reset: true, items: [{ id: 1, kind: 'agent', text: 'hel' }] });
    frame({ items: [{ id: 1, kind: 'agent', text: 'lo', append: true }] });
    expect(container.querySelectorAll('.acp-item--agent')).toHaveLength(1);
    expect(container.querySelector('.acp-item--agent')?.textContent).toBe(
      'hello',
    );
  });

  it('refetches when a delta comes from a new epoch, and on reconnect', async () => {
    render(<AcpTranscript sessionId={SID} />);
    await flush();
    frame({ reset: true, items: [] });
    frame({ epoch: 2, items: [{ id: 1, kind: 'agent', text: 'x' }] });
    await flush();
    expect(GetAcpTranscript).toHaveBeenCalledTimes(2);
    frame({ reset: true, epoch: 2, items: [] });
    act(() => resetAcpOnSessionList(new Set([SID])));
    await flush();
    expect(GetAcpTranscript).toHaveBeenCalledTimes(3);
  });

  it('labels a prompt the user did not type here', () => {
    const { container } = render(<AcpTranscript sessionId={SID} />);
    frame({
      reset: true,
      items: [
        { id: 1, kind: 'user', text: 'a', origin: 'plugin:ci' },
        { id: 2, kind: 'user', text: 'b', origin: 'replayed' },
      ],
    });
    const origins = [...container.querySelectorAll('.acp-item__origin')].map(
      (e) => e.textContent,
    );
    expect(origins).toEqual(['From plugin ci', 'Earlier turn']);
  });

  it('answers a permission request with the option it offered', () => {
    const { getByText, container } = render(<AcpTranscript sessionId={SID} />);
    frame({ reset: true, items: [], permission: PERM });
    expect(container.querySelector('.acp-permission')?.textContent).toContain(
      'Read file',
    );
    fireEvent.click(getByText('Deny'));
    expect(AnswerPermission).toHaveBeenCalledWith(SID, '1-3', 'reject');
    frame({ items: [] }); // the daemon's next message carries no permission
    expect(container.querySelector('.acp-permission')).toBeNull();
  });

  it('Enter sends the prompt, Shift+Enter does not', () => {
    const { container } = render(<AcpTranscript sessionId={SID} />);
    frame({ reset: true, items: [] });
    const box = container.querySelector(
      'textarea[data-acp-prompt]',
    ) as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: 'first line' } });
    fireEvent.keyDown(box, { key: 'Enter', shiftKey: true });
    expect(PromptAcp).not.toHaveBeenCalled();
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(PromptAcp).toHaveBeenCalledWith(SID, 'first line');
    expect(box.value).toBe('');
  });

  it('does not send on the Enter that commits an IME composition', () => {
    const { container } = render(<AcpTranscript sessionId={SID} />);
    frame({ reset: true, items: [] });
    const box = container.querySelector(
      'textarea[data-acp-prompt]',
    ) as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: '日本' } });
    // WebKit: compositionend has fired, so isComposing is false.
    fireEvent.keyDown(box, { key: 'Enter', keyCode: 229 });
    expect(PromptAcp).not.toHaveBeenCalled();
    expect(box.value).toBe('日本');
  });

  it('does not send while the agent is working', () => {
    resetStore({ sessions: [session({ state: 'working' })] });
    const { container } = render(<AcpTranscript sessionId={SID} />);
    const box = container.querySelector(
      'textarea[data-acp-prompt]',
    ) as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: 'again' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(PromptAcp).not.toHaveBeenCalled();
    expect(box.value).toBe('again');
  });

  it('moves focus to the prompt once a permission is answered', () => {
    const { getByText, container } = render(<AcpTranscript sessionId={SID} />);
    frame({ reset: true, items: [], permission: PERM });
    const allow = getByText('Allow').closest('button') as HTMLButtonElement;
    allow.focus();
    fireEvent.click(allow);
    expect(document.activeElement).toBe(
      container.querySelector('textarea[data-acp-prompt]'),
    );
  });

  it('gives a prompt back when it could not be sent', async () => {
    PromptAcp.mockImplementationOnce(() =>
      Promise.reject(new Error('no control')),
    );
    const { container } = render(<AcpTranscript sessionId={SID} />);
    frame({ reset: true, items: [] });
    const box = container.querySelector(
      'textarea[data-acp-prompt]',
    ) as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: 'keep me' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(box.value).toBe('');
    await flush();
    expect(box.value).toBe('keep me');
  });

  it('gives a prompt back when the daemon refuses it', async () => {
    const { container } = render(<AcpTranscript sessionId={SID} />);
    frame({ reset: true, items: [] });
    const box = container.querySelector(
      'textarea[data-acp-prompt]',
    ) as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: 'too soon' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await flush();
    expect(box.value).toBe('');
    // Another session's refusal is not this one's.
    act(() => applyAcpError({ code: 'acp_busy', session_id: 'other' }));
    expect(box.value).toBe('');
    act(() => applyAcpError({ code: 'acp_busy', session_id: SID }));
    expect(box.value).toBe('too soon');
  });

  it('keeps an accepted prompt gone, even if an error follows', async () => {
    const { container } = render(<AcpTranscript sessionId={SID} />);
    frame({ reset: true, items: [] });
    const box = container.querySelector(
      'textarea[data-acp-prompt]',
    ) as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: 'go' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await flush();
    frame({ items: [{ id: 1, kind: 'user', text: 'go', origin: 'user' }] });
    act(() => applyAcpError({ code: 'session_dead', session_id: SID }));
    expect(box.value).toBe('');
  });

  it('shows a refused snapshot as failed, not loading', async () => {
    const { container } = render(<AcpTranscript sessionId={SID} />);
    await flush();
    expect(container.textContent).toContain('Loading');
    act(() => applyAcpError({ code: 'not_acp_session', session_id: SID }));
    expect(container.textContent).toContain("Couldn't load the transcript");
  });
});
