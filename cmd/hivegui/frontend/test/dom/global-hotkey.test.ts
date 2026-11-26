// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// @vitest-environment jsdom
//
// Spec 553, src/app/global-hotkey.ts: the GUI keeps the daemon's copy of
// the dictation hotkey in step with keymap.json, and reports focus.
//
//   • nothing is sent until the daemon has said what it holds
//   • a DICTATION whose chord equals the keymap sends nothing (no echo loop)
//   • a differing one — a hand-edited or imported keymap — is corrected
//   • a keymap change is sent once, even before the echo arrives
//   • unbinding the command disables the hotkey
//   • window focus reports the active session
//   • the Handy switch is tracked and never triggers a chord send
import { beforeEach, describe, expect, it, vi } from 'vitest';

const handlers = new Map<string, (p: unknown) => void>();
const SetGlobalHotkey = vi.fn(
  (_k: number, _m: number, _e: boolean): Promise<void> => Promise.resolve(),
);
const ReportFocusedSession = vi.fn(
  (_id: string): Promise<void> => Promise.resolve(),
);

vi.mock('../../src/bridge.js', () => ({
  EventsOn: (name: string, fn: (p: unknown) => void) => handlers.set(name, fn),
  SetGlobalHotkey: (...a: [number, number, boolean]) => SetGlobalHotkey(...a),
  ReportFocusedSession: (id: string) => ReportFocusedSession(id),
}));
vi.mock('../../src/lib/platform.js', () => ({
  isMac: true,
  isWindows: false,
  dictationSupported: true,
}));

const { appStore, setKeymap, setActiveId } = await import(
  '../../src/store/store.js'
);
const {
  initGlobalHotkey,
  _resetGlobalHotkeyForTest,
  getHandyAutoStart,
  subscribeHandyAutoStart,
} = await import('../../src/app/global-hotkey.js');

const DEFAULT = { key_code: 49, modifiers: 0x1800, enabled: true };
const deliver = (h: object, handy = false) =>
  handlers.get('dictation:settings')?.(
    JSON.stringify({ hotkey: h, handy_auto_start: handy }),
  );

beforeEach(() => {
  _resetGlobalHotkeyForTest();
  handlers.clear();
  SetGlobalHotkey.mockClear();
  ReportFocusedSession.mockClear();
  setKeymap({});
  initGlobalHotkey();
});

describe('dictation hotkey reconcile', () => {
  it('sends nothing before the daemon reports, nor when it already agrees', () => {
    setKeymap({ mac: { 'dictate-hud': ['Mod+Shift+[KeyD]'] } });
    expect(SetGlobalHotkey).not.toHaveBeenCalled();
    setKeymap({});
    deliver(DEFAULT);
    expect(SetGlobalHotkey).not.toHaveBeenCalled();
  });

  it('corrects a daemon copy that differs from the keymap', () => {
    setKeymap({ mac: { 'dictate-hud': ['Mod+Shift+[KeyD]'] } });
    deliver(DEFAULT);
    expect(SetGlobalHotkey).toHaveBeenCalledTimes(1);
    expect(SetGlobalHotkey).toHaveBeenCalledWith(0x02, 0x0300, true);
    // The echo of what was sent settles it: no second send.
    deliver({ key_code: 0x02, modifiers: 0x0300, enabled: true });
    expect(SetGlobalHotkey).toHaveBeenCalledTimes(1);
  });

  it('sends a keymap change once, even before the echo', () => {
    deliver(DEFAULT);
    const rebound = { mac: { 'dictate-hud': ['Mod+Shift+[KeyD]'] } };
    setKeymap(rebound);
    setKeymap({ ...rebound });
    expect(SetGlobalHotkey).toHaveBeenCalledTimes(1);
  });

  it('disables the hotkey when the command is unbound', () => {
    deliver(DEFAULT);
    setKeymap({ mac: { 'dictate-hud': [] } });
    expect(SetGlobalHotkey).toHaveBeenCalledWith(0, 0, false);
  });
});

describe('focus reports', () => {
  it('reports the active session when the window gains focus', () => {
    setActiveId('s7');
    window.dispatchEvent(new Event('focus'));
    expect(ReportFocusedSession).toHaveBeenCalledWith('s7');
  });

  it('reports nothing with no active session', () => {
    setActiveId(null);
    window.dispatchEvent(new Event('focus'));
    expect(ReportFocusedSession).not.toHaveBeenCalled();
    expect(appStore.getState().activeId).toBeNull();
  });
});

describe('Handy auto-start switch', () => {
  it('is unknown until the daemon reports, then follows it', () => {
    expect(getHandyAutoStart()).toBeNull();
    const seen: (boolean | null)[] = [];
    const off = subscribeHandyAutoStart(() => seen.push(getHandyAutoStart()));
    deliver(DEFAULT, false);
    deliver(DEFAULT, true);
    deliver(DEFAULT, true); // unchanged: no notification
    off();
    expect(seen).toEqual([false, true]);
  });

  it('a switch-only change sends no chord', () => {
    deliver(DEFAULT, false);
    deliver(DEFAULT, true);
    expect(SetGlobalHotkey).not.toHaveBeenCalled();
  });
});
