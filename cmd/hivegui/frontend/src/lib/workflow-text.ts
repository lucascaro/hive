// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Text from the workflow engine is untrusted: a workflow file name, a
// human prompt, an agent's reply and a test runner's output all come from
// files the user is only about to trust, or from an agent. React escapes
// markup; these helpers cover what escaping does not -- terminal escape
// sequences and characters that reorder or hide text.

// C0 + DEL + C1, the bidi embeddings/overrides/isolates and marks, line and
// paragraph separators, zero-width characters and the BOM.
// biome-ignore lint/complexity/useRegexLiterals: a literal would put raw invisible characters in the source
const INVISIBLE = new RegExp(
  '[\\p{Cc}\\p{Cf}\\p{Zl}\\p{Zp}\\u00AD\\u034F\\u115F\\u1160\\u17B4\\u17B5\\u180E\\u3164\\uFFA0\\uFE00-\\uFE0F\\u{E0100}-\\u{E01EF}]',
  'gu',
);

const escapeOf = (ch: string): string =>
  `\\u{${(ch.codePointAt(0) ?? 0).toString(16).toUpperCase().padStart(4, '0')}}`;

/** Makes controls and bidi/invisible characters visible as `\u{XXXX}`, so a
 * file name cannot reorder or hide what the user is trusting. `keep` lists
 * characters left alone (a prompt keeps its newlines and tabs). */
export function showControls(s: string, keep = ''): string {
  return s.replace(INVISIBLE, (ch) => (keep.includes(ch) ? ch : escapeOf(ch)));
}

/** Plain engine text for display: controls made visible, newlines and tabs
 * kept. */
export const plainText = (s: string): string => showControls(s, '\n\t');

// biome-ignore-start lint/suspicious/noControlCharactersInRegex: matching control characters is the point
// OSC ends at BEL or ST; CSI is ESC [ params intermediates final; the 8-bit
// forms (U+009B, U+009D) are the same.
const OSC =
  /(?:\u001B\]|\u009D)[^\u0007\u001B\u009C]*(?:\u0007|\u001B\\|\u009C)?/g;
const CSI = /(?:\u001B\[|\u009B)[0-?]*[ -/]*[@-~]/g;
// DCS, SOS, PM and APC strings run to ST; their payload is not text.
const STRING_ESC = /\u001B[PX^_][^\u001B]*(?:\u001B\\)?/g;
// Any other ESC form: optional intermediates, then a final byte.
const ESC2 = /\u001B[ -/]*[0-~]/g;
// biome-ignore-end lint/suspicious/noControlCharactersInRegex: matching control characters is the point

/** "2 min ago" for an RFC 3339 time; the empty string when unparseable. */
export function ago(iso: string, now: number = Date.now()): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return '';
  const s = Math.max(0, Math.round((now - t) / 1000));
  if (s < 60) return 'just now';
  const m = Math.round(s / 60);
  if (m < 60) return `${m} min ago`;
  const h = Math.round(m / 60);
  if (h < 24) return `${h} h ago`;
  const d = Math.round(h / 24);
  return d === 1 ? 'yesterday' : `${d} days ago`;
}

/** "2.1 KB" for a byte count. */
export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

/** Removes terminal escape sequences from a program's output, then shows
 * every remaining control or bidi character (newlines and tabs kept; CRLF
 * becomes a newline). */
export function stripCsi(s: string): string {
  return plainText(
    s
      .replace(OSC, '')
      .replace(CSI, '')
      .replace(STRING_ESC, '')
      .replace(ESC2, '')
      .replace(/\r\n/g, '\n')
      .replace(/\r/g, ''),
  );
}
