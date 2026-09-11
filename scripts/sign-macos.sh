#!/usr/bin/env bash
#
# Sign a macOS build.
#
# Two modes:
#
#   sign-macos.sh <release-zip>
#       Release path. Signs with the Developer ID identity, notarizes,
#       staples the ticket, and rewrites the zip in place. Requires
#       credentials. Called by scripts/release-artifacts.sh, which runs
#       either on the CI macOS runner or locally via
#       `scripts/release.sh --local-artifacts`.
#
#   sign-macos.sh --adhoc <app-dir>
#       Local path. Signs with an ad-hoc identity and the hardened
#       runtime, then stops: no credentials, no network, no
#       notarization. An ad-hoc signature carries no Team ID, so a
#       bundle signed this way can never satisfy the updater's pin —
#       it cannot be mistaken for a release.
#
#       Its purpose is that `codesign -s - -o runtime` still sets the
#       hardened-runtime flag, so the runtime restrictions a notarized
#       build will face can be reproduced — and a PTY host or login
#       item broken by them found — without a $99 certificate.
#
# Environment (release mode only):
#   HIVE_SIGN_IDENTITY   "Developer ID Application: Name (TEAMID)"
#   HIVE_NOTARY_PROFILE  notarytool keychain profile name
#
# See docs/releasing-signed-macos.md for how to obtain both.
set -euo pipefail

BUNDLE_NAME="hivegui.app"

# The pinned Team ID is read from a repo file, but $ZIP and the --adhoc
# app path belong to the caller and may be relative to wherever they
# ran this from. So the repo file is resolved from this script's own
# location rather than by cd'ing to the repo root like sibling scripts
# do: a cd would silently break every relative path argument, and
# resolving against $PWD would break when run from outside the repo.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SIGNING_GO="$SCRIPT_DIR/../internal/buildinfo/signing.go"

die() { echo "error: $*" >&2; exit 1; }

# sign_inside_out signs every Mach-O in the bundle before the bundle
# itself. Order is not optional: signing the outer .app seals a hash of
# its contents, so a nested binary signed afterwards invalidates the
# seal.
sign_inside_out() {
    local app="$1" identity="$2"
    local bar="$app/Contents/Library/LoginItems/hivebar.app"

    # --force because build.sh's `lipo -create` output, and Go's own
    # linker-signed binaries, may already carry a signature we are
    # replacing. --timestamp is required for notarization; it is a
    # network call, and the one in ad-hoc mode is harmless because
    # codesign skips it for the ad-hoc identity.
    local opts=(--force --options runtime --timestamp)
    [[ "$identity" == "-" ]] && opts=(--force --options runtime)

    if [[ -d "$bar" ]]; then
        codesign "${opts[@]}" --sign "$identity" "$bar/Contents/MacOS/hivebar"
        codesign "${opts[@]}" --sign "$identity" "$bar"
    fi
    codesign "${opts[@]}" --sign "$identity" "$app/Contents/MacOS/hived"
    codesign "${opts[@]}" --sign "$identity" "$app/Contents/MacOS/hivegui"
    codesign "${opts[@]}" --sign "$identity" "$app"
}

# ---- adhoc mode ----------------------------------------------------------

if [[ "${1:-}" == "--adhoc" ]]; then
    APP="${2:-}"
    [[ -n "$APP" ]] || die "usage: $0 --adhoc <app-dir>"
    [[ -d "$APP" ]] || die "not a directory: $APP"

    echo "==> [adhoc] Signing $APP with the hardened runtime"
    sign_inside_out "$APP" "-"
    codesign --verify --deep --strict "$APP" \
        || die "ad-hoc verification failed for $APP"

    echo "==> [adhoc] Done. Flags below should include 'runtime':"
    codesign --display --verbose "$APP" 2>&1 | grep -i 'flags' || true
    echo
    echo "This build is NOT distributable — an ad-hoc signature has no Team ID"
    echo "and the updater's pin will reject it. It is for testing hardened-runtime"
    echo "behavior (PTY sessions, login item) without a certificate."
    exit 0
fi

# ---- release mode --------------------------------------------------------

ZIP="${1:-}"
[[ -n "$ZIP" ]] || die "usage: $0 <release-zip> | --adhoc <app-dir>"
[[ -f "$ZIP" ]] || die "no such zip: $ZIP"

