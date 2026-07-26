# Phase 1 — Unblock CI: the flaky e2e-real suite (P1)

**Why first:** the Linux and macOS CI legs (then named `CI (Linux)` /
`CI (macOS)`, now a single `CI` matrix) fail on unchanged `main` (5/8 and
4/8 recent runs). Every red check on every PR is untrustworthy. Spec:
`docs/product-specs/245-flaky-e2e-real-suite-blocks-every-pr.md`.

All failures cluster in three specs:
- `cmd/hivegui/frontend/test/e2e-real/wheel-scroll.spec.js`
- `cmd/hivegui/frontend/test/e2e-real/scroll-codex.spec.js`
- `cmd/hivegui/frontend/test/e2e-real/scroll-restream-strand.spec.js`

## Step 1 — Immediate: quarantine (same day, tiny diff)

Tag the three specs with an annotated skip-on-CI (or a separate non-required
Playwright project) so `main` is green again and red means "your diff broke
something." Keep them runnable locally via `npm run test:e2e:real`.

Do this even if you start Step 2 immediately — an undiagnosed P1 flake should
not stay a required gate while being diagnosed.

## Step 2 — Root cause the setup-fragility

Known concrete thread (from spec 245): `scroll-codex.spec.js` ~:244/:286
asserts `expect(baseY).toBeGreaterThan(4500)` as a *precondition* — it needs
"output filled the scrollback cap" but relies on wall-clock output volume, so
under CI CPU contention the test fails its own setup (`got 1624`), not its
invariant.

Fix pattern (likely applies to all three specs):
- Replace volume-by-time preconditions with **poll-until-condition**: pump
  output in a loop until the buffer reports ≥ cap (with a generous timeout),
  or `test.skip()` with a reason if the precondition can't be reached.
- This matches the prior lesson in memory/spec history: cap-trim scroll bugs
  only reproduce with a FULL buffer — a sub-cap run is a false pass, so the
  precondition must be enforced, not assumed.

Diagnosis rule (repo policy): no patches without a reproducer. Reproduce the
failure locally first, e.g. under CPU throttling:
`taskset -c 0` (Linux) or run with `--workers=1` plus a background CPU hog, and
loop the spec 10–20×.

## Step 3 — Re-gate

Per spec 245 success criteria: 10 consecutive green runs on unchanged `main`
before the suite returns to required status. Then remove the quarantine.

Re-gate **per spec, not all-or-nothing.** Only `scroll-codex` has a diagnosed
root cause (the `baseY > 4500` precondition); `wheel-scroll` and
`scroll-restream-strand` are assumed to share it but that's unverified. Bring
each spec off quarantine when *it* has 10 green runs, so a still-flaky third
spec can't re-block every PR the moment the other two are fixed.

## Explicitly not in scope

- Bumping `retries` beyond 1 as the "fix" — that hides the flake, doesn't fix
  the setup fragility, and doubles CI time on every real failure.
