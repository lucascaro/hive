// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Terminal font-size constants + clamp helper.

export const DEFAULT_FONT_SIZE = 14;
export const MIN_FONT_SIZE = 8;
export const MAX_FONT_SIZE = 32;

// `n` is `unknown`, not `number`: callers feed it parsed localStorage
// (`state.ts:43`) and arithmetic on possibly-absent state, so garbage is part
// of the contract, not a caller bug. `typeof` does the narrowing —
// `Number.isFinite` is typed `(n: unknown) => boolean` and narrows nothing.
export function clampFont(n: unknown): number {
  if (typeof n !== 'number' || !Number.isFinite(n)) return DEFAULT_FONT_SIZE;
  return Math.max(MIN_FONT_SIZE, Math.min(MAX_FONT_SIZE, Math.round(n)));
}
