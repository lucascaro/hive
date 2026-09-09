#!/usr/bin/env bash
set -euo pipefail

# release-artifacts-selftest.sh — prove scripts/release-artifacts.sh still
# fires.
#
# Same reasoning as check-daemon-contract-selftest.sh: this code only runs on
# the release path, where a mistake reaches users instead of CI. Each
# assertion below is named for the specific regression it catches.
#
# The script under test is driven with SKIP_BUILD=1 (no Wails, no Xcode) and
# a stubbed `gh` / `sign-macos.sh` / `shasum` on PATH. `gh` is deliberately
# NOT stubbed out of existence: the create-vs-clobber branch is the whole
# point of the idempotency assertion.

cd "$(git rev-parse --show-toplevel)"
REPO_ROOT="$PWD"

pass=0
fail=0
ok()   { printf '  ok   %s\n' "$1"; pass=$((pass + 1)); }
bad()  { printf '  FAIL %s\n' "$1"; fail=$((fail + 1)); }
check() { if [[ "$2" == "$3" ]]; then ok "$1"; else bad "$1"; printf '       expected: %s\n       actual:   %s\n' "$3" "$2"; fi; }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# ---- fixture -------------------------------------------------------------
#
# A throwaway git repo, so `git rev-parse --show-toplevel` inside the script
# under test lands here and not in the real checkout.

setup() {
    rm -rf "${WORK:?}/repo" "${WORK:?}/bin"
    mkdir -p "$WORK/repo/release" "$WORK/repo/scripts" "$WORK/repo/internal/buildinfo" "$WORK/bin"
    cd "$WORK/repo"
    git init -q . && git config user.email t@t && git config user.name t

    cp "$REPO_ROOT/scripts/release-artifacts.sh" scripts/
    cp "$REPO_ROOT/scripts/release-standdown.sh" scripts/
    cp "$REPO_ROOT/scripts/release.sh" scripts/
    mkdir -p internal/buildinfo
    printf 'var signingTeamID = "AAAAAAAAAA"\n' > internal/buildinfo/signing.go
    printf 'zip-macos\n'   > release/Hive-9.9.9-macos-universal.zip
    printf 'zip-windows\n' > release/Hive-9.9.9-windows-amd64.zip

    cat > CHANGELOG.md <<'CL'
# Changelog

## [Unreleased]

## [9.9.9] — 2026-01-01

- the version under test

## [9.9.8] — 2025-12-01

- an older release that must NOT bleed into the notes
CL

    # Stubs. Each records what it was asked to do; sign and shasum append to
    # a shared order log so their relative order is observable.
    cat > "$WORK/bin/gh" <<'GH'
#!/usr/bin/env bash
# Squash newlines: --notes carries a multi-line changelog body, and a raw
# `echo "gh $*"` would spread one invocation across several log lines, so a
# per-line grep would never see the tag and a flag together.
{ printf 'gh %s' "$*" | tr '\n' ' '; printf '\n'; } >> "$GH_LOG"
if [[ "$1" == "release" && "$2" == "view" ]]; then
    [[ "${GH_RELEASE_EXISTS:-0}" == "1" ]] && exit 0 || exit 1
fi
exit 0
GH

    cat > "$WORK/bin/shasum" <<'SH'
#!/usr/bin/env bash
echo "shasum" >> "$ORDER_LOG"
# Consume `-a 256` (flag AND its value) so only the filenames remain.
while [[ "${1:-}" == -* ]]; do
    case "$1" in -a|--algorithm) shift 2 ;; *) shift ;; esac
done
for f in "$@"; do echo "deadbeef  $f"; done
SH

    mkdir -p scripts
    cat > scripts/sign-macos.sh <<'SIGN'
#!/usr/bin/env bash
echo "sign" >> "$ORDER_LOG"
SIGN

    # The signing assertions below are the reason this script exists, and
    # release-artifacts.sh reaches them only under `uname -s` == Darwin. CI
    # runs this on ubuntu (changesets.yml, and there is no macOS leg), so
    # gating them on the real platform meant the four checks that guard
    # against publishing an unsigned zip printed "skip" on every CI run —
    # the exact regressions the script was written to catch went unguarded.
    #
    # Stub `uname` instead. Nothing here needs real Apple tooling: codesign
    # and notarytool are already behind the sign-macos.sh stub above, so
    # these assert control flow (refuse when credentials are missing, never
    # publish unsigned, sign before shasum) which is platform-independent.
    cat > "$WORK/bin/uname" <<'UN'
