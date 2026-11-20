// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// The pinned-responses store (spec 566): header pages merge newest
// first, and a delta landing between two pages is neither lost nor
// counted twice.

// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('../../src/bridge.js', () => ({
  GetPins: vi.fn(() => Promise.resolve()),
  GetPinBody: vi.fn(() => Promise.resolve()),
  GetActivity: vi.fn(() => Promise.resolve()),
}));

import * as bridge from '../../src/bridge.js';
import { forgetActivity } from '../../src/store/activity.js';
import {
  applyPinBody,
  applyPinsChanged,
  applyPinsPage,
  forgetPins,
  pinBody,
  pinsStore,
  requestPins,
  resetPinsOnSessionList,
  type Pin,
} from '../../src/store/pins.js';

const SID = 's1';
const pin = (n: number): Pin => ({
  id: n.toString(16).padStart(24, '0'),
  title: `pin ${n}`,
  created_at: n,
  body_len: 1,
});
const titles = () =>
  (pinsStore.getState().byId.get(SID)?.headers ?? []).map((p) => p.title);
const entry = () => pinsStore.getState().byId.get(SID);
const flush = () => Promise.resolve().then(() => Promise.resolve());

beforeEach(() => {
  pinsStore.setState({ byId: new Map(), collapsed: false });
  vi.mocked(bridge.GetPins).mockReset();
  vi.mocked(bridge.GetPins).mockImplementation(() => Promise.resolve());
});

describe('pins store', () => {
  it('merges pages newest first and marks the entry loaded on done', async () => {
    requestPins(SID);
    await flush();
    expect(bridge.GetPins).toHaveBeenCalledWith(SID);
    applyPinsPage({ session_id: SID, pins: [pin(5), pin(4)], done: false });
    expect(entry()?.loaded).toBe(false);
    applyPinsPage({ session_id: SID, pins: [pin(3)], done: true });
    expect(titles()).toEqual(['pin 5', 'pin 4', 'pin 3']);
    expect(entry()?.loaded).toBe(true);
    // Loaded: no second request.
    requestPins(SID);
    await flush();
    expect(bridge.GetPins).toHaveBeenCalledTimes(1);
  });

  it('ignores pages nobody asked for', () => {
    applyPinsPage({ session_id: SID, pins: [pin(1)], done: true });
    expect(titles()).toEqual([]);
  });

  it('dedupes an add delta that is also in a later page', () => {
    requestPins(SID);
    applyPinsPage({ session_id: SID, pins: [pin(3)], done: false });
    applyPinsChanged({ session_id: SID, added: pin(9) });
    applyPinsPage({ session_id: SID, pins: [pin(9), pin(2)], done: true });
    expect(titles()).toEqual(['pin 9', 'pin 3', 'pin 2']);
  });

  it('keeps a pin removed between pages out of the later page', () => {
    requestPins(SID);
    applyPinsPage({ session_id: SID, pins: [pin(5)], done: false });
    applyPinsChanged({ session_id: SID, removed_ids: [pin(2).id, pin(5).id] });
    applyPinsPage({ session_id: SID, pins: [pin(3), pin(2)], done: true });
    expect(titles()).toEqual(['pin 3']);
  });

  it('after a clear mid-load, drops the stale pages but keeps later adds', () => {
    requestPins(SID);
    applyPinsPage({ session_id: SID, pins: [pin(5)], done: false });
    applyPinsChanged({ session_id: SID, cleared: true });
    applyPinsChanged({ session_id: SID, added: pin(9) });
    applyPinsPage({ session_id: SID, pins: [pin(4)], done: true });
    expect(titles()).toEqual(['pin 9']);
    expect(entry()?.loaded).toBe(true);
    // The next load is not still ignoring pages.
    applyPinsChanged({ session_id: SID, added: pin(10) });
    expect(titles()).toEqual(['pin 10', 'pin 9']);
  });

  it('applies add, remove and clear deltas once loaded', () => {
    requestPins(SID);
    applyPinsPage({ session_id: SID, pins: [pin(2), pin(1)], done: true });
    applyPinsChanged({ session_id: SID, added: pin(3) });
    expect(titles()).toEqual(['pin 3', 'pin 2', 'pin 1']);
    applyPinsChanged({ session_id: SID, removed_ids: [pin(2).id] });
    expect(titles()).toEqual(['pin 3', 'pin 1']);
    applyPinsChanged({ session_id: SID, cleared: true });
    expect(titles()).toEqual([]);
  });

  it('caches bodies only for pins it knows, and drops them on removal', () => {
    requestPins(SID);
    applyPinsPage({ session_id: SID, pins: [pin(1)], done: true });
    applyPinBody({ session_id: SID, id: pin(1).id, body: 'b1' });
    applyPinBody({ session_id: SID, id: pin(7).id, body: 'stray' });
    expect(entry()?.bodies.get(pin(1).id)).toBe('b1');
    expect(entry()?.bodies.has(pin(7).id)).toBe(false);
    applyPinsChanged({ session_id: SID, removed_ids: [pin(1).id] });
    expect(entry()?.bodies.size).toBe(0);
  });

  it('pinBody resolves from the cache or once the body arrives', async () => {
    requestPins(SID);
    applyPinsPage({ session_id: SID, pins: [pin(1)], done: true });
    const p = pinBody(SID, pin(1).id);
    applyPinBody({ session_id: SID, id: pin(1).id, body: 'later' });
    await expect(p).resolves.toBe('later');
    await expect(pinBody(SID, pin(1).id)).resolves.toBe('later');
  });

  it('survives forgetActivity (a restart), and refetches after a reconnect', async () => {
    requestPins(SID);
    applyPinsPage({ session_id: SID, pins: [pin(1)], done: true });
    forgetActivity(SID);
    expect(titles()).toEqual(['pin 1']);
    resetPinsOnSessionList(new Set([SID]));
    expect(entry()?.loaded).toBe(false);
    expect(titles()).toEqual(['pin 1']);
    requestPins(SID);
    await flush();
    expect(bridge.GetPins).toHaveBeenCalledTimes(2);
    resetPinsOnSessionList(new Set());
    expect(entry()).toBeUndefined();
  });

  it('a reload drops pins removed while this client was not listening', () => {
    requestPins(SID);
    applyPinsPage({ session_id: SID, pins: [pin(2), pin(1)], done: true });
    resetPinsOnSessionList(new Set([SID]));
    requestPins(SID);
    // A pin added during the reload survives the first page.
    applyPinsChanged({ session_id: SID, added: pin(5) });
    applyPinsPage({ session_id: SID, pins: [pin(2)], done: true });
    expect(titles()).toEqual(['pin 5', 'pin 2']);
  });

  it('forgets a removed session', () => {
    applyPinsChanged({ session_id: SID, added: pin(1) });
    forgetPins(SID);
    expect(entry()).toBeUndefined();
  });
});
