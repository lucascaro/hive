// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// @vitest-environment jsdom
//
// Spec 575, src/app/global-hotkey.ts off macOS: Windows keeps the
// daemon's copy of the chord in step and reports focus, with its own
// default (Ctrl+Shift+Space); Linux, which has no dictation box, does
// neither.
//
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
let windows = true;
vi.mock('../../src/lib/platform.js', () => ({
  isMac: false,
  get isWindows() {
    return windows;
  },
  get dictationSupported() {
    return windows;
  },
}));

const { setKeymap, setActiveId } = await import('../../src/store/store.js');
const { initGlobalHotkey, _resetGlobalHotkeyForTest } = await import(
  '../../src/app/global-hotkey.js'
);

// The daemon's default: ⌃⌥Space, kVK 49 with Control|Option.
const DAEMON_DEFAULT = { key_code: 49, modifiers: 0x1800, enabled: true };
const deliver = (h: object) =>
  handlers.get('dictation:settings')?.(
    JSON.stringify({ hotkey: h, handy_auto_start: false }),
  );

function boot(onWindows: boolean) {
  windows = onWindows;
  _resetGlobalHotkeyForTest();
  handlers.clear();
  SetGlobalHotkey.mockClear();
  ReportFocusedSession.mockClear();
  setKeymap({});
  initGlobalHotkey();
}

describe('Windows', () => {
  beforeEach(() => boot(true));

  it('replaces the daemon default with Ctrl+Shift+Space (Control|Shift, kVK 49)', () => {
    deliver(DAEMON_DEFAULT);
    expect(SetGlobalHotkey).toHaveBeenCalledTimes(1);
    expect(SetGlobalHotkey).toHaveBeenCalledWith(49, 0x1000 | 0x0200, true);
  });

  it('reads a rebind in the PC grammar: Mod is Ctrl', () => {
    deliver(DAEMON_DEFAULT);
    SetGlobalHotkey.mockClear();
    setKeymap({ other: { 'dictate-hud': ['Mod+Alt+[KeyD]'] } });
    expect(SetGlobalHotkey).toHaveBeenCalledWith(0x02, 0x1000 | 0x0800, true);
  });

  it('reports the focused session', () => {
    setActiveId('s7');
    window.dispatchEvent(new Event('focus'));
    expect(ReportFocusedSession).toHaveBeenCalledWith('s7');
  });
});

describe('Linux', () => {
  beforeEach(() => boot(false));

  it('never sends a chord and reports no focus', () => {
    deliver(DAEMON_DEFAULT);
    setActiveId('s7');
    window.dispatchEvent(new Event('focus'));
    expect(SetGlobalHotkey).not.toHaveBeenCalled();
    expect(ReportFocusedSession).not.toHaveBeenCalled();
  });
});