#!/usr/bin/env bash
[[ "${1:-}" == "-s" ]] && { echo Darwin; exit 0; }
exec /usr/bin/uname "$@"
UN
    chmod +x "$WORK/bin/uname"

    chmod +x "$WORK/bin/gh" "$WORK/bin/shasum" scripts/sign-macos.sh \
        scripts/release-artifacts.sh scripts/release-standdown.sh scripts/release.sh

    # A real commit, so `git rev-parse HEAD` resolves. Without one it exits
    # 128 and prints the literal string "HEAD" — which release-artifacts.sh
    # then passed to `--target`, and the assertion below still matched on a
    # substring. The bug was invisible precisely because it was untested.
    git add -A >/dev/null && git commit -qm init
    FIXTURE_SHA="$(git rev-parse HEAD)"
    export FIXTURE_SHA
    export PATH="$WORK/bin:$PATH"
    export GH_LOG="$WORK/gh.log" ORDER_LOG="$WORK/order.log"
    : > "$GH_LOG"; : > "$ORDER_LOG"
}

run() {
    SKIP_BUILD=1 REPO=owner/repo \
        HIVE_SIGN_IDENTITY='Developer ID Application: T (AAAAAAAAAA)' \
        HIVE_NOTARY_PROFILE=profile \
        bash scripts/release-artifacts.sh "$@" 2>&1
}

echo "release-artifacts.sh selftest"

# 1. The checksum manifest must be basenamed. Dropping the `cd release`
#    subshell would write "release/Hive-...zip" paths, which no longer match
#    the asset names GitHub serves — silently breaking the in-app updater's
#    manifest lookup for every user.
setup
run 9.9.9 >/dev/null
check "checksum manifest is basenamed" \
    "$(cut -c11- release/checksums.txt | tr '\n' ' ' | sed 's/ $//')" \
    "Hive-9.9.9-macos-universal.zip Hive-9.9.9-windows-amd64.zip"

# 2. Notes extraction must stop at the next section. An awk that loses its
#    /^## \[/{exit} guard would paste every older release into the notes.
setup
run 9.9.9 >/dev/null
notes="$(grep -c 'the version under test' "$GH_LOG" || true)"
check "notes extraction includes the target section" "$notes" "1"
check "notes extraction stops at the next section" \
    "$(grep -c 'an older release' "$GH_LOG" || true)" "0"

# 3. A missing artifact is fatal. Losing the [[ -f ]] assertions in the move
#    would publish a release with an asset silently absent.
setup
rm release/Hive-9.9.9-windows-amd64.zip
if run 9.9.9 >/dev/null 2>&1; then bad "missing artifact is fatal"; else ok "missing artifact is fatal"; fi

# 4. Publishing is idempotent. This is what makes a re-dispatch able to
#    repair a run that died after the tag was already pushed; a straight
#    `gh release create` would fail on "release already exists" forever.
setup
GH_RELEASE_EXISTS=0 run 9.9.9 >/dev/null
if grep -q 'gh release create v9.9.9' "$GH_LOG"; then ok "no existing release -> create"; else bad "no existing release -> create"; fi
setup
GH_RELEASE_EXISTS=1 run 9.9.9 >/dev/null
if grep -q 'gh release upload v9.9.9 .*--clobber' "$GH_LOG"; then ok "existing release -> upload --clobber"; else bad "existing release -> upload --clobber"; fi
if grep -q 'gh release create' "$GH_LOG"; then bad "must not also create when one exists"; else ok "must not also create when one exists"; fi

