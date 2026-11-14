// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Redaction (spec 529, B-5). Everything the engine serves or pushes, events
// and node outputs alike, is redacted. The raw output a condition or a prompt
// reads lives in a separate column nothing serves. Patterns, not entropy: this
// removes the credentials an agent or a check is likely to print, and does
// not promise more.
//
// Every pattern but the PEM block is local to one line: the stream layer
// (check.ts, Coalescer) relies on that, and redacts only whole lines.

import { truncateEncoded } from './size.ts';

const MARK = '[redacted]';

const PATTERNS: RegExp[] = [
  // A PEM private key, up to its END line, or to the end of the text when truncated.
  /-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?(?:-----END [A-Z ]*PRIVATE KEY-----|$)/g,
  /-----BEGIN [A-Z ]*KEY-----/g,
  /\bAKIA[0-9A-Z]{16}\b/g,
  /\bgh[pousr]_[A-Za-z0-9]{36,}/g,
  /\bgithub_pat_[A-Za-z0-9_]{22,}/g,
  /\bsk-[A-Za-z0-9_-]{20,}/g,
  /\bxox[abpr]-[A-Za-z0-9-]{10,}/g,
  /Authorization["']?[ \t]*[:=][ \t]*\S+.*/gi,
  /\b(?:Bearer|Basic)[ \t]+[A-Za-z0-9._~+/=-]{16,}/gi,
  /["']?\b(?:auth_?token|access_?token|api_?key|secret|token)["']?[ \t]*[:=][ \t]*["'][^"'\s]{8,}["']/gi,
  // Credentials in a URL: scheme://user:password@host.
  /\b[a-z][a-z0-9+.-]*:\/\/[^\s/:@]+:[^\s/@]+@/gi,
];

export function redactString(s: string): { text: string; count: number } {
  let count = 0;
  let text = s;
  for (const re of PATTERNS) {
    text = text.replace(re, () => {
      count++;
      return MARK;
    });
  }
  return { text, count };
}

/** A copy of `v` with every string redacted, and how many replacements were made. Keys are left alone. */
export function redactValue<T>(v: T): { value: T; count: number } {
  let count = 0;
  const walk = (x: unknown): unknown => {
    if (typeof x === 'string') {
      const r = redactString(x);
      count += r.count;
      return r.text;
    }
    if (Array.isArray(x)) return x.map(walk);
    if (typeof x === 'object' && x !== null) {
      const out: Record<string, unknown> = {};
      for (const [k, val] of Object.entries(x)) out[k] = walk(val);
      return out;
    }
    return x;
  };
  return { value: walk(v) as T, count };
}

/**
 * Redacts, then cuts to `max` encoded bytes. The order is the rule: a secret
 * that straddles the cut must be replaced whole first, or its front half is
 * stored in the clear. Every place that shortens a value that might hold one
 * goes through this.
 */
export function redactCut(s: string, max: number): string {
  return truncateEncoded(redactString(s).text, max).text;
}
