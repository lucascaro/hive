// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// The bridge stub for vi.mock's factory. Kept apart from workflows-harness
// on purpose: the factory awaits this module while the bridge is being
// mocked, so it must not import anything that imports the bridge.

import { vi } from 'vitest';

export interface Hoisted {
  send: ReturnType<typeof vi.fn>;
  notify: ReturnType<typeof vi.fn>;
  setPluginEnabled: ReturnType<typeof vi.fn>;
  answerPermission: ReturnType<typeof vi.fn>;
  handlers: Map<string, (...a: unknown[]) => void>;
}

export const makeHoisted = (): Hoisted => ({
  send: vi.fn(),
  notify: vi.fn(() => Promise.resolve()),
  setPluginEnabled: vi.fn(() => Promise.resolve()),
  answerPermission: vi.fn(() => Promise.resolve()),
  handlers: new Map(),
});

/** Every bridge export as a resolved no-op, except the ones observed. */
export async function stubBridge(
  h: Hoisted,
  importOriginal: <T>() => Promise<T>,
): Promise<Record<string, unknown>> {
  const actual = await importOriginal<Record<string, unknown>>();
  const out: Record<string, unknown> = {};
  for (const k of Object.keys(actual)) out[k] = vi.fn(() => Promise.resolve());
  out.SendPluginMessage = h.send;
  out.Notify = h.notify;
  out.SetPluginEnabled = h.setPluginEnabled;
  out.AnswerPermission = h.answerPermission;
  out.EventsOn = (name: string, handler: (...a: unknown[]) => void) => {
    h.handlers.set(name, handler);
  };
  return out;
}
