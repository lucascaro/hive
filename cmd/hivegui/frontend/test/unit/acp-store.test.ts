import { describe, it, expect, vi, beforeEach } from 'vitest';

vi.mock('../../src/bridge.js', () => ({
  GetAcpTranscript: vi.fn(() => Promise.resolve()),
}));

import * as bridge from '../../src/bridge.js';
import {
  acpStore,
  applyAcpError,
  applyAcpFrame,
  forgetAcp,
  noteSentPrompt,
  requestAcpTranscript,
  resetAcpOnSessionList,
} from '../../src/store/acp.js';
import {
  applyAcp,
  emptyAcp,
  MAX_ITEMS,
  type AcpTranscript,
  type AcpTranscriptMsg,
} from '../../src/lib/acp.js';

const msg = (m: Partial<AcpTranscriptMsg>): AcpTranscriptMsg => ({
  session_id: 's1',
  epoch: 1,
  ...m,
});

const loaded = (over: Partial<AcpTranscript> = {}): AcpTranscript =>
  applyAcp(
    emptyAcp(),
    msg({
      reset: true,
      items: [
        { id: 1, kind: 'user', text: 'hi', origin: 'user' },
        { id: 2, kind: 'agent', text: 'hel' },
      ],
      ...over,
    } as Partial<AcpTranscriptMsg>),
  );

describe('applyAcp', () => {
  it('a reset replaces everything and marks the copy loaded', () => {
    const t = loaded();
    const next = applyAcp(
      t,
      msg({
        reset: true,
        epoch: 2,
        items: [{ id: 9, kind: 'agent', text: 'x' }],
      }),
    );
    expect(next.loaded).toBe(true);
    expect(next.epoch).toBe(2);
    expect(next.items.map((i) => i.id)).toEqual([9]);
  });

  it('drops a delta that arrives before any snapshot', () => {
    const t = emptyAcp();
    expect(
      applyAcp(t, msg({ items: [{ id: 1, kind: 'agent', text: 'x' }] })),
    ).toBe(t);
  });

  it('appends a chunk to the end of its item', () => {
    const t = applyAcp(
      loaded(),
      msg({ items: [{ id: 2, kind: 'agent', text: 'lo', append: true }] }),
    );
    expect(t.items[1].text).toBe('hello');
    expect(t.items[1].append).toBeUndefined();
  });

  it('replaces an item a non-append delta names, and adds a new one', () => {
    const t = applyAcp(
      loaded(),
      msg({
        items: [
          { id: 2, kind: 'agent', text: 'whole' },
          { id: 3, kind: 'tool', tool_call_id: 't1', status: 'pending' },
        ],
      }),
    );
    expect(t.items.map((i) => i.text ?? i.status)).toEqual([
      'hi',
      'whole',
      'pending',
    ]);
  });

  it('marks the copy stale on a delta from another epoch', () => {
    const t = applyAcp(
      loaded(),
      msg({ epoch: 2, items: [{ id: 1, kind: 'agent', text: 'new run' }] }),
    );
    expect(t.loaded).toBe(false);
    expect(t.items[1].text).toBe('hel');
  });

  it('overwrites the pending permission from every message', () => {
    const perm = {
      request_id: '1-1',
      options: [{ option_id: 'allow', name: 'Allow', kind: 'allow_once' }],
    };
    const asked = applyAcp(loaded(), msg({ permission: perm }));
    expect(asked.permission?.request_id).toBe('1-1');
    const answered = applyAcp(asked, msg({}));
    expect(answered.permission).toBeNull();
  });

  it('returns the same object when nothing changed', () => {
    const t = loaded();
    expect(applyAcp(t, msg({}))).toBe(t);
  });
});