# 4b. A new release must be created as a DRAFT, populated, and only then
#     published. A draft creates no git tag, so the tag springs into being
#     with the release already complete — which is the whole reason the
#     local path cannot race the CI build the tag push triggers. Creating it
#     public-first would reopen that race silently.
setup
GH_RELEASE_EXISTS=0 run 9.9.9 >/dev/null
if grep 'gh release create v9.9.9' "$GH_LOG" | grep -q -- '--draft'; then ok "new release is created as a draft"; else bad "new release is created as a draft"; fi
if grep -q 'gh release edit v9.9.9 .*--draft=false' "$GH_LOG"; then ok "draft is published after upload"; else bad "draft is published after upload"; fi
check "create -> upload -> publish, in that order" \
    "$(grep -o 'release \(create\|upload\|edit\)' "$GH_LOG" | tr '\n' ' ' | sed 's/ $//')" \
    "release create release upload release edit"
# Exact sha, not a loose 40-hex pattern: a wrong-but-hex value would satisfy
# the pattern and the assertion would prove nothing.
if grep -q -- "--target ${FIXTURE_SHA}" "$GH_LOG"; then
    ok "--target carries the fixture's real commit sha"
else
    bad "--target carries the fixture's real commit sha"
fi
if grep -q -- '--target HEAD' "$GH_LOG"; then bad "--target must not be the literal string HEAD"; else ok "--target must not be the literal string HEAD"; fi

# 4c. A pre-release version must be published as a GitHub pre-release. The
#     in-app updater polls /releases/latest (cmd/hivegui/update.go:26), and
#     GitHub serves the newest NON-prerelease there — so publishing an rc
#     without the flag offers a release candidate to every user as their next
#     update. This happened once, during the rc rehearsal for this feature.
setup
# The fixture seeds 9.9.9-named zips; this case releases 9.9.9-rc1, so give it
# artifacts under that name or it aborts on "expected artifact missing"
# before it ever reaches `gh release create`.
cp release/Hive-9.9.9-macos-universal.zip release/Hive-9.9.9-rc1-macos-universal.zip
cp release/Hive-9.9.9-windows-amd64.zip   release/Hive-9.9.9-rc1-windows-amd64.zip
GH_RELEASE_EXISTS=0 run 9.9.9-rc1 >/dev/null
if grep 'gh release create v9.9.9-rc1' "$GH_LOG" | grep -q -- '--prerelease'; then
    ok "a -rc version is published as a prerelease"
else
    bad "a -rc version is published as a prerelease"
fi
setup
GH_RELEASE_EXISTS=0 run 9.9.9 >/dev/null
if grep 'gh release create v9.9.9' "$GH_LOG" | grep -q -- '--prerelease'; then
    bad "a plain version must NOT be a prerelease"
else
    ok "a plain version must NOT be a prerelease"
fi

# 5. Missing credentials on Darwin are fatal, not a silent skip. "Sign if the
#    credentials happen to be present" would turn a mis-set repo secret into a
#    green run that publishes an unsigned, un-notarized zip.
#
#    Runs everywhere, via the stubbed `uname` in setup() — see the note there.
    setup
    out="$(SKIP_BUILD=1 REPO=owner/repo env -u HIVE_SIGN_IDENTITY -u HIVE_NOTARY_PROFILE \
        bash scripts/release-artifacts.sh 9.9.9 2>&1 || true)"
    if grep -q 'refusing to publish unsigned' <<<"$out"; then ok "missing credentials are fatal"; else bad "missing credentials are fatal"; fi
    if grep -q 'gh release' "$GH_LOG"; then bad "must not publish when unsigned"; else ok "must not publish when unsigned"; fi

    setup
    SKIP_BUILD=1 REPO=owner/repo env -u HIVE_SIGN_IDENTITY -u HIVE_NOTARY_PROFILE \
        bash scripts/release-artifacts.sh 9.9.9 --allow-unsigned >/dev/null 2>&1
    if grep -q 'gh release' "$GH_LOG"; then ok "--allow-unsigned publishes anyway"; else bad "--allow-unsigned publishes anyway"; fi

    # 6. Signing must run BEFORE the checksum manifest. A reordering would
    #    publish a checksums.txt describing the PRE-signature zip, breaking
    #    the updater's integrity check for every user.
    setup
    run 9.9.9 >/dev/null
    check "sign runs before shasum" "$(tr '\n' ' ' < "$ORDER_LOG" | sed 's/ $//')" "sign shasum"

