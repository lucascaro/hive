# Worktrees, overlays and the launcher ignore the chosen theme

- **Spec:** [docs/product-specs/258-worktrees-and-overlays-ignore-the-theme.md](../../product-specs/258-worktrees-and-overlays-ignore-the-theme.md)
- **Issue:** —
- **PR:** #313
- **Branch:** stone-light
- **Status:** completed

## Summary

Retire the 31 `/* ui-lint: allow */` colour literals the design system left in
`src/theme/components/`, add the one token the worktree kind ramp needs, give
`ui-lint` the radius rule phase 6 wished it had, and extend the per-preset
screenshot loop to the two surfaces this touches.

## Research

Measured on this branch (2026-09-01):

- `git grep -n 'ui-lint: allow' cmd/hivegui/frontend/src/theme/` — 31 lines
  across 8 component files; 16 in `worktrees.css`.
- `src/theme/tokens.css` — the token layer. `--state-*` today is
  `running / attention / starting / exited / error`. No informational hue.
- `src/theme/themes.css` — five preset blocks plus the `hive-dark` marker.
  Header rule: every block re-values every token; partial presets fall through.
- `src/lib/worktrees.ts:82` — `WorktreeKind = 'main' | 'active' | 'holding' |
  'idle'`. `active` means *a session is running in it*; `holding` means
  *deleting it loses work*; `merged` (a badge, not a kind) means *safe to
  sweep*.
- `src/theme/components/field.css` — `.hv-input` is
  `background: var(--surface-raised)`, which is the precedent for the two
  literal `#141414` input fills in `launcher.css`.
- `scripts/ui-contrast.mjs` — `PAIRS` checks six fg/bg pairs plus the ANSI 16 on
  a light ground. **No `--state-*` token is checked at all.**
- Measured ratios of the current state colours on each preset's `--surface`:

  | preset | `--state-running` | `--state-attention` | `--state-error` |
  |---|---|---|---|
  | hive-dark | 10.28 | 9.01 | 6.62 |
  | classic | 8.69 | 9.22 | 9.75 |
  | **hive-light** | **3.45** | **3.27** | 5.06 |
  | native-dark | 6.91 | 8.26 | 4.76 |
  | native-light | 4.51 | 4.51 | 5.23 |
  | terminal | 7.21 | 11.59 | 5.46 |

  `hive-light` fails AA on two of them. That does not matter while the tokens
  only fill 8px state icons (decorative, >= 3:1), but this change paints 11px
  text with them, so it has to be fixed first or the migration *introduces* a
  contrast regression.
- `scripts/ui-lint.sh` — three rules (hex / px-size / glyph), bash-3.2-safe, no
  PCRE. Literal `border-radius` values in the tree today: four hairline `1/2/3px`
  (commented as deliberate) and four `50%` circles.
- `test/e2e/theme.spec.ts:149` — the per-preset loop, `sidebar` + `dialog`,
  gated behind `HIVE_SNAPSHOT`.

## Approach

Three groups, in order. The mechanical group is a pure substitution and is
verified by a computed-style dump being unchanged in `hive-dark`; the design
group is where the actual decisions are; the tooling group is independent.

### 1. Token layer first

- Fix `hive-light`'s two failing state colours: `--state-running`
  `#1f9d6a` -> `#177a53` (5.32:1), `--state-attention` `#d9731a` -> `#a35f0d`
  (5.00:1, the value `native-light` already ships).
- Add **one** new token, `--state-info` — "in use / informational, no action
  needed", the missing neutral member of the `--state-*` family. Values:
  `hive-dark` and `classic` `#7fb3d5` (the blue the worktree ramp already
  used), `native-dark` `#6cb6e8`, `hive-light` and `native-light` `#1163a8`,
  `terminal` `#b4b4b4`. Worst ratio on `--surface` is 5.61:1.
- Extend `ui-contrast.mjs`'s `PAIRS` with the four state colours on `--surface`
  at 4.5, so the gate covers what this change starts painting text with.
