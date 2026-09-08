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
echo "gh $*" >> "$GH_LOG"
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

    chmod +x "$WORK/bin/gh" "$WORK/bin/shasum" scripts/sign-macos.sh scripts/release-artifacts.sh
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

# 5. Missing credentials on Darwin are fatal, not a silent skip. "Sign if the
#    credentials happen to be present" would turn a mis-set repo secret into a
#    green run that publishes an unsigned, un-notarized zip.
if [[ "$(uname -s)" == "Darwin" ]]; then
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
else
    echo "  skip signing assertions (not macOS)"
fi

# 7. The release workflow must never gain a pull_request trigger: it can read
#    the signing certificate and the notary key. actionlint validates syntax
#    but not which triggers a workflow declares, so nothing else guards this.
cd "$REPO_ROOT"
if grep -Eq '^\s*pull_request(_target)?\s*:' .github/workflows/release.yml; then
    bad "release.yml must not have a pull_request trigger"
else
    ok "release.yml has no pull_request trigger"
fi

printf '\n%d passed, %d failed\n' "$pass" "$fail"
[[ "$fail" -eq 0 ]]