# 6b. The stand-down decision. A sort or whitespace bug that wrongly answered
#     "skip" would turn a repair dispatch into a silent no-op, and a draft or
#     partial release must never stand down — finishing exactly those is why
#     workflow_dispatch exists.
standdown() {
    cat > "$WORK/bin/gh" <<GHS
#!/usr/bin/env bash
if [[ "\$3" == "--repo" ]]; then :; fi
case "\$*" in
  *"--json isDraft"*) echo '$2'; exit 0 ;;
  *"--json assets"*)  [[ -n '$1' ]] || exit 1; printf '%s\n' '$1' | tr ' ' '\n'; exit 0 ;;
esac
exit 0
GHS
    chmod +x "$WORK/bin/gh"
    REPO=o/r bash "$REPO_ROOT/scripts/release-standdown.sh" v9.9.9
}
setup
check "complete release -> skip" \
    "$(standdown 'Hive-9.9.9-macos-universal.zip Hive-9.9.9-windows-amd64.zip checksums.txt' false)" "skip"
check "missing checksums -> run" \
    "$(standdown 'Hive-9.9.9-macos-universal.zip Hive-9.9.9-windows-amd64.zip' false)" "run"
check "complete but draft -> run" \
    "$(standdown 'Hive-9.9.9-macos-universal.zip Hive-9.9.9-windows-amd64.zip checksums.txt' true)" "run"
check "no release at all -> run" "$(standdown '' false)" "run"
# Order must not matter. The previous revision compared sorted joined strings
# and could never match, because jq sorts bytewise and shell `sort` collates by
# locale — so the stand-down silently never fired and every local release got
# rebuilt by CI. Feeding the names in a different order proves the comparison
# is by membership now, with no collation left to get wrong.
check "asset order does not matter -> skip" \
    "$(standdown 'checksums.txt Hive-9.9.9-windows-amd64.zip Hive-9.9.9-macos-universal.zip' false)" "skip"
check "a wrong-but-count-3 asset set -> run" \
    "$(standdown 'checksums.txt Hive-9.9.9-macos-universal.zip Hive-9.9.8-windows-amd64.zip' false)" "run"
# The upper bound matters too: with `-lt` instead of `-ne` a release carrying
# an unexpected extra asset would stand down, and the suite would not notice.
check "an extra asset -> run" \
    "$(standdown 'checksums.txt Hive-9.9.9-macos-universal.zip Hive-9.9.9-windows-amd64.zip stray.txt' false)" "run"

# 6c. The relocated Team-ID pin guard. Moving it out of the `uname` Darwin
#     block was one of this change's riskier edits: it is what stops a build
#     that pins no team — and would therefore skip update signature
#     verification for every user — from ever being released. --check-preflight
#     exists so this can be asserted without cutting a real release.
cd "$WORK/repo"
out="$(env -u HIVE_SIGN_IDENTITY -u HIVE_NOTARY_PROFILE bash scripts/release.sh --check-preflight 9.9.9 2>&1 || true)"
if grep -q 'preflight ok: team=AAAAAAAAAA signing=off' <<<"$out"; then
    ok "preflight runs without any signing credentials"
else
    bad "preflight runs without any signing credentials"; printf '       %s\n' "$out"
fi

# The credential-dependent branch: --local-artifacts + a mismatched identity
# must be refused. `uname` is stubbed to Darwin, so this reaches the block
# that plain --check-preflight deliberately skips.
out="$(HIVE_SIGN_IDENTITY='Developer ID Application: T (BBBBBBBBBB)' HIVE_NOTARY_PROFILE=p \
    bash scripts/release.sh --local-artifacts --check-preflight 9.9.9 2>&1 || true)"
if grep -q 'does not match the pinned team' <<<"$out"; then
    ok "mismatched signing identity is refused"
else
    bad "mismatched signing identity is refused"; printf '       %s\n' "$out"
fi

