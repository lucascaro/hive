// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// The only way to render a key hint. patterns.md › Keyboard hints:
// mono, --text-xs, --fg-subtle, no border, no fill; format ([1] for
// digits/symbols, (n) for letters) is the caller's, per AGENTS.md.
export function Kbd({ children }: { children: string }) {
  return <kbd className="hv-kbd">{children}</kbd>;
}
