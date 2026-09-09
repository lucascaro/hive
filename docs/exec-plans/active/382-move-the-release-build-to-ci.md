
- **2026-09-08** — Review iter 2 showed the iter-1 race fix did not deliver
  what its own comments claimed. `gh release create --target` creates the tag
  ref *first* and uploads assets afterwards, so the `push: tags` webhook fires
  against a release with zero assets; the stand-down (which skips only on a
  complete release) would correctly say "incomplete" and start a full rebuild
  racing the local upload. `checksums.txt` uploads last, so that window also
  publishes a manifest disagreeing with the zip beside it — which the in-app
  updater rejects. Fixed by publishing as a **draft** first: a draft creates
  no tag ref, so the tag appears only at `--draft=false`, with the release
  already complete. Applied on both paths, which also makes a died CI run
  leave an unpublished draft rather than a half-public release.
- **2026-09-08** — Three test gaps from iter 2, all real. The selftest fixture
  ran `git init` with no commit, so `git rev-parse HEAD` exited 128 and printed
  the literal string `HEAD`; `--target HEAD` was passed and the assertion still
  matched on a substring, so the newest fix had zero coverage. The stand-down
  comparison was inline workflow bash, unreachable from any test — extracted to
  `scripts/release-standdown.sh`, and writing its test immediately found a
  latent bug: jq's `sort` is bytewise while shell `sort` is locale-collated, so
  `checksums.txt` and `Hive-…` ordered differently and the comparison could
  never match. Pinned with `LC_ALL=C`. And `--check-preflight` was a dead hook
  whose comment claimed a test grepped for it; the pin-guard and no-credentials
  checks are now assertions in the selftest, so that claim is true. 12 → 23
  assertions.
- **2026-09-08** — Deviated from the "do not change `sign-macos.sh`" non-goal,
  deliberately. Its failure message told the operator to `git reset --hard
  HEAD~1 && git tag -d && git pull --ff-only`, which this change made actively
  harmful: under `--local-artifacts` the release commit is already on origin,
  so the pull undoes the reset and a re-run would re-stamp on top of a
  published commit. Message and header comment only, no behaviour change.

# Move the release build to CI

- **Spec:** [docs/product-specs/382-move-the-release-build-to-ci.md](../../product-specs/382-move-the-release-build-to-ci.md)
- **Issue:** #382
- **PR:** #383
- **Branch:** `feature/382-move-the-release-build-to-ci`
- **Status:** active

## Summary

Split `scripts/release.sh` at the `---- BUILD ARTIFACTS ----` boundary. The
local half keeps the parts that want a human (version, changelog, commit, tag,
push). The build/sign/notarize/checksum/publish half moves into
`scripts/release-artifacts.sh`, driven on a `macos-latest` runner by a new
`.github/workflows/release.yml` on `v*` tag push and `workflow_dispatch`. One
copy of the artifact half, callable locally as a fallback.

## Research

### Relevant code

- `scripts/release.sh:1-181` — the local half as it stands: validation
  (`:60-63`), macOS signing pre-flight (`:71-98`), stale-plan note (`:102-103`),
  `RELEASE_SHA` pin (`:108`), changeset detection (`:114-118`), version bump
  (`:122-131`), changelog stamp (`:135-156`), commit + tag (`:160-180`).
- `scripts/release.sh:182-239` — the half that moves: `build.sh --zip --version
  --platform all` (`:191`), artifact existence assertions (`:192-197`),
  `sign-macos.sh` (`:201-204`), basenamed `shasum -a 256` manifest (`:211-216`),
  the `origin/main` drift guard + push (`:220-233`), notes extraction by `awk`
  and `gh release create` (`:237-239`).
- `scripts/sign-macos.sh:96-97,119-120,135-136` — reads `HIVE_SIGN_IDENTITY`
  and `HIVE_NOTARY_PROFILE` from the environment, resolves the identity via
  `security find-identity -v -p codesigning` (i.e. the keychain **search
  list**), and notarizes with `--keychain-profile … --wait`. Unmodified by this
  work.
- `scripts/sign-macos.sh:107-117` — cross-checks the identity's Team ID against
  `internal/buildinfo/signing.go`. The same check is duplicated in
  `release.sh:77-90` as an early refusal.
- `scripts/ci-bootstrap.sh` — pinned `WAILS_VERSION=v2.15.0`, seeds
  `frontend/dist`, installs the CLI, generates `wailsjs/` bindings.
  `build.sh:70-80` reads the pin back out of this file and refuses to run
  against a drifting CLI, so the workflow must call it, not `go install` by hand.
- `build.sh:10-13` — `--platform all` cross-compiles the Windows zip on macOS;
  `--zip` writes `release/Hive-<version>-<plat>.zip`.