describe('acp store', () => {
  const GetAcpTranscript = vi.mocked(bridge.GetAcpTranscript);
  const flush = async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  };
  beforeEach(() => {
    acpStore.setState({ byId: new Map() });
    GetAcpTranscript.mockReset();
    GetAcpTranscript.mockImplementation(() => Promise.resolve());
  });

  it('drops a delta for a session nobody has shown', () => {
    applyAcpFrame(msg({ items: [{ id: 1, kind: 'agent', text: 'x' }] }));
    expect(acpStore.getState().byId.has('s1')).toBe(false);
  });

  it('marks a failed request and does not retry it', async () => {
    GetAcpTranscript.mockImplementation(() =>
      Promise.reject(new Error('no control')),
    );
    requestAcpTranscript('s1');
    await flush();
    expect(acpStore.getState().byId.get('s1')?.failed).toBe(true);
    expect(acpStore.getState().byId.get('s1')?.requested).toBe(false);
    requestAcpTranscript('s1');
    await flush();
    expect(GetAcpTranscript).toHaveBeenCalledTimes(1);
  });

  it('a session list drops dead ids and clears the rest for a refetch', async () => {
    GetAcpTranscript.mockImplementation(() =>
      Promise.reject(new Error('no control')),
    );
    requestAcpTranscript('s1');
    requestAcpTranscript('gone');
    await flush();
    applyAcpFrame(msg({ reset: true, items: [] }));
    resetAcpOnSessionList(new Set(['s1']));
    const { byId } = acpStore.getState();
    expect(byId.has('gone')).toBe(false);
    expect(byId.get('s1')).toMatchObject({ requested: false, failed: false });
    expect(byId.get('s1')?.tx.loaded).toBe(false);
  });

  it('applyAcpError ignores codes that are not refusals, and unknown sessions', () => {
    applyAcpFrame(msg({ reset: true, items: [] }));
    noteSentPrompt('s1', 'go');
    applyAcpError({ code: 'permission_stale', session_id: 's1' });
    applyAcpError({ code: 'something_else', session_id: 's1' });
    applyAcpError({ code: 'acp_busy' });
    applyAcpError({ code: 'acp_busy', session_id: 'nobody' });
    const { byId } = acpStore.getState();
    expect(byId.get('s1')).toMatchObject({ sent: 'go', returned: null });
    expect(byId.has('nobody')).toBe(false);
  });

  it('applyAcpError returns a pending prompt and fails a pending snapshot together', () => {
    requestAcpTranscript('s1');
    noteSentPrompt('s1', 'go');
    applyAcpError({ code: 'session_dead', session_id: 's1' });
    expect(acpStore.getState().byId.get('s1')).toMatchObject({
      sent: null,
      returned: 'go',
      requested: false,
      failed: true,
    });
  });

  it('applyAcpError does not fail a transcript that is already loaded', () => {
    applyAcpFrame(msg({ reset: true, items: [] }));
    applyAcpError({ code: 'acp_failed', session_id: 's1' });
    expect(acpStore.getState().byId.get('s1')?.failed).toBe(false);
  });

  it('a pending prompt survives a different user turn, not a reconnect', () => {
    applyAcpFrame(msg({ reset: true, items: [] }));
    noteSentPrompt('s1', 'go');
    applyAcpFrame(
      msg({ items: [{ id: 1, kind: 'user', text: 'other', origin: 'user' }] }),
    );
    expect(acpStore.getState().byId.get('s1')?.sent).toBe('go');
    // Its refusal could only come on the old connection, which is gone.
    resetAcpOnSessionList(new Set(['s1']));
    expect(acpStore.getState().byId.get('s1')?.sent).toBeNull();
    applyAcpError({ code: 'session_dead', session_id: 's1' });
    expect(acpStore.getState().byId.get('s1')?.returned).toBeNull();
  });

  it('forgetAcp deletes the entry', () => {
    applyAcpFrame(msg({ reset: true, items: [] }));
    expect(acpStore.getState().byId.has('s1')).toBe(true);
    forgetAcp('s1');
    expect(acpStore.getState().byId.has('s1')).toBe(false);
  });
});

describe('applyAcp bounds', () => {
  it('keeps unchanged items by identity across a delta', () => {
    const t = loaded();
    const next = applyAcp(
      t,
      msg({ items: [{ id: 2, kind: 'agent', text: 'lo', append: true }] }),
    );
    expect(next.items[0]).toBe(t.items[0]);
    expect(next.items[1]).not.toBe(t.items[1]);
  });

  it('refetches rather than show a chunk for an item it does not hold', () => {
    const t = applyAcp(
      loaded(),
      msg({ items: [{ id: 99, kind: 'agent', text: 'frag', append: true }] }),
    );
    expect(t.loaded).toBe(false);
  });

  it('keeps at most MAX_ITEMS, dropping the oldest', () => {
    const many = Array.from({ length: MAX_ITEMS }, (_, i) => ({
      id: i + 1,
      kind: 'agent',
      text: `m${i + 1}`,
    }));
    const t = applyAcp(emptyAcp(), msg({ reset: true, items: many }));
    const next = applyAcp(
      t,
      msg({ items: [{ id: MAX_ITEMS + 1, kind: 'agent', text: 'new' }] }),
    );
    expect(next.items).toHaveLength(MAX_ITEMS);
    expect(next.items[0].id).toBe(2);
    expect(next.items.at(-1)?.text).toBe('new');
  });
});

describe('pending prompt matching', () => {
  const SID2 = 'p1';
  const seed = (
    items: { id: number; kind: string; text?: string; origin?: string }[],
  ) => {
    acpStore.setState({ byId: new Map() });
    applyAcpFrame({ session_id: SID2, epoch: 1, reset: true, items });
  };
  const entry = () => acpStore.getState().byId.get(SID2);

  it('is not matched by an identical older turn in a snapshot', () => {
    seed([]);
    noteSentPrompt(SID2, 'again');
    applyAcpFrame({
      session_id: SID2,
      epoch: 1,
      reset: true,
      items: [{ id: 1, kind: 'user', text: 'again', origin: 'user' }],
    });
    expect(entry()?.sent).toBe('again');
    applyAcpError({ code: 'acp_busy', session_id: SID2 });
    expect(entry()?.returned).toBe('again');
  });

  it('is matched by the daemon-clipped form of a long prompt', () => {
    seed([]);
    const long = `${'é'.repeat(40000)}tail`;
    noteSentPrompt(SID2, long);
    const clipped = `${'é'.repeat(32767)}�\n\n[… truncated by Hive]`;
    applyAcpFrame({
      session_id: SID2,
      epoch: 1,
      items: [{ id: 1, kind: 'user', text: clipped, origin: 'user' }],
    });
    expect(entry()?.sent).toBeNull();
    applyAcpError({ code: 'session_dead', session_id: SID2 });
    expect(entry()?.returned).toBeNull();
  });
});