- Document all of it in `docs/design-docs/ui/tokens.md`.

### 2. The 31 literals

**Mechanical (19).** Mapped by role, not by hex:

| site | literal | token |
|---|---|---|
| `worktrees.css` section rule, row border | `#1e1e1e` | `--border` |
| `worktrees.css` empty card text | `#999` | `--fg-muted` |
| `worktrees.css` row ground | `#141414` | `--surface-raised` |
| `worktrees.css` status, subject | `#777`, `#808080` | `--fg-muted` |
| `worktrees.css` action button label | `#ccc` | `--fg` |
| `launcher.css` search, palette input | `#eee` | `--fg` |
| `launcher.css` worktree-toggle label | `#ccc` | `--fg` |
| `launcher.css` branch + rename fills | `#141414` | `--surface-raised` |
| `command-palette.css` placeholder | `#555` | `--fg-subtle` |
| `help-overlay.css` heading, definition | `#777`, `#aaa` | `--fg-muted` |
| `phase-overlay.css` dead card | `#0d0d0d` | `--surface-raised` |
| `phase-overlay.css` dead title, subtitle | `#fff`, `#aaa` | `--fg`, `--fg-muted` |
| `phase-overlay.css` focus-ring blend endpoint | `#fff` | `--fg` |
| `sidebar.css` new-project button | `#ccc` | `--fg` |

The status/subject pair collapses onto one token on purpose: the existing
comment says the subject is set apart by *italics, not by being dimmer*, and
`#808080` was chosen as the 4.5:1 floor on `#141414` — a floor a token makes
unnecessary.