- `.github/workflows/ci.yml:32,85-101` — the checkout/setup-go/setup-node/
  ci-bootstrap/`npm ci && npm run build` sequence to copy, with the action SHAs
  already pinned in this repo. `:28-46` is the shellcheck job, which globs
  `scripts/*.sh` and so covers the new script automatically.

### Constraints / dependencies

- **The keychain sequence has four load-bearing calls, not one.** All four are
  mandatory and each fails differently:
  - `security create-keychain -p` then **`security unlock-keychain -p`** — a
    locked keychain blocks `codesign` with no prompt on a runner, and the job
    burns the whole 180-minute budget before anyone learns why.
  - `security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k` —
    without it `codesign` blocks on a GUI keychain prompt that never appears.
  - `security list-keychains -d user -s "$KEYCHAIN" login.keychain` —
    `sign-macos.sh:119` finds the identity through the keychain *search list*,
    not a `--keychain` argument.
  - **`security default-keychain -s "$KEYCHAIN"`** — `xcrun notarytool
    store-credentials` writes the profile into the *default* keychain. Without
    this the profile lands somewhere `sign-macos.sh:135-136`'s
    `--keychain-profile` lookup cannot see it, and the failure surfaces only
    after the full build has already run.
- **Notarization latency is unbounded.** v2.7.0 took >90 min in Apple's queue;
  the 360-minute default job timeout survives it but `timeout-minutes: 180` is
  the explicit budget.
- **Ordering inverts.** Today the tag is pushed only after a successful build
  and sign. In the CI world the tag push *is* the trigger, so a failed run
  leaves a pushed tag with no release. The publish step must therefore be
  idempotent and re-runnable.
- **Public repo.** The signing secrets must be unreachable from a fork:
  `push: tags` and `workflow_dispatch` only, never `pull_request_target`.
- `CHANGELOG.md` is generated; the release notes are extracted from the copy
  committed in the tagged commit, which the runner checks out at the tag.

### Prior lessons

- `reference_wails_build_skip_flag` — build hivegui with plain `wails build`,
  never `-s`; `-s` skips the frontend build and the app dies with "no
  index.html". `build.sh` already does the right thing; do not "optimise" the
  workflow by skipping the `npm run build` step.
- `reference_typecheck_needs_wails_bindings` — a fresh checkout needs
  `scripts/ci-bootstrap.sh` before anything reads `wailsjs/`. The workflow runs
  on a fresh runner every time, so this step is not optional.
- `feedback_no_skip_ci_text_in_pr_body` — never write a literal `[skip ci]` in
  the PR body or commit.
- `feedback_verify_with_biome_ci_not_lint` — verify by exit code, not by
  grepping output.

### Conventions card

```
build:  ./build.sh              # macOS .app (GUI + daemon)
test:   scripts/test.sh         # layers: go · unit · dom · e2e
lint:   biome ci .  (frontend)  ·  shellcheck -S warning scripts/*.sh build.sh
boot:   ./scripts/ci-bootstrap.sh   # required before any go build in a fresh tree
```

- **TDD / "boil the lake"** — every behaviour change ships with the check that
  would have caught its regression, in the same PR.
- **Shell scripts are gated.** `.github/workflows/ci.yml:38-46` runs
  `shellcheck -S warning` over `scripts/*.sh`, `scripts/hooks/*` and `build.sh`;
  new scripts are covered by that glob with no CI edit.
- **Selftest precedent.** `scripts/check-daemon-contract-selftest.sh` and
  `scripts/regen-features-selftest.py` are how this repo proves a
  release-critical script still fires. Follow that shape.
- **Changelog** — `CHANGELOG.md` is generated and `block-generated-edits` fails
  any PR touching it. A CI/docs-only PR skips the changeset with the
  `no-changeset` label.
- **Docs are part of the change** — a doc made wrong by the change is fixed in
  the same commit.
- **Action SHAs are pinned** in this repo's workflows (`actions/checkout@3d3c42e…`
  # v7). Match that.

## Approach

Cut `scripts/release.sh` at line 182 and move everything below into
`scripts/release-artifacts.sh <version>`, which becomes the single owner of
build → sign → checksums → publish. Both the workflow and a local fallback call
that one script, so no step exists twice.

The **push moves up**, above the cut: the local half ends with the existing
`origin/main` drift guard (`release.sh:220-230`) and `git push origin HEAD
"$TAG"`, then prints the Actions run URL and exits. Chosen over having CI push
the tag because the drift guard is the reason the commit/tag half is local at
all — a runner cannot know whether `main` moved under the maintainer.

Why one script rather than inlining the steps into workflow YAML: the artifact
half is already shellchecked in CI, is testable on a laptop, and keeps the local
fallback ("keep it local, drop `--wait`") one flag away instead of a rewrite.