: "${HIVE_SIGN_IDENTITY:?set HIVE_SIGN_IDENTITY (see docs/releasing-signed-macos.md)}"
: "${HIVE_NOTARY_PROFILE:?set HIVE_NOTARY_PROFILE (see docs/releasing-signed-macos.md)}"

command -v xcrun >/dev/null 2>&1 \
    || die "xcrun not found — notarization needs the Xcode command-line tools"

# The Team ID is the parenthesised suffix of the identity string.
IDENTITY_TEAM_ID="$(sed -n 's/.*(\([A-Z0-9]\{10\}\))$/\1/p' <<<"$HIVE_SIGN_IDENTITY")"
[[ -n "$IDENTITY_TEAM_ID" ]] \
    || die "could not parse a 10-character Team ID from HIVE_SIGN_IDENTITY: $HIVE_SIGN_IDENTITY"

# The pin the shipped binary will enforce, read from the single
# declaration in internal/buildinfo/signing.go. If these two disagree
# the release is unusable: every client would reject an update signed
# by a team its binary does not trust. TestSigningTeamIDMatchesSource
# asserts this extraction agrees with what the Go code sees.
[[ -f "$SIGNING_GO" ]] || die "cannot find $SIGNING_GO — run this from inside the hive repo"
PINNED_TEAM_ID="$(sed -n 's/^var signingTeamID = "\(.*\)"$/\1/p' "$SIGNING_GO")"
[[ -n "$PINNED_TEAM_ID" ]] \
    || die "$SIGNING_GO pins no Team ID — a release built from it would skip signature verification for every user. Set signingTeamID first."
[[ "$PINNED_TEAM_ID" == "$IDENTITY_TEAM_ID" ]] \
    || die "Team ID mismatch: signing with $IDENTITY_TEAM_ID but the binary pins $PINNED_TEAM_ID. No client could install this release."

security find-identity -v -p codesigning | grep -qF "$HIVE_SIGN_IDENTITY" \
    || die "identity not found in keychain: $HIVE_SIGN_IDENTITY"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

echo "==> [sign] Unpacking $ZIP"
ditto -x -k "$ZIP" "$WORK/app"
APP="$WORK/app/$BUNDLE_NAME"
[[ -d "$APP" ]] || die "zip did not contain $BUNDLE_NAME"

echo "==> [sign] Signing as $HIVE_SIGN_IDENTITY"
sign_inside_out "$APP" "$HIVE_SIGN_IDENTITY"

echo "==> [sign] Submitting for notarization (typically 2-15 minutes)"
ditto -c -k --keepParent "$APP" "$WORK/notarize.zip"
if ! xcrun notarytool submit "$WORK/notarize.zip" \
        --keychain-profile "$HIVE_NOTARY_PROFILE" --wait; then
    cat >&2 <<UNWIND

Notarization failed. Do NOT reset or delete the tag: the release commit is
already on origin by the time this runs, so a reset would be undone by the
next pull, and re-running scripts/release.sh would re-bump the version and
re-stamp the changelog on top of a commit that is already published.

Fix the cause, then re-publish the artifacts for the existing tag:

  scripts/release-artifacts.sh <version>          # local
  gh workflow run release.yml -f tag=<tag>        # CI

Both are safe to repeat — publishing is idempotent (it updates an existing
release's assets rather than failing on one).

If notarytool reported "Invalid", fetch the detail with:

  xcrun notarytool log <submission-id> --keychain-profile $HIVE_NOTARY_PROFILE
UNWIND
    exit 1
fi

echo "==> [sign] Stapling the ticket"
xcrun stapler staple "$APP"
xcrun stapler validate "$APP"

# Verify what we are about to ship the same way every client will. This
# is the check that would catch signing with the wrong identity.
echo "==> [sign] Verifying against the shipped pin"
codesign --verify --deep --strict \
    -R "=anchor apple generic and certificate leaf[subject.OU] = \"$PINNED_TEAM_ID\"" \
    "$APP" || die "signed bundle does not satisfy the pin clients will enforce"

# Repackage to a temp path and move into place, so an abort never
# leaves an unsigned artifact sitting at the publish path.
echo "==> [sign] Repackaging $ZIP"
ditto -c -k --keepParent "$APP" "$WORK/signed.zip"
mv "$WORK/signed.zip" "$ZIP"

echo "==> [sign] $ZIP is signed, notarized and stapled"
