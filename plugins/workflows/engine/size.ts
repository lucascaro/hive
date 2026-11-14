// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Message budgets are measured on the bytes the daemon will send, not on the
// bytes the plugin wrote (spec 529, Contract for C). The daemon re-encodes a
// plugin message with Go's encoding/json, which escapes `<`, `>`, `&` as
// six-byte \u00XX sequences and U+2028/U+2029 as six bytes where UTF-8 has
// three. So 16 KiB of `<` is 96 KiB on the wire.

/** The whole plugin message must fit A's 64 KiB cap; this leaves headroom. */
export const MAX_MESSAGE_BYTES = 48 << 10;

const enc = new TextEncoder();

/** Bytes of `JSON.stringify(v)` once Go has re-encoded it. */
export function encodedSize(v: unknown): number {
  const json = JSON.stringify(v);
  if (json === undefined) return 0;
  let n = enc.encode(json).length;
  for (let i = 0; i < json.length; i++) {
    const c = json.charCodeAt(i);
    if (c === 0x3c || c === 0x3e || c === 0x26) n += 5;
    else if (c === 0x2028 || c === 0x2029) n += 3;
  }
  return n;
}

/** The longest prefix of `s` whose JSON string form is at most `max` encoded bytes (never splits a surrogate pair). */
export function truncateEncoded(s: string, max: number): { text: string; truncated: boolean } {
  if (encodedSize(s) <= max) return { text: s, truncated: false };
  // A cut never lands between the halves of a surrogate pair, so the cost is monotonic in the cut.
  const aligned = (n: number): number => {
    const c = s.charCodeAt(n - 1);
    return n > 0 && c >= 0xd800 && c <= 0xdbff ? n - 1 : n;
  };
  let lo = 0;
  let hi = s.length;
  while (lo < hi) {
    const mid = (lo + hi + 1) >> 1;
    if (encodedSize(s.slice(0, aligned(mid))) <= max) lo = mid;
    else hi = mid - 1;
  }
  return { text: s.slice(0, aligned(lo)), truncated: true };
}
