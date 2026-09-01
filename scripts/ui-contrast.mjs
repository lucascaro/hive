#!/usr/bin/env node
// WCAG 2.1 contrast gate for the theme presets. No dependencies: it parses the
// two token files with a regex rather than a CSS AST because the input is a
// flat list of `--name: value;` declarations we control, and a real parser
// would be a dependency for no extra correctness.
//
// Rules (docs/design-docs/ui/themes.md):
//   --fg        on --surface  >= 4.5   body text
//   --fg-muted  on --surface  >= 4.5   subtitles/labels are real text too
//   --fg-subtle on --surface  >= 3.0   decorative/disabled
//   --fg        on --bg       >= 4.5   the app ground, not just panels
//   --term-fg   on --term-bg  >= 4.5   terminal default pair
//   --on-accent on --accent   >= 4.5   primary buttons must read their own label
// color-mix()/var() values that don't resolve to a hex are skipped, loudly.
//
// Run via `scripts/ui-lint.sh --contrast [--verbose] [path ...]`, which is the
// single entry point CI uses. With no paths it checks the real token files.
import { readFileSync } from 'node:fs';

// Defaults are the real token files. Explicit paths override them, which
// is what lets the CI self-test point the checker at a known-bad and a
// known-good fixture — without that, this gate only ever runs against
// tokens that currently pass, so a regression in hex()/ratio()/the
// light-ground branch would turn it into a rubber stamp silently.
// Mirrors how ui-lint.sh takes explicit targets.
const DEFAULT_FILES = [
  'cmd/hivegui/frontend/src/theme/tokens.css',
  'cmd/hivegui/frontend/src/theme/themes.css',
];
const args = process.argv.slice(2);
const fileArgs = args.filter((a) => !a.startsWith('--'));
const FILES = fileArgs.length ? fileArgs : DEFAULT_FILES;
const PAIRS = [
  ['--fg', '--surface', 4.5],
  ['--fg-muted', '--surface', 4.5],
  ['--fg-subtle', '--surface', 3],
  ['--fg', '--bg', 4.5],
  ['--term-fg', '--term-bg', 4.5],
  ['--on-accent', '--accent', 4.5],
];
// The ANSI 16 are checked only on a light ground. On a dark one, ANSI 0
// ("black") is meant to be invisible on the background — every terminal
// works that way — so a blanket rule would fail every dark preset for
// doing the right thing. On a light ground the same slot is the DARKEST
// colour and the rule bites where it should: xterm's defaults put seven
// of sixteen under AA on white, brightWhite at 1.16:1.
const ANSI_MIN = 4.5;
const LIGHT_GROUND = 0.5; // relative luminance above which --term-bg is "light"

// Every :root / :root[data-theme="x"] block, in source order. The base :root
// block in tokens.css seeds every preset, matching the cascade: a preset that
// omits a token inherits the default, and that inherited value is what the
// browser will actually paint, so it is what we must check.
function blocks(css) {
  const out = [];
  const re = /:root(?:\[data-theme=["']([^"']+)["']\])?\s*\{([^}]*)\}/g;
  for (let m; (m = re.exec(css)); ) {
    const decls = {};
    for (const d of m[2].matchAll(/(--[a-z0-9-]+)\s*:\s*([^;]+);/gi)) {
      decls[d[1]] = d[2].trim();
    }
    out.push({ name: m[1] || null, decls });
  }
  return out;
}

function hex(value, decls, depth = 0) {
  if (!value) return null;
  const v = value.trim();
  if (/^#[0-9a-f]{3}$/i.test(v))
    return `#${[...v.slice(1)].map((c) => c + c).join('')}`.toLowerCase();
  if (/^#[0-9a-f]{6}$/i.test(v)) return v.toLowerCase();
  const ref = v.match(/^var\((--[a-z0-9-]+)\)$/i);
  if (ref && depth < 4) return hex(decls[ref[1]], decls, depth + 1);
  return null; // color-mix, rgba, transparent, keywords
}

const lum = (h) => {
  const [r, g, b] = [1, 3, 5]
    .map((i) => parseInt(h.slice(i, i + 2), 16) / 255)
    .map((c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4));
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
};
const ratio = (a, b) => {
  const [hi, lo] = [lum(a), lum(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
};

const verbose = args.includes('--verbose');
const all = FILES.flatMap((f) => blocks(readFileSync(f, 'utf8')));
const base = all.find((b) => b.name === null)?.decls ?? {};
const presets = all.filter((b) => b.name);
if (!presets.length) {
  console.error('ui-contrast: no [data-theme] blocks found — did the file move?');
  process.exit(1);
}

let failed = 0;
const line = (ok, name, what, fg, bg, r, min) => {
  if (!ok) failed++;
  if (!ok || verbose) {
    console.log(
      `${ok ? 'ok  ' : 'FAIL'}  ${name.padEnd(13)} ${what}` +
        `  ${fg}/${bg}  ${r.toFixed(2)}:1 (need ${min})`,
    );
  }
};

for (const p of presets) {
  const decls = { ...base, ...p.decls };
  for (const [fgName, bgName, min] of PAIRS) {
    const fg = hex(decls[fgName], decls);
    const bg = hex(decls[bgName], decls);
    if (!fg || !bg) {
      if (verbose) console.log(`skip  ${p.name} ${fgName}/${bgName} (not a hex value)`);
      continue;
    }
    line(ratio(fg, bg) >= min, p.name, `${fgName} on ${bgName}`, fg, bg, ratio(fg, bg), min);
  }

  const termBg = hex(decls['--term-bg'], decls);
  if (!termBg) continue;
  if (lum(termBg) < LIGHT_GROUND) {
    if (verbose) console.log(`skip  ${p.name} ANSI (dark --term-bg ${termBg})`);
    continue;
  }
  for (let i = 0; i < 16; i++) {
    const c = hex(decls[`--ansi-${i}`], decls);
    if (!c) {
      console.log(`FAIL  ${p.name.padEnd(13)} --ansi-${i} is not set on a light ground`);
      failed++;
      continue;
    }
    line(ratio(c, termBg) >= ANSI_MIN, p.name, `--ansi-${i} on --term-bg`, c, termBg, ratio(c, termBg), ANSI_MIN);
  }
}
console.log(`ui-contrast: ${presets.length} preset(s), ${failed} failure(s)`);
process.exit(failed ? 1 : 0);