out="$(env -u HIVE_SIGN_IDENTITY -u HIVE_NOTARY_PROFILE \
    bash scripts/release.sh --local-artifacts --check-preflight 9.9.9 2>&1 || true)"
if grep -q 'HIVE_SIGN_IDENTITY' <<<"$out"; then
    ok "--local-artifacts demands credentials"
else
    bad "--local-artifacts demands credentials"; printf '       %s\n' "$out"
fi

printf 'var signingTeamID = ""\n' > internal/buildinfo/signing.go
out="$(env -u HIVE_SIGN_IDENTITY -u HIVE_NOTARY_PROFILE bash scripts/release.sh --check-preflight 9.9.9 2>&1 || true)"
if grep -q 'pins no Team ID' <<<"$out"; then
    ok "empty Team ID pin is refused (and not gated on macOS)"
else
    bad "empty Team ID pin is refused (and not gated on macOS)"; printf '       %s\n' "$out"
fi
cd "$REPO_ROOT"

# 7. The release workflow must never gain a pull_request trigger: it can read
#    the signing certificate and the notary key. actionlint validates syntax
#    but not which triggers a workflow declares, so nothing else guards this.
cd "$REPO_ROOT"
if grep -Eq '^\s*pull_request(_target)?\s*:' .github/workflows/release.yml; then
    bad "release.yml must not have a pull_request trigger"
else
    ok "release.yml has no pull_request trigger"
fi

# 8. The signing credentials are gated behind a protected environment. The
#    job-level `environment:` key is what makes the required reviewer and the
#    tag-only deployment policy apply; delete the line and the secrets become
#    available to any run again, silently and with CI still green. This
#    assertion is the only thing standing between that edit and a release.
if grep -Eq '^\s{4}environment:\s*release\s*$' .github/workflows/release.yml; then
    ok "release job is gated behind the 'release' environment"
else
    bad "release job is gated behind the 'release' environment"
fi

# 9. Credential-blast-radius invariants in release.yml. Each of these was a
#    real finding from the pre-merge security audit; each would re-open
#    silently, with CI green, if the line were edited away.
wf=.github/workflows/release.yml

# 9a. The dispatch input selects what code runs with the signing key, so it
#     must resolve to a TAG, never a branch or a bare SHA. Without the
#     refs/tags/ prefix an actor could dispatch against an existing v* tag
#     (satisfying the environment's deployment policy, and needing no admin
#     action since reusing a tag is not tag creation) while pointing `tag:`
#     at a branch they pushed.
if grep -q "format('refs/tags/{0}', inputs.tag)" "$wf"; then
    ok "checkout ref is constrained to refs/tags/"
else
    bad "checkout ref is constrained to refs/tags/"
fi

# 9b. ...and the input is validated, so `refs/tags/<anything>` cannot select
#     a non-release tag that any writer is free to create.
if grep -q 'refusing to release from' "$wf"; then
    ok "tag input is validated against a semver pattern"
else
    bad "tag input is validated against a semver pattern"
fi

# 9c. The keychain password must not reach $GITHUB_ENV: nothing after the
#     import step needs it, and exporting it hands it to every later step.
if grep -q 'KEYCHAIN_PASSWORD=.*GITHUB_ENV' "$wf"; then
    bad "keychain password must not be exported to \$GITHUB_ENV"
else
    ok "keychain password is not exported to \$GITHUB_ENV"
fi

# 9d. The build must finish BEFORE the certificate is imported. build.sh runs
#     its own `npm ci`, so building afterwards would execute every
#     third-party lifecycle script in the frontend dependency tree while the
#     Developer ID key sits in an unlocked, codesign-partitioned keychain.
build_line=$(grep -n 'name: Build release artifacts' "$wf" | cut -d: -f1)
cert_line=$(grep -n 'name: Import the Developer ID certificate' "$wf" | cut -d: -f1)
if [[ -n "$build_line" && -n "$cert_line" && "$build_line" -lt "$cert_line" ]]; then
    ok "build completes before the certificate is imported"
else
    bad "build completes before the certificate is imported"
fi

printf '\n%d passed, %d failed\n' "$pass" "$fail"
[[ "$fail" -eq 0 ]]