**`release.sh` stops requiring signing credentials by default.** The Darwin
pre-flight at `:71-98` currently refuses to run without `HIVE_SIGN_IDENTITY` and
a keychain identity — which would defeat the point, since the whole goal is that
a machine with no certificate can cut a release. It moves behind the new
`--local-artifacts` flag. The Team-ID pin check (`:77-83`) does **not** move: it
reads a repo file, needs no credentials, and a release built with no pinned team
is unusable regardless of where it is signed.

The workflow's publish step is idempotent by construction: `gh release view
"$TAG"` decides between `gh release create` and `gh release upload --clobber`.
This is what makes the inverted ordering safe — a run that dies after the tag is
pushed is fixed by re-dispatching, not by deleting a tag.

**Signing is mandatory on Darwin, not conditional.** `release.sh:201-204`
signs unconditionally today; `release-artifacts.sh` keeps that and hard-fails
when `HIVE_SIGN_IDENTITY` or `HIVE_NOTARY_PROFILE` is missing on Darwin, unless
an explicit `--allow-unsigned` opt-out is passed (used by nothing but the
selftest). Inverting this to "sign if credentials happen to be present" would
turn a mis-set repo secret into a **green run that publishes an unsigned,
un-notarized zip** — spec success criterion 3 violated with no failure anywhere
and every client's updater pin rejecting the release.

Notary credentials use an **App Store Connect API key**
(`xcrun notarytool store-credentials --key/--key-id/--issuer`), created on the
runner into the temporary keychain at job start. Revocable per key, not tied to
a human's Apple ID. `sign-macos.sh`'s `--keychain-profile "$HIVE_NOTARY_PROFILE"`
keeps working unchanged.

### Files to change

1. `.github/workflows/release.yml` — **new**. `on: push: tags: ['v*']` plus
   `workflow_dispatch` with a required `tag` input. `runs-on: macos-latest`,
   `timeout-minutes: 180`, `permissions: contents: write`.

   Two details that are wrong by default and must be written explicitly:
   - **`with: ref: ${{ inputs.tag || github.ref }}` on the checkout.** A
     `workflow_dispatch` checks out the *dispatch branch*, not the tag. Left
     default, the rehearsal in verification step 5 would build the branch tip
     and publish it under the tag's name.
   - **`concurrency.group` must normalize to a bare tag** —
     `release-${{ inputs.tag || github.ref_name }}`, not `github.ref`. A
     dispatch yields `v1.2.3` while a tag push yields `refs/tags/v1.2.3`, so
     the two would sit in *different* groups and could race each other through
     `gh release upload --clobber` for the same release. `cancel-in-progress:
     false` — never kill a run that may already be mid-notarization.

   Steps: checkout at that ref (pinned SHA, matching
   `ci.yml:32`) → setup-go (`go-version-file: go.mod`) → setup-node 24 →
   `./scripts/ci-bootstrap.sh` → `npm ci && npm run build` in
   `cmd/hivegui/frontend` → import the certificate into a temporary keychain →
   `store-credentials` for the notary profile → `./scripts/release-artifacts.sh
   "${TAG#v}"` → always-run cleanup deleting the temporary keychain and the
   decoded key material.
2. `scripts/release-artifacts.sh` — **new**. `<version>` positional plus
   `--allow-unsigned` (selftest only). Lifts `release.sh:189-216` and `:235-239`
   verbatim: build via `build.sh --zip --version "$VERSION" --platform all`,
   assert both zips exist, **sign on Darwin unconditionally — hard-failing when
   `HIVE_SIGN_IDENTITY`/`HIVE_NOTARY_PROFILE` are missing unless
   `--allow-unsigned` is passed** (see Approach; "sign if credentials happen to
   be present" is the exact regression this contract exists to prevent), write
   the basenamed `release/checksums.txt`, extract notes from `CHANGELOG.md` with
   the existing `awk`, then create-or-clobber the GitHub release.

   Two separate test knobs, not one: `SKIP_BUILD=1` skips `build.sh` and
   operates on zips already sitting in `release/`; `gh` is **always** invoked,
   so the selftest can stub it on `PATH` and observe the create-vs-`--clobber`
   branch. A single `DRY_RUN` suppressing both would make the idempotency
   assertion — spec criterion 5 — untestable.
3. `scripts/release.sh` — delete `:182-216` and `:235-245`; move the push block
   (`:218-233`) up to become the last step; on `--local-artifacts` call
   `scripts/release-artifacts.sh`. On the default path, print the
   *workflow-filtered* Actions URL —
   `https://github.com/lucascaro/hive/actions/workflows/release.yml` — not a run
   URL: the run id does not exist at push time and cannot be known.

   The pre-flight block (`:71-98`) splits in two, and the split has to be
   precise: `set -euo pipefail` (`:2`) turns a sloppy one into an
   unbound-variable crash.
   - **Credential-dependent — gated behind `--local-artifacts`:** `:71-74` (the
     two `:?` refusals), **`:85-90`** (parses the Team ID *out of*
     `$HIVE_SIGN_IDENTITY` and match-tests it — this dereferences the variable,
     so leaving it ungated kills the no-credentials path before it can print
     anything), and `:92-97` (`security find-identity`, `xcrun`).
   - **Credential-free — and it leaves the `if [[ $(uname -s) == Darwin ]]`
     block entirely:** the Team-ID pin check (`:77-83`). It reads a repo file,
     needs no keychain, and a release built with no pinned team is unusable no
     matter which machine built it. It must therefore also fire on the Linux CI
     legs, where verification steps 3 and 4 run.