**Design (12).** Each folds onto a state token, deriving borders and fills with
one `color-mix()` per use site (`tokens.md`'s stated cap):

- `active` -> `--state-info`. Status text direct; row border
  `color-mix(--state-info 30%, --border)`.
- `holding` -> `--state-attention`, same shape.
- `main` row ground -> `--surface`, so the repo's own checkout reads *flush with
  the panel* while every worktree is a raised card. Cheaper and more legible
  than a second near-black.
- merged badge -> `--state-running` (text), `color-mix(… 40%, --border)`
  (border), `color-mix(… 15%, transparent)` (fill — this also retires the
  `rgba(22, 101, 52, .18)` the hex rule never saw).
- destructive button -> `--state-error` (text), `color-mix(… 22%, transparent)`
  (hover fill). `tokens.md` already names destructive actions as
  `--state-error`'s role.
- `.hints.mismatch` -> `color-mix(--state-attention 55%, --fg-muted)`. The
  comment demands it stay quieter than the banner; mixing toward `--fg-muted`
  is how "quieter" survives a preset change.
- `.choice-dialog-bullets` -> `--state-attention` direct.

**Why one new token and not three.** `active`, `holding` and `merged` are three
distinct facts shown in one panel, and only two hues exist for them. `holding`
is literally attention and `merged` is literally a success signal, so both fold.
`active` is the leftover: folding it onto `--state-running` would make green
mean both "a session is running here" and "already merged, safe to delete" on
rows that can be both at once. One general-purpose informational token in the
`--state-*` family is the smaller price than a colour that means two things.

### 3. Tooling

- `ui-lint.sh`: a **radius** rule, matching the px-size rule's shape —
  `border-radius:\s*[0-9.]+px` outside `tokens.css`, honouring
  `ui-lint: allow`. Deliberately px-only, so the four `50%` circles are exempt
  by construction rather than by suppression. Add a `border-radius: 8px` line to
  `scripts/testdata/ui-lint/bad.css`. The four hairline radii get `allow`
  comments.
- `theme.spec.ts`: add `worktrees` and `launcher` to the per-preset loop. The
  worktrees panel needs the mock to answer `Worktrees` — reuse whatever
  `test/e2e/worktrees.spec.ts` already seeds rather than inventing a fixture.

## Files to change

- `cmd/hivegui/frontend/src/theme/tokens.css` — `--state-info`
- `cmd/hivegui/frontend/src/theme/themes.css` — `--state-info` x5, two
  `hive-light` re-values
- `cmd/hivegui/frontend/src/theme/components/{worktrees,launcher,command-palette,help-overlay,phase-overlay,sidebar,choice-dialog,hints}.css`
- `cmd/hivegui/frontend/src/theme/components/{project-card,session-row}.css` —
  `allow` on the hairline radii
- `scripts/ui-lint.sh`, `scripts/ui-contrast.mjs`,
  `scripts/testdata/ui-lint/bad.css`
- `docs/design-docs/ui/tokens.md`
- `cmd/hivegui/frontend/test/e2e/theme.spec.ts` (+ new baseline PNGs)
- `.changesets/258-theme-tokens-everywhere.md`

## Tests

- Existing `theme.spec.ts` "every preset paints its own tokens" extended to
  assert `--state-info` resolves per preset (live cascade, all platforms).
- New standing guard: under `hive-light`, `.worktree-row`'s computed background
  is light — the assertion that would have failed before this change.
- `scripts/ui-lint.sh --strict scripts/testdata/ui-lint/bad.css` must report the
  radius violation; `good.css` must stay at 0.
- `scripts/ui-lint.sh --contrast` with the state pairs added.
- Per-preset screenshots for worktrees + launcher, darwin-local.
- Computed-style dump across the seven surfaces under all six presets, before
  and after, to prove the `hive-dark` path is inert and to see what the other
  five actually changed.

## Decision log

- **2026-09-01** — One new token, `--state-info`, not three and not zero. Why:
  the worktree panel shows three facts at once — `active` ("a session is running
  in it"), `holding` ("deleting this loses work") and the `merged` badge ("safe
  to sweep") — and the token layer had two hues for them. `holding` is literally
  `--state-attention` and `merged` is literally a success signal, so both fold.
  `active` is the leftover: folding it onto `--state-running` too would make
  green mean both "running here" and "already merged" on rows that are
  frequently both. A per-site `--worktree-active` would be the regime that
  produced 51 literals; a general informational member of the `--state-*` family
  is not.
- **2026-09-01** — `hive-light`'s `--state-running` and `--state-attention` were
  re-valued (`#1f9d6a` -> `#177a53`, `#d9731a` -> `#a35f0d`) *before* any
  component was migrated. Why: measured 3.45:1 and 3.27:1 on that preset's white
  `--surface`. That was legal while the state tokens only filled 8px icons
  (decorative, >= 3:1); this change makes them colour 11px text, so migrating
  first would have replaced a theme bug with a contrast bug. `--state-attention`
  landed on the value `native-light` already ships rather than a new hex.
- **2026-09-01** — `ui-contrast.mjs` now checks all four state colours on
  `--surface` at 4.5:1. Why: the gate phase 6 shipped had no `--state-*` pair at
  all, which is exactly why `hive-light` could ship at 3.27:1 and stay green.
  Adding the rule is what stops the next preset from doing it again. Worst
  passing ratio across the six presets is 4.51:1 (`native-light`).
- **2026-09-01** — `.worktree-row[data-kind='main']` is `var(--surface)`, not a
  second ground. Why: the fact it encodes is "this is the checkout, not a
  sweepable worktree". Painting it flush with the panel while every other row is
  a raised card says that with the two grounds the preset already defines,
  instead of inventing a third value that then needs six.
- **2026-09-01** — `.worktree-status` and `.worktree-subject` collapse onto one
  `--fg-muted`. Why: the existing comment says the subject is set apart by
  *italics, not by being dimmer*, and the two literals it had (`#777` / `#808080`)
  contradicted that — `#808080` was picked as the 4.5:1 floor against a
  hard-coded `#141414`, a floor that stops existing once the ground is a token.
- **2026-09-01** — `.dead-title` drops its literal white for `var(--fg)`. Why:
  emphasis there is weight and size; white was invisible on a light preset's
  card. The `:focus-visible` ring's `color-mix` endpoint moved from `#fff` to
  `var(--fg)` for the same reason — the endpoint exists to pull the session
  colour away from the card behind it, and on a light ground white pulls it the
  wrong way. Both were `allow`-commented as "not a themed role"; both were.
- **2026-09-01** — `.hints.mismatch` is
  `color-mix(in srgb, var(--state-attention) 55%, var(--fg-muted))`, not a
  fourth amber. Why: its rule is "attention, but quieter than the stale-daemon
  banner", and only a derivation keeps meaning "quieter" under six presets.
  Measured 5.87:1 (`native-light`) to 9.38:1 (`terminal`) on `--surface`.
- **2026-09-01** — The merged badge's `rgba(22, 101, 52, 0.18)` fill went with
  the hexes even though `ui-lint`'s hex rule never saw it. Why: an `rgba()`
  literal ignores `[data-theme]` exactly as much as a `#hex` does; the rule's
  blind spot is not a licence. It is now
  `color-mix(in srgb, var(--state-running) 15%, transparent)`.
- **2026-09-01** — The radius rule matches `border-radius: <n>px` only, so the
  four `border-radius: 50%` circles need no suppression. Why: a circle is not a
  scale step and never wanted to be one; a rule that flagged it would have
  invited four `allow` comments that say nothing. The four sub-scale hairlines
  (1px drag indicators, the 2px project swatch, the 3px worktree badge) do get
  `allow`, which is the only remaining use of the escape hatch in `src/theme/`.
- **2026-09-01** — Verified by a computed-style dump of seven surfaces under all
  six presets, before and after (the method the phase-6 log recommends). Every
  one of the ~90 changed properties per preset traces to a declared
  substitution; nothing else moved. The dump is what confirmed the bug as
  reported: under `hive-light` every `.worktree-row` was
  `rgb(20, 20, 20)` before and `rgb(248, 248, 251)` after, with the main row at
  `rgb(16, 16, 16)` -> `rgb(255, 255, 255)`.
- **2026-09-01** — The new standing guard reads `.worktree-name`'s colour, not
  the row's. Why: the row declares no `color`, so `getComputedStyle(row).color`
  returns the `--fg-muted` the dialog body hands down — the first version of the
  assertion failed on that, correctly.
- **2026-09-01 (review iter 1)** — The contrast gate's first version checked the
  state family on `--surface` only, and `--surface` is not where two of the four
  text uses sit. Measured: the destructive row action paints `--state-error` on
  `--sel` (native-dark 3.46:1 — *worse* than the `#e08585` literal it replaced,
  4.17:1; hive-light 4.37:1), and the merged badge sat on its own
  `color-mix(--state-running 15%, transparent)` tint (hive-light 4.10:1). A gate
  that reports green over the text it exists to protect is worse than no gate.
  Fixed three ways: `PAIRS` gained the family on `--surface-raised` (the row-card
  ground) plus `--state-error` on `--sel`; `hive-light --state-error` moved to
  `#bd3030` and `native-dark --state-error` to `#ee9090`; and the merged badge
  lost its tint, so its ground is the row's `--surface-raised`, which *is*
  gated. The tint had to go rather than be re-valued — `ui-contrast.mjs` cannot
  resolve `color-mix()`, so any hue-tinted ground is unverifiable by
  construction.
- **2026-09-01 (review iter 1)** — `--state-error` is gated on `--sel` alone, not
  the whole family on every ground. Why: a pair for a combination nothing
  renders would constrain the palette for nothing. The rule tracks the sites.
- **2026-09-01 (review iter 1)** — Both fixture files gained the state tokens.
  Why: `ui-contrast.mjs` treats an unresolvable token as a bare `continue`, so
  with no `--state-*` in `good.css` the two fixtures behaved identically whether
  the new pairs were present, misspelled or deleted — the self-test could not
  tell a working rule from a dead one. `bad.css` gained `fixture-dim-state`
  (`#1f9d6a` at 3.45:1 on white — the real bug that moved hive-light) and
  `fixture-dim-danger` (`#e06c6c` at 3.46:1 on `--sel`, which the `--surface`
  pair alone passes). Each fails for exactly one reason, per that file's header.
- **2026-09-01 (review iter 1)** — The radius rule now strips comments and
  matches the longhands and multi-value shorthands
  (`border-top-left-radius: 4px`, `border-radius: 0 0 4px 4px`). Nothing in the
  tree hits either today, which is the point: the rule exists for what gets
  written next.
- **2026-09-01 (review iter 1)** — Re-valuing two `--state-error`s and dropping
  the badge tint moved *no* screenshot baseline until the PNGs were deleted and
  regenerated. Cause: `toHaveScreenshot`'s default per-pixel `threshold` (0.2
  YIQ) absorbs a small hue shift before `maxDiffPixels: 0` ever counts it. Worth
  writing down — these baselines catch a token falling through to the wrong
  preset, not a colour moving a few steps. The exact-value guards remain the
  contrast gate and the computed-style tests, exactly as the phase-6 log says.
- **2026-09-01 (review iter 2)** — The destructive action's hover tint is gone;
  it keeps the shared `var(--hover)` its sibling buttons use. Why: the tint was
  `color-mix(--state-error 22%, transparent)`, so the red label sat on a ground
  made of its own hue — hive-light 3.87:1, native-light 4.05:1, terminal 4.04:1,
  native-dark 4.24:1 — and because the ground is a `color-mix()` the gate skips
  it. Same defect class as iteration 1's finding, one ground further in: the
  only surface that failed was the only one nothing could check. The rule now is
  simply that a state colour is never painted on a ground tinted with itself.
  The label stays red; the affordance does not need the fill.
- **2026-09-01 (review iter 2)** — Dropped the `--state-*` length assertion from
  the per-preset token test. Why: `tokens.css`'s base `:root` declares all four,
  so reading them off `documentElement` returns an inherited value for a preset
  that omits one — the assertion could not fail and its comment claimed it
  could. `ui-contrast.mjs` merges the base block into every preset and does
  catch that case, which is now what the comment says.
- **2026-09-01 (gate)** — The merge gate failed acceptance criterion 4 and it
  was right: iteration 2 deleted the `--state-*` assertion for being vacuous but
  put nothing in its place, and `ui-contrast.mjs` cannot stand in for it — that
  script regex-parses `tokens.css`/`themes.css`, so it validates the *values*
  and says nothing about whether a rule still points at them. Closed with a new
  ungated, all-platforms block: for each of the six presets, the four sites that
  spell a state out in words are asserted to paint the colour the preset
  resolves for their token, compared through a probe element so the browser does
  the notation conversion. Mutation-checked — reverting
  `.worktree-row[data-kind='active'] .worktree-status` to the literal `#7fb3d5`
  fails it on four presets (hive-dark and classic legitimately value
  `--state-info` at that same hex, so they still pass, which is correct). The
  contrast gate stays green through that mutation, which is the point: the two
  checks cover different halves and neither substitutes for the other.
- **2026-09-01** — Two existing baselines changed as well as twelve being added:
  `sidebar-hive-light` and `sidebar-native-light`. Why: the session-state icons
  are filled with `--state-running`, and `hive-light`'s value moved. Expected,
  and the reason the re-valuing was done as its own step.

## PR convergence ledger

- **2026-09-01 iter 1** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: c6151570d5c5dce221aa402fdc5caa8ca81a5d30e18c171eb62d9edf7cb2d9b2; threads_open: 0; action: continue (three IMPORTANT findings taken rather than stopping on a no-thread COMMENT); head_sha: 82e4c00.
- **2026-09-01 iter 1 (follow-up)** — all three IMPORTANT findings and the one MINOR fixed on the branch; gates re-run green; the six worktrees baselines regenerated.
- **2026-09-01 iter 2** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: b9ad6144c9ef5ea88f17f9fd891c9f71b499f2dfa5323cd05b44861fecdebcfc; threads_open: 0; action: continue (one IMPORTANT + two MINOR taken rather than stopping on a no-thread COMMENT); head_sha: 563bd95.
- **2026-09-01 iter 3** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop (three MINOR comment/whitespace nits taken before the gate); head_sha: 516c386. CI fully green; the spec-257 flake did not fire.

## Gate verdict

- **2026-09-01** — verdict: FAIL; checks: 1 dimension passed / 2 failed / 0 followups; followups: none (PR open — fixed on the branch, which is the point of gating pre-merge); one-line: acceptance criterion 4 had no test behind it after review iteration 2 deleted a vacuous one, and the changeset described only half the colour moves.
  - 2026-09-01 dimensions:
    - acceptance — FAIL — five of six criteria observable on HEAD. Criterion 4 ("a resolved value under all six presets, asserted from the live cascade by a Playwright test rather than by reading the CSS") had nothing behind it for `--state-info`: `grep -rn state-info cmd/hivegui/frontend/test/` returned no matches, and `ui-contrast.mjs` cannot stand in — it regex-parses `tokens.css`/`themes.css`, so it validates values and not whether a rule still points at them.
    - non-goals — PASS — all four respected. Also flagged that the literal `main...HEAD` range is wrong in this worktree: the local `main` ref is 2 commits stale, dragging ~2600 lines of the merged zustand refactor into the range. `origin/main...HEAD` is the correct range and was used throughout.
    - doc accuracy — FAIL on the changeset only; the other nine checks passed, including six decision-log contrast ratios recomputed by hand and matched exactly. The changeset said only hive-light's running/attention moved, omitting hive-light's `--state-error` (`#bd3030`), native-dark's (`#ee9090`, a *dark* preset getting *lighter*) and the merged badge's tint removal.

- **2026-09-01** — verdict: PASS; checks: 3 dimensions passed / 0 failed / 0 followups; followups: none; one-line: both failures fixed on the branch and independently re-verified, including a reproduced mutation proof; all gates green at `e121f09`.
  - 2026-09-01 dimensions (re-run):
    - acceptance — PASS — criterion 4 closed by an ungated, all-platforms block in `theme.spec.ts` that asserts each of the four state-word sites paints the colour its preset resolves for the token, compared through a probe element. Re-verified by mutation *by the validator, not by the author*: hard-coding `.worktree-row[data-kind='active'] .worktree-status` to `#7fb3d5` fails 4 of 6 presets, with hive-dark and classic correctly still passing because their `--state-info` is that same hex. `--contrast` stays green through the mutation, which is what makes the two checks complementary rather than redundant.
    - non-goals — PASS — re-run after the three follow-up commits. No production `.ts`/`.tsx`/`.go`/`.html` touched, no ANSI/xterm value moved, `PRESETS` and the pre-paint list untouched, exactly four `allow` comments (down from 30 on `origin/main`), no CSS geometry property added or removed, no generated file edited.
    - doc accuracy — PASS — changeset now names both `--state-error` moves and the badge tint removal, verified against `git show HEAD:themes.css` and `HEAD:worktrees.css`.

  Caught by the gate and fixed in the PR, which is why it runs pre-merge: (1) criterion 4's missing test, (2) the incomplete changeset, (3) `test-results/.last-run.json` committed at the repo root — Playwright writes artifacts relative to CWD and the ignore rule only covered `/cmd/hivegui/frontend/test-results/`, so a run invoked from the root left an unignored copy that `git add -A` swept in. File removed, both paths ignored.

  Observed and deliberately NOT actioned here: `docs/exec-plans/active/ui-design-system-phase{1,6}.md` still quote retired hex literals. Pre-existing, outside this diff, and they are historical planning records for a shipped programme — editing them would falsify what was planned at the time. Worth a separate tidy-up (those phase plans arguably belong in `completed/` beside their umbrella); not this feature's to do.

## Progress

- [x] 1. Token layer (`--state-info`, hive-light fixes, contrast gate, tokens.md)
- [x] 2. Mechanical substitutions
- [x] 3. Design substitutions
- [x] 4. ui-lint radius rule
- [x] 5. Screenshot loop + baselines
- [x] 6. Gates + PR
