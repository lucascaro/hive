import { describe, it, expect } from 'vitest';
import {
  applyAcp,
  emptyAcp,
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