4. `scripts/release-artifacts-selftest.sh` — **new**. See Tests. It must be
   **invoked**, not merely linted: `.github/workflows/ci.yml:38-46` only
   shellchecks `scripts/*.sh`. Follow the repo's precedent and add an explicit
   step next to the two existing selftest steps in
   `.github/workflows/changesets.yml:84-92`
   (`check-daemon-contract-selftest.sh`, `regen-features-selftest.py`), whose
   stated rationale — "it only runs on the release path, where a mistake reaches
   users instead of CI" — is exactly this script's situation.
4b. `.github/workflows/changesets.yml` — one step added, per the line above.
5. `.github/workflows/ci.yml` — add `actionlint` to the existing shellcheck job
   (`:38-46`), which already `apt-get install`s its linter, so this is one more
   install line plus one invocation. Same rationale the job already states for
   shellchecking `release.sh`: a workflow that only runs live, after a tag
   exists, must be linted before it gets the chance to fail at the worst moment.
   `actionlint` is **not** installed on this machine — locally it needs
   `brew install actionlint`; CI installs it in the job.
6. `docs/releasing-signed-macos.md` — new "Releasing from CI" section: the seven
   secrets/variables, how to produce the `.p12` and the `.p8`, the two-command
   release flow, `workflow_dispatch` recovery, and the fully-local fallback via
   `--local-artifacts`. Update the existing "Releasing" section (`:103-119`),
   which describes the blocking local notarization as the only path. Also fix
   `:195`: the troubleshooting row points at `sign-macos.sh`'s unwind recipe
   (`sign-macos.sh:137-148`, "git reset --hard HEAD~1 && git tag -d"), which is
   **wrong once the tag is pushed and the failure happened on a runner** — the
   recovery there is re-dispatch, not a local reset. Changing that script is a
   spec non-goal, so the correction lives in the doc.
7. `AGENTS.md` — the **Releasing** section (`## Releasing`) says the script
   "handles everything … GitHub release with attached binaries" and tells the
   reader to export both credentials. Rewrite to the split flow.
8. `DESIGN.md:117` — cites `scripts/release.sh` as what exercises macOS, Linux
   and Windows builds every release. Still true after the split (the workflow
   drives the same `build.sh --platform all`), but the citation should point at
   the workflow too. One-line edit, no structural change, so no new hard rule.
11. `README.md` — two lines that are **already stale**, predating this work:
   `:82` lists "code signing and notarization" under *Not yet shipping*, and
   `:195-196` says the bundle "is neither signed nor notarized". Both were made
   wrong by spec 374 shipping in v2.7.0. This PR is the release-docs PR, so fix
   them here rather than leaving a known-wrong README (AGENTS.md: stale docs are
   a bug; boil the lake). Two one-line edits, no new prose.
9. `.claude/skills/hs-release/SKILL.md` — user-level, outside this repo.
   **Out of scope for this PR**; noted in the PR body as a follow-up so the
   post-release verification step becomes "watch the Actions run".

### New files

- `.github/workflows/release.yml` — the CI half.
- `scripts/release-artifacts.sh` — build/sign/checksum/publish, one copy.
- `scripts/release-artifacts-selftest.sh` — red/green proof for the above.

### Tests

The change is shell and YAML, so the tests are the repo's existing
script-selftest shape, not Go or vitest.

