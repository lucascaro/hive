#!/usr/bin/env bash
set -euo pipefail

# release-artifacts.sh — build, sign, checksum and publish the release
# artifacts for an already-created tag.
#
# Usage: scripts/release-artifacts.sh <version> [--allow-unsigned]
#
# This is the half of the release that used to live below the
# "---- BUILD ARTIFACTS ----" line in scripts/release.sh. It moved out so
# .github/workflows/release.yml and the local `release.sh --local-artifacts`
# fallback can share one copy instead of two that drift.
#
# The caller is responsible for the tag existing. This script never commits,
# never tags and never pushes.
#
# Flags:
#   --allow-unsigned   Skip macOS signing instead of failing when the
#                      credentials are absent. Exists for the selftest and
#                      for a deliberate unsigned local build; a release must
#                      NEVER use it (see the refusal below).
#
# Environment:
#   HIVE_SIGN_IDENTITY   "Developer ID Application: Name (TEAMID)"
#   HIVE_NOTARY_PROFILE  notarytool keychain profile name
#   SKIP_BUILD=1         Skip ./build.sh and use the zips already in release/.
#                        Test hook. `gh` is still invoked, so the selftest can
#                        stub it and observe the create-vs-clobber branch.
#   REPO                 owner/repo (default: lucascaro/hive)

REPO="${REPO:-lucascaro/hive}"

cd "$(git rev-parse --show-toplevel)"

VERSION=""
allow_unsigned=0
while [[ $# -gt 0 ]]; do
    case "$1" in
        --allow-unsigned) allow_unsigned=1; shift ;;
        -h|--help) sed -n '4,30p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
        -*) echo "unknown flag: $1" >&2; exit 2 ;;
        *)  [[ -z "$VERSION" ]] || { echo "unexpected argument: $1" >&2; exit 2; }
            VERSION="$1"; shift ;;
    esac
done

[[ -n "$VERSION" ]] || { echo "Usage: $0 <version> [--allow-unsigned]" >&2; exit 2; }

TAG="v${VERSION}"
MACOS_ZIP="release/Hive-${VERSION}-macos-universal.zip"
WINDOWS_ZIP="release/Hive-${VERSION}-windows-amd64.zip"

# ---- BUILD ---------------------------------------------------------------
#
# Hive uses Wails for the GUI and a separate `hived` daemon binary, so a
# generic GOOS/GOARCH loop can't produce the right .app/.exe bundles.
# build.sh knows how to assemble the macOS universal .app and cross-compile
# the Windows amd64 zip.

if [[ "${SKIP_BUILD:-0}" == "1" ]]; then
    echo "SKIP_BUILD=1 — using the artifacts already in release/"
else
    echo "Building release artifacts via build.sh..."
    ./build.sh --zip --version "$VERSION" --platform all
fi

ARTIFACTS=()
for f in "$MACOS_ZIP" "$WINDOWS_ZIP"; do
    [[ -f "$f" ]] || { echo "Error: expected artifact missing: $f" >&2; exit 1; }
    ARTIFACTS+=("$f")
done

# ---- SIGN ----------------------------------------------------------------
#
# Signing is unconditional on macOS, and its absence is fatal rather than
# skipped. "Sign if the credentials happen to be present" is the dangerous
# shape: a mis-set repo secret would then produce a green CI run that
# publishes an unsigned, un-notarized zip — which every client's updater
# pin rejects, and nothing anywhere would have failed.
#
# This must run BEFORE the checksum manifest below. The manifest has to
# describe the artifact that actually ships, and signing rewrites the zip
# in place.

if [[ "$(uname -s)" == "Darwin" ]]; then
    if [[ -n "${HIVE_SIGN_IDENTITY:-}" && -n "${HIVE_NOTARY_PROFILE:-}" ]]; then
        echo "Signing and notarizing the macOS artifact..."
        ./scripts/sign-macos.sh "$MACOS_ZIP"
    elif [[ "$allow_unsigned" == "1" ]]; then
        echo "warning: --allow-unsigned — skipping signing. NOT a releasable artifact." >&2
    else
        echo "Error: refusing to publish unsigned macOS artifacts." >&2
        echo "  HIVE_SIGN_IDENTITY and HIVE_NOTARY_PROFILE must both be set." >&2
        echo "  In CI these come from repo variables; see docs/releasing-signed-macos.md." >&2
        echo "  Pass --allow-unsigned only for a deliberate throwaway build." >&2
        exit 1
    fi
else
    echo "Not macOS — skipping signing (the macOS artifact must be built on macOS)."
fi

# ---- CHECKSUMS -----------------------------------------------------------
#
# The GUI's in-app updater downloads this alongside the macOS zip and
# refuses to install on a mismatch — without it, a truncated download
# becomes a broken Hive.app the user can only fix by reinstalling by hand.
# Names are basenamed so the manifest matches the asset names GitHub serves.

echo "Writing checksums..."
SUMS="release/checksums.txt"
ARTIFACT_NAMES=()
for f in "${ARTIFACTS[@]}"; do ARTIFACT_NAMES+=("$(basename "$f")"); done
( cd release && shasum -a 256 "${ARTIFACT_NAMES[@]}" ) > "$SUMS"
ARTIFACTS+=("$SUMS")

# ---- PUBLISH -------------------------------------------------------------
#
# Idempotent by construction. The tag is pushed before this script runs
# (that push is what triggers the workflow), so a run that dies here leaves
# a tag with no release. Re-dispatching must repair that rather than fail on
# "release already exists" — hence create-or-clobber.
#
# Notes come from the CHANGELOG committed in the tagged commit. A hand-made
# rehearsal tag (v9.9.9-rc1) has no matching section, and empty notes are
# fine: they are not an error condition.

NOTES=$(awk "/^## \[${VERSION}\]/{found=1; next} found && /^## \[/{exit} found" CHANGELOG.md)
[[ -n "$NOTES" ]] || echo "note: no '## [${VERSION}]' section in CHANGELOG.md — publishing with empty notes."

if gh release view "$TAG" --repo "$REPO" >/dev/null 2>&1; then
    echo "Release ${TAG} exists — updating its assets..."
    gh release upload "$TAG" --repo "$REPO" --clobber "${ARTIFACTS[@]}"
else
    echo "Creating GitHub release ${TAG}..."
    gh release create "$TAG" --repo "$REPO" --title "$TAG" --notes "$NOTES" "${ARTIFACTS[@]}"
fi

echo ""
echo "Published ${TAG}:"
echo "  https://github.com/${REPO}/releases/tag/${TAG}"
