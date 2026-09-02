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
//   --state-*   on --surface  >= 4.5   state colours are text, not just icons
//                                      (the worktree kind ramp's status lines)
//   --state-*   on --surface-raised    the same words on a row card / popover
//               >= 4.5                 ground (worktree rows, merged badge)
//   --state-error on --sel    >= 4.5   the destructive row action's button fill
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
  // The state family is checked on --surface because that is the ground
  // every panel that spells a state out in words sits on. It was icon-only
  // when phase 6 wrote this list — 8px shapes, decorative, >= 3:1 — and
  // hive-light shipped --state-running at 3.45:1 and --state-attention at
  // 3.27:1 as a result. Both are text now.
  ['--state-running', '--surface', 4.5],
  ['--state-attention', '--surface', 4.5],
  ['--state-error', '--surface', 4.5],
  ['--state-info', '--surface', 4.5],
  // A worktree row is a --surface-raised card and the state words sit on
  // it, not on the panel behind it. Checking only --surface would have
  // reported green over the merged badge at 4.10:1 on hive-light.
  ['--state-running', '--surface-raised', 4.5],
  ['--state-attention', '--surface-raised', 4.5],
  ['--state-error', '--surface-raised', 4.5],
  ['--state-info', '--surface-raised', 4.5],
  // The one state colour painted on a filled control. Kept to --sel
  // alone rather than the whole family on every ground: a pair for a
  // combination nothing renders would constrain the palette for nothing.
  ['--state-error', '--sel', 4.5],
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

// Presets that MUST be checked, whatever the CSS says. The community presets
// (spec 305) carry `--contrast-exempt: 1` because they are ported at their
// published values and several do not clear AA — they are opt-in, and a user
// who picks "Dracula" wants Dracula. That opt-out is a loaded gun pointed at
// this gate, so the six themes a user can land on WITHOUT opting in are named
// here and the marker is refused on them. `system` resolves to hive-dark or
// hive-light, so those two are the real defaults; the rest ship in the picker
// above the community section.
//
// What this does NOT do: stop a FUTURE first-party preset from exempting
// itself, since a name absent from this list is unprotected. Closing that
// properly means teaching a Node script which PRESETS entries are `group:
// 'Community'` — a third copy of the preset registry across a language
// boundary, with nothing keeping it in sync (the repo already pays for the
// two copies it has, and guards them with an e2e test). The judgement is that
// a wrong exemption is a reviewable one-line diff that prints a `skip` line
// on every subsequent run, whereas a silent sync drift is neither. themes.md
// § "Adding a preset" says plainly that a first-party preset must pass.
const NEVER_EXEMPT = new Set([
  'hive-dark',
  'hive-light',
  'native-dark',
  'native-light',
  'terminal',
  'classic',
]);

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

let exempt = 0;
for (const p of presets) {
  const decls = { ...base, ...p.decls };
  // Read from p.decls, not the merged view: an exemption has to be declared by
  // the preset itself. Merging base first would let one stray declaration in
  // tokens.css switch the whole gate off for every preset at once.
  // `=== '1'`, not truthiness: the value is a raw declaration string, so a
  // typo'd `--contrast-exempt: 0` (or `false`, or `no`) would otherwise opt
  // the preset out while reading, to anyone skimming the CSS, as if it had
  // opted in. One spelling, and anything else falls through to the gate.
  if (p.decls['--contrast-exempt']?.trim() === '1') {
    if (NEVER_EXEMPT.has(p.name)) {
      console.log(
        `FAIL  ${p.name.padEnd(13)} declares --contrast-exempt; ` +
          'a default preset may not opt out of the contrast gate',
      );
      failed++;
      continue;
    }
    // Opting out of the RATIOS does not opt out of being a complete preset.
    // Skipping straight to the next preset would have removed the only
    // check that catches a partial one: `decls` merges the base block in, so
    // an omitted token reads back as hive-dark's value everywhere — the
    // cascade paints it, getComputedStyle returns it, and the e2e "paints its
    // own tokens" test sees a plausible colour. Contrast was what caught that
    // on a light ground, and the exemption would have taken it away. So every
    // token the pair list names must still be declared by the preset itself.
    //
    // The ANSI 16 join that set on a LIGHT ground, mirroring the rule below:
    // there, an unset slot is a failure because the inherited value is
    // tokens.css's Tango palette, which is unreadable on white (brightWhite
    // lands at 1.16:1). Leaving them out here would have re-opened exactly
    // the hole this block exists to close, for exactly the presets most
    // likely to hit it — three of the ports on this branch are light. Dark
    // grounds are not required to re-declare, matching the ratio rule, which
    // exempts them because ANSI 0 is meant to vanish into the background.
    const termBgOwn = hex(p.decls['--term-bg'], p.decls);
    const required = new Set(PAIRS.flatMap(([fg, bg]) => [fg, bg]));
    if (termBgOwn && lum(termBgOwn) >= LIGHT_GROUND)
      for (let i = 0; i < 16; i++) required.add(`--ansi-${i}`);
    const missing = [...required].filter((t) => !(t in p.decls));
    if (missing.length) {
      console.log(
        `FAIL  ${p.name.padEnd(13)} exempt from ratios, not from completeness: ` +
          `missing ${missing.join(' ')}`,
      );
      failed++;
      continue;
    }
    exempt++;
    console.log(`skip  ${p.name.padEnd(13)} opted out (--contrast-exempt)`);
    continue;
  }
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
console.log(
  `ui-contrast: ${presets.length} preset(s), ${exempt} opted out, ${failed} failure(s)`,
);
process.exit(failed ? 1 : 0);