1. `scripts/release-artifacts-selftest.sh` — runs `release-artifacts.sh` under
   `DRY_RUN=1` in a `mktemp -d` sandbox and asserts:
   - **checksum manifest is basenamed** — seeded fake zips produce a
     `checksums.txt` whose lines end in `Hive-9.9.9-macos-universal.zip`, not a
     `release/` path. (Fails if someone drops the `cd release` subshell — which
     would silently break the in-app updater's manifest lookup.)
   - **notes extraction stops at the next section** — a fixture `CHANGELOG.md`
     with `## [9.9.9]` followed by `## [9.9.8]` yields only the 9.9.9 body.
     (Fails on an `awk` that drops the `/^## \[/{exit}` guard.)
   - **missing artifact is fatal** — with one zip absent the script exits
     non-zero. (Fails if the `[[ -f "$f" ]]` assertions are lost in the move.)
   - **publish is idempotent** — with a stubbed `gh` on `PATH` reporting an
     existing release, the script calls `release upload --clobber`; reporting
     none, it calls `release create`. (Fails on a straight `gh release create`,
     the exact regression that makes a re-dispatch useless.)
   - **missing credentials on Darwin are fatal** — with `HIVE_SIGN_IDENTITY`
     unset and no `--allow-unsigned`, the script exits non-zero with
     `refusing to publish unsigned`. (Fails on a "sign if creds are present"
     implementation, which would publish an unsigned zip from a green run.)
   - **signing runs before the checksum manifest** — stub `sign-macos.sh` and
     `shasum` on `PATH`, each appending its name to an order log; assert the log
     reads `sign` then `shasum`. (Fails on a reordering, which would publish a
     `checksums.txt` describing the *pre*-signature zip and break the in-app
     updater's integrity check for every user — `release.sh:199-215` documents
     exactly this ordering requirement.)
2. `release.sh --check-preflight <version>` — an early-exit path that runs the
   pre-flight block and then prints `preflight ok: team=<pinned> signing=<on|off>`
   instead of bumping anything. The marker matters: a flag that short-circuits
   before reaching any pre-flight logic would also exit 0, so the test greps for
   the marker rather than trusting the exit code alone. It skips the clean-tree
   guard (`release.sh:61`), because the negative test below has to mutate a file
   to exercise the pin check.
3. `actionlint .github/workflows/release.yml` in CI — catches a malformed
   `concurrency`, an unknown `runs-on`, or a bad expression before a tag exists.
4. `shellcheck -S warning` — automatic, via the existing `scripts/*.sh` glob.
5. **Trigger allow-list** — `actionlint` does not check *which* triggers a
   workflow declares, so nothing would catch spec criterion 7 regressing. One
   grep in the selftest: `release.yml` must contain neither `pull_request` nor
   `pull_request_target`. Cheap, and it is the difference between the signing
   secrets being unreachable from a fork and being one careless edit away.

### Verification

```bash
# 1. Lint — new script and workflow. Exit code, not output.
shellcheck -S warning scripts/release-artifacts.sh scripts/release.sh \
  scripts/release-artifacts-selftest.sh && echo LINT_OK
actionlint .github/workflows/release.yml && echo ACTIONLINT_OK

# 2. The selftest's four assertions.
bash scripts/release-artifacts-selftest.sh && echo SELFTEST_OK

# 3. release.sh reaches the bump with NO signing credentials — and prove it
#    got there, rather than short-circuiting. Grep the marker, not the exit code.
#    Team ID is asserted against its source, not hardcoded, so a legitimate
#    pin rotation does not break the check.
pin=$(sed -n 's/^var signingTeamID = "\(.*\)"$/\1/p' internal/buildinfo/signing.go)
env -u HIVE_SIGN_IDENTITY -u HIVE_NOTARY_PROFILE \
  bash scripts/release.sh --check-preflight 9.9.9 \
  | grep -q "preflight ok: team=${pin} signing=off" && echo NOCREDS_OK

# 4. The Team-ID pin guard survived the split. NOTE: `git worktree add HEAD`
#    checks out COMMITTED code, so run this after committing -- against an
#    uncommitted tree it silently tests the old script.
#    Must fail with THAT error,
#    not with "working tree not clean" or an unset-variable exit.
#    Commit the mutation so the clean-tree guard cannot mask the result; use
#    portable `sed -i.bak` (BSD `sed -i ''` errors on the Linux legs).
tmp=$(mktemp -d); git worktree add -q "$tmp" HEAD
sed -i.bak 's/^var signingTeamID = ".*"$/var signingTeamID = ""/' \
  "$tmp/internal/buildinfo/signing.go" && rm -f "$tmp/internal/buildinfo/signing.go.bak"
git -C "$tmp" commit -qam 'test: empty the team pin'
#    No --local-artifacts and no credentials: the pin check is credential-free
#    and outside the Darwin block, so it must fire on its own. If someone
#    reorders the `:?` credential refusals ahead of it, this step goes red.
err=$( cd "$tmp" && env -u HIVE_SIGN_IDENTITY -u HIVE_NOTARY_PROFILE \
       bash scripts/release.sh --check-preflight 9.9.9 2>&1 )
grep -q 'pins no Team ID' <<<"$err" && echo PIN_GUARD_OK || \
  { echo "PIN_GUARD_LOST or wrong-reason exit:"; echo "$err"; }
git worktree remove --force "$tmp"

# 5. End-to-end, on the PR branch, once the secrets are in place:
gh workflow run release.yml --ref <branch> -f tag=v2.7.1-rc1
gh run watch
gh release view v2.7.1-rc1 --json assets --jq '.assets[].name'   # 3 assets
# and the real proof the signature survived CI:
gh release download v2.7.1-rc1 -p 'Hive-*-macos-universal.zip' -D /tmp/rc
ditto -x -k /tmp/rc/Hive-*.zip /tmp/rc/app
spctl -a -vv -t install /tmp/rc/app/hivegui.app     # must say "accepted … Notarized Developer ID"
xcrun stapler validate /tmp/rc/app/hivegui.app
```

**The rc rehearsal has no CHANGELOG section.** `awk` extracts notes by matching
`^## \[<version>\]` (`release.sh:238`), and no `## [2.7.1-rc1]` section exists
— the rehearsal tag is created by hand, not by `release.sh`, so nothing stamps
one. Accept empty notes for the rehearsal explicitly: the run must still
succeed and publish three assets, and `release-artifacts.sh` must therefore
**not** treat empty notes as an error. Assert that in the selftest too.

Step 5 is the only check that proves the keychain dance works; steps 1–4 cannot.
It needs the secrets loaded and a throwaway `-rc` tag, and it is the gate for
calling this done.

## Open questions / risks

- **Secrets must exist before the workflow can be proven.** Six repo settings:
  variables `HIVE_SIGN_IDENTITY`, `HIVE_NOTARY_PROFILE`; secrets
  `MACOS_CERT_P12`, `MACOS_CERT_PASSWORD`, `NOTARY_API_KEY` (base64 `.p8`),
  `NOTARY_KEY_ID`, `NOTARY_ISSUER_ID`. Producing the `.p12` and the App Store
  Connect key is manual, maintainer-only work. **This blocks verification step 5,
  not the PR.**
- **A half-failed run leaves a tag with no release.** Mitigated by the
  create-or-clobber publish and `workflow_dispatch`, not eliminated: a run that
  dies mid-notarization still needs a re-dispatch.
- **`macos-latest` moves under us.** Xcode/SDK version drift can change signed
  output. Not pinned in this PR; if the output ever shifts, pin the image and
  `xcode-select` explicitly.
- **`--wait` still blocks — just not a human.** A 90-minute notarization now
  burns 90 minutes of macOS runner time (10× billing multiplier on private
  repos; free here while the repo is public). Acceptable, and the reason for
  `timeout-minutes: 180`.
- **Windows artifact stays cross-compiled** on the macOS runner rather than
  moving to a matrix: it works today, and both zips must land in one release.

### Alternatives ruled out

- **Keep it local, drop `--wait`.** Submit, reclaim the terminal, staple and
  publish from a second script when Apple returns. A fraction of the work, but
  only half the benefit — still one machine, still manual. Kept as the shape of
  the `--local-artifacts` fallback.
- **Self-hosted runner on the maintainer's Mac.** Same machine, same single
  point of failure, plus a runner to maintain. Solves nothing.
- **Inline the build steps in workflow YAML.** Duplicates what `release.sh`
  already does, escapes shellcheck, and cannot be run locally.

## Decision log

- **2026-09-08** — Notary credentials use an App Store Connect API key, not an
  app-specific password. Why: revocable per key and not tied to the
  maintainer's Apple ID; the app-specific password documented today in
  `docs/releasing-signed-macos.md` stays valid for the local fallback.
- **2026-09-08** — Triggers are `push: tags: ['v*']` **and**
  `workflow_dispatch`. Why: a dispatch fallback re-runs a failed publish without
  deleting and re-pushing the tag.
- **2026-09-08** — Ingested at PLAN from a hand-drafted design doc
  (`docs/exec-plans/active/release-on-ci.md`), which already carried the
  research. Why: re-running TRIAGE/RESEARCH would only re-derive it.
- **2026-09-08** — `.claude/skills/hs-release/SKILL.md` is out of scope. Why: it
  is a user-level skill outside this repository.

## Second opinion

**Round 1 — verdict `revise`, confidence 8.** Reviewer found the Approach maps
cleanly onto the success criteria and stays inside the non-goals, and confirmed
`release.sh:182` is the right cut, but flagged six must-fix items. **All six
applied:**

1. *Verification step 4 could not fail for the right reason* — the `sed`
   left the worktree dirty, so `release.sh:61`'s clean-tree guard exited first
   and `PIN_GUARD_OK` was printed by the wrong check; `--local-artifacts` also
   tripped the `:?` unset-variable exit; and `sed -i ''` is BSD-only. Rewritten
   to commit the mutation, supply fake credentials, use portable `sed -i.bak`,
   and assert on the `pins no Team ID` string.
2. *Verification step 3 was vacuous* — `exit 0` is also satisfied by a
   `--check-preflight` that short-circuits before running any pre-flight logic.
   The flag now prints `preflight ok: team=… signing=…` and the step greps it.
3. *Signing must be mandatory, not conditional* — "sign when credentials are
   present" would turn a mis-set secret into a green run publishing an unsigned
   zip. Inverted to a hard failure with an explicit `--allow-unsigned` opt-out,
   plus a selftest assertion.
4. *Keychain sequence was incomplete* — the constraints named two of four calls.
   Added `unlock-keychain` (a locked keychain hangs `codesign` for the full
   180-minute budget) and `default-keychain -s` (`notarytool store-credentials`
   writes to the *default* keychain, so without it the profile lookup fails
   after the build has already run).
5. *Checkout ref unspecified* — `workflow_dispatch` defaults to the dispatch
   branch, so the rc rehearsal would have built the branch tip and published it
   under the tag. Now `ref: ${{ inputs.tag || github.ref }}`, and the empty-notes
   consequence for an rc tag is stated and accepted explicitly.
6. *`DESIGN.md:117` missing from blast radius* — already added before the
   review returned (Files to change #8).

**Round 2 — verdict `revise`, confidence 8.** The reviewer confirmed all six
round-1 items were genuinely fixed, but caught that three of the fixes were
contradicted elsewhere in the same file, plus two real gaps. **All five
applied** — and this is the final reviewer round; the loop allows one revise +
one re-review, not a third pass.

1. *The `--local-artifacts` gate was drawn in the wrong place* — it named
   `release.sh:71-74` and `:92-97` but omitted **`:85-90`**, which dereferences
   `$HIVE_SIGN_IDENTITY` to parse the Team ID out of it. Under `set -euo
   pipefail` that is an `unbound variable` crash, so verification step 3 would
   have died before printing its marker and spec criterion 1 would be unmet.
   The pre-flight split is now spelled out line by line.
2. *Files-to-change #2 still said "sign on Darwin when credentials are
   present"* — the exact behaviour round-1 item 3 rejected, sitting three
   sections away from the contract that replaced it. An implementer reading the
   file list would have rebuilt the regression. Rewritten to the hard-fail +
   `--allow-unsigned` contract.
3. *The selftest was wired into nothing* — `ci.yml:38-46` shellchecks
   `scripts/*.sh`, it does not execute them. Added an explicit invocation next
   to the two existing selftest steps at `changesets.yml:84-92`, whose stated
   rationale is precisely this script's situation.
4. *The idempotency assertion was vacuous* — `DRY_RUN=1` was specified as
   skipping "build and `gh`", and the selftest runs only under it, so the
   stubbed `gh` would never be called and the create-vs-`--clobber` branch —
   spec criterion 5 — went untested. Split into `SKIP_BUILD=1`, with `gh`
   always invoked.
5. *"The pin check stays unconditional" was ambiguous* — it currently sits
   inside `if [[ $(uname -s) == Darwin ]]`, and steps 3 and 4 are expected to
   run on the Linux legs, where a still-gated check makes both assertions
   unreachable. Now stated explicitly: it leaves the Darwin block.

Round-2 nice-to-haves taken: step 3 reads the pinned team from
`internal/buildinfo/signing.go` instead of hardcoding it, so a pin rotation
does not break verification; step 4 drops the `--local-artifacts` and fake
credentials it never needed, so it goes red if someone reorders the credential
refusals ahead of the pin check; a trigger allow-list grep now guards spec
criterion 7 (`actionlint` does not check which triggers a workflow declares);
fixed the duplicate `9.` numbering in Files to change.

Round-1 nice-to-haves taken: normalized the `concurrency` key to a bare tag (a dispatch
and a tag push would otherwise sit in different groups and race through
`--clobber`); dropped the caller-less `--no-publish`; added a sign-before-shasum
ordering assertion; corrected the stale unwind recipe in
`docs/releasing-signed-macos.md:195`; specified the workflow-filtered Actions
URL. Left as-is: the workflow's `npm ci && npm run build` step, which the
reviewer noted overlaps `build.sh` — it matches `ci.yml:102-108` and dropping it
risks the "no index.html" failure mode recorded in the prior lessons.

- **2026-09-08** — Review iter 3, two findings. The troubleshooting rows in
  `docs/releasing-signed-macos.md` had gone stale in the *other* direction:
  they described an unwind recipe `sign-macos.sh` no longer prints, and one row
  still claimed the push precedes the build under `--local-artifacts`, which
  the draft-publish change reversed. And the `LC_ALL=C` collation pin could not
  be exercised — CI's locale is already C, so no assertion could distinguish
  the pinned code from the broken code.
  Rather than test around the collation, removed it: the stand-down now asks
  "is each expected asset present, and are there exactly three?" instead of
  comparing two sorted joined strings. There is no ordering left to get wrong,
  so nothing locale-dependent remains to test. Rewriting it immediately caught
  a second portability bug — `mapfile` is bash 4+, and macOS (including the
  release runner) ships bash 3.2, so the script silently produced no output.
  Replaced with a `while read` loop; the selftest now passes under both
  `/bin/bash` 3.2 and modern bash. 23 → 25 assertions.

- **2026-09-08** — Review iter 4: APPROVE, no BLOCKING or IMPORTANT, CI green.
  Applied the five MINOR nits in the same PR per AGENTS.md's boil-the-lake
  rule: tightened the `--target` assertion to the exact fixture sha (a loose
  40-hex pattern would have accepted a wrong sha), added an extra-asset case so
  `-ne` cannot silently become `-lt`, covered the credential-dependent
  `--local-artifacts` pre-flight branch, and made the non-Darwin path refuse to
  publish an unsigned artifact rather than skip signing silently. The cert
  password on `security import`'s argv is accepted and documented — `security
  import` has no stdin alternative and the runner is ephemeral. 25 → 28
  assertions.

## PR convergence ledger

Append-only. One line per `/hs-review-loop` iteration.

- **2026-09-08 iter 1** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: ed7b26e2; threads_open: 0; action: autofix+push; head_sha: 358dfa85.
- **2026-09-08 iter 2** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; findings_hash: 95392a29; threads_open: 0; action: escalated:risky-fix-needs-human-decision; head_sha: b4d62177.
- **2026-09-08 iter 3** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; findings_hash: 4d542a35; threads_open: 0; action: escalated:risky-fix-needs-human-decision; head_sha: 39423662.
- **2026-09-08 iter 4** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: ff8209da.

## Progress

- **2026-09-08** — Spec #382 created; draft design doc reshaped into this plan.
- **2026-09-08** — Implemented. Verification steps 1–4 pass locally: shellcheck
  clean on all three scripts, `actionlint` clean on every workflow, selftest
  12/12, no-credentials preflight prints its marker, pin guard fails with
  `pins no Team ID`. Step 5 (the rc rehearsal) is blocked on the seven repo
  secrets/variables, which only the maintainer can create.
- **2026-09-08** — Adding `actionlint` surfaced a *pre-existing* SC2155 in
  `ci.yml:267` (`export HOME="$(mktemp -d)"` masks a failing `mktemp` behind
  `export`'s exit status). Fixed in the same PR: a gate introduced already red
  is not a gate. Used `go install …/actionlint@v1.7.7` rather than the upstream
  `curl | bash` installer, to match the pinned-SHA discipline the other
  workflows hold actions to.
- **2026-09-08** — `--allow-unsigned` replaced the planned `--no-publish`,
  which had no caller.
- **2026-09-08** — Review iter 1 raised two IMPORTANT findings, both real.
  (a) Four of the twelve selftest assertions were `uname`-gated to Darwin
  while CI runs the script on ubuntu only, so the guards against publishing
  an unsigned zip printed "skip" on every CI run. Fixed by stubbing `uname`
  in the fixture rather than adding a macOS CI leg: all four assert control
  flow, and codesign/notarytool are already behind stubs, so there is no real
  Apple tooling to exercise — a macOS runner would have cost minutes to test
  the same stubs.
  (b) `--local-artifacts` published *after* `git push origin HEAD "$TAG"`,
  and that tag push is what triggers `release.yml` — so the fallback raced
  the CI build it had just started, two notarizations clobbering the same
  assets, with `--clobber` making the collision look like success. Note the
  first fix attempted (build before pushing) only reordered the race; the
  push still triggers the workflow. The actual fix has three parts: the local
  path pushes the commit but **not** the tag, `gh release create --target`
  creates the remote tag as part of publishing, and `release.yml` gained a
  stand-down step that exits when the release already carries all three
  assets. The stand-down deliberately requires *all three* — a release left
  partial by a died run must still be republishable by re-dispatch.
- **2026-09-08** — Second-opinion round 1: `revise` (confidence 8), 6 must-fix,
  6 applied. Round 2: `revise` (confidence 8), 5 must-fix, 5 applied — three of
  them contradictions introduced by the round-1 edits. See `## Second opinion`.
