#!/usr/bin/env bash
set -euo pipefail

# release.sh — cut a release for this project.
#
# Usage: ./scripts/release.sh <version>     e.g. ./scripts/release.sh 0.2.0
#
# What it does:
#   1. Validates inputs and working tree
#   2. Bumps the version in VERSION_FILE
#   3. Rolls .changesets/*.md into a stamped CHANGELOG section and deletes
#      them (falls back to a plain date stamp if the repo has no changesets)
#   4. Commits and tags
#   5. (Optional) cross-compiles artifacts via BUILD_CMD
#   6. Creates a GitHub release with artifacts attached
#   7. Pushes the commit and tag
#
# Configuration — edit the variables below for your project.

# ---- CONFIG --------------------------------------------------------------

# Project/binary name used in artifact filenames and the release message.
PROJECT="${PROJECT:-hive}"

# GitHub owner/repo, used in CHANGELOG compare links.
REPO="${REPO:-lucascaro/hive}"

# File containing the version string. Leave empty to skip the version bump.
# Hive has no in-repo version constant — version lives in git tags only.
VERSION_FILE="${VERSION_FILE:-}"

# A sed expression that replaces the version in VERSION_FILE. `__VERSION__`
# is substituted with the new version before running. Leave empty to write
# the version verbatim to VERSION_FILE.
# Examples:
#   Go constant:     's/const Version = ".*"/const Version = "__VERSION__"/'
#   package.json:    's/"version": ".*"/"version": "__VERSION__"/'
#   Cargo.toml:      's/^version = ".*"/version = "__VERSION__"/'
VERSION_SED="${VERSION_SED:-}"

# Hive's release artifacts are produced by ./build.sh (Wails .app for
# macOS + cross-compiled Windows zip), not by the generic per-platform
# loop the hivesmith template provides. The BUILD_CMD/PLATFORMS knobs
# from the template have been removed; see "BUILD ARTIFACTS" below.

# ---- VALIDATION ----------------------------------------------------------

cd "$(git rev-parse --show-toplevel)"

VERSION="${1:-}"
if [[ -z "$VERSION" ]]; then
    echo "Usage: $0 <version>"
    echo "  e.g. $0 0.2.0"
    exit 1
fi

TAG="v${VERSION}"
TODAY=$(date +%Y-%m-%d)

command -v gh >/dev/null || { echo "Error: gh (GitHub CLI) required"; exit 1; }
[[ -z "$(git status --porcelain)" ]] || { echo "Error: working tree not clean"; exit 1; }
! git rev-parse "$TAG" &>/dev/null || { echo "Error: tag $TAG already exists"; exit 1; }
grep -q '## \[Unreleased\]' CHANGELOG.md || { echo "Error: CHANGELOG.md has no [Unreleased] section"; exit 1; }

# Non-blocking reminder: shipped exec-plans should move active/ -> completed/
# (PLANS.md lifecycle) at merge time. Surface any stragglers before a release.
active_plans=$(find docs/exec-plans/active -maxdepth 1 -name '*.md' 2>/dev/null | wc -l | tr -d ' ')
[[ "$active_plans" -gt 0 ]] && echo "Note: $active_plans exec-plan(s) still in docs/exec-plans/active/ — move any shipped in this release to completed/ (see PLANS.md)."

# Pin to the start-of-release SHA. Every step below operates against this
# revision; if main advances mid-release (e.g. the regenerator bot lands a
# commit), we detect the drift before pushing and refuse to clobber it.
RELEASE_SHA="$(git rev-parse HEAD)"
echo "Pinned release base: ${RELEASE_SHA}"

# Detect the changeset-driven layout. New layout: the CHANGELOG's
# [Unreleased] body is generated from .changesets/*.md by
# scripts/regen-generated.sh. Old layout: CHANGELOG.md is hand-edited.
USE_CHANGESETS=0
if [[ -d .changesets ]] && [[ -x scripts/regen-generated.sh ]]; then
    USE_CHANGESETS=1
    echo "Detected .changesets/ layout — release will roll changesets into the version section."
fi

# ---- VERSION BUMP --------------------------------------------------------

if [[ -n "$VERSION_FILE" ]]; then
    echo "Bumping version to ${VERSION} in ${VERSION_FILE}..."
    if [[ -n "$VERSION_SED" ]]; then
        expr="${VERSION_SED//__VERSION__/$VERSION}"
        sed -i.bak "$expr" "$VERSION_FILE"
        rm -f "${VERSION_FILE}.bak"
    else
        echo "$VERSION" > "$VERSION_FILE"
    fi
fi

# ---- CHANGELOG STAMP -----------------------------------------------------

echo "Stamping changelog..."

# git's -v:refname sorts prerelease tags ABOVE the matching release
# (e.g. v2.0.0-alpha.2 > v2.0.0), which produces wrong compare links.
# Tell sort to treat "-" as a prerelease marker so v2.0.0 > v2.0.0-alpha.2.
PREV_TAG=$(git -c versionsort.suffix=- tag -l 'v*' --sort=-v:refname | head -1)
[[ -n "$PREV_TAG" ]] || PREV_TAG="v0.0.0"

if [[ "$USE_CHANGESETS" == "1" ]]; then
    # Promotes the generated [Unreleased] body into a stamped section and
    # leaves [Unreleased] empty; the consumed changesets go with it.
    scripts/regen-generated.sh --release "$VERSION"
    find .changesets -name '*.md' ! -name 'README.md' ! -name '.gitkeep' -delete
else
    sed -i.bak "s/^## \[Unreleased\]/## [Unreleased]\n\n## [${VERSION}] — ${TODAY}/" CHANGELOG.md
fi

# Update or append compare links at bottom
if grep -q "^\[Unreleased\]: " CHANGELOG.md; then
    sed -i.bak "s|\[Unreleased\]: https://github.com/${REPO}/compare/.*\.\.\.HEAD|[Unreleased]: https://github.com/${REPO}/compare/${TAG}...HEAD\n[${VERSION}]: https://github.com/${REPO}/compare/${PREV_TAG}...${TAG}|" CHANGELOG.md
fi
rm -f CHANGELOG.md.bak

# ---- COMMIT + TAG --------------------------------------------------------

echo "Committing and tagging ${TAG}..."
git add CHANGELOG.md ${VERSION_FILE:+"$VERSION_FILE"}
if [[ "$USE_CHANGESETS" == "1" ]]; then
    git add -A .changesets/
fi
git commit -m "release: ${TAG}"
git tag "$TAG"

# ---- BUILD ARTIFACTS -----------------------------------------------------
#
# Hive uses Wails for the GUI and a separate `hived` daemon binary, so the
# generic GOOS/GOARCH loop in the hivesmith template can't produce the
# right .app/.exe bundles. Delegate to build.sh, which knows how to
# assemble the macOS universal .app and the Windows amd64 zip.

ARTIFACTS=()
echo "Building release artifacts via build.sh..."
./build.sh --zip --version "$VERSION" --platform all
for f in \
    "release/Hive-${VERSION}-macos-universal.zip" \
    "release/Hive-${VERSION}-windows-amd64.zip"; do
    [[ -f "$f" ]] || { echo "Error: expected artifact missing: $f"; exit 1; }
    ARTIFACTS+=("$f")
done

# SHA-256 manifest. The GUI's in-app updater downloads this alongside
# the macOS zip and refuses to install on a mismatch — without it, a
# truncated download becomes a broken Hive.app the user can only fix by
# reinstalling by hand. Names are basenamed so the manifest matches the
# asset names GitHub serves.
echo "Writing checksums..."
SUMS="release/checksums.txt"
( cd release && shasum -a 256 $(printf '%s\n' "${ARTIFACTS[@]}" | xargs -n1 basename) ) > "$SUMS"
ARTIFACTS+=("$SUMS")

# ---- PUSH ----------------------------------------------------------------

# Pin check: refuse to push if main advanced after we started.
echo "Verifying release base is still tip of main..."
git fetch origin main --quiet
CURRENT_REMOTE_SHA="$(git rev-parse origin/main)"
if [[ "$CURRENT_REMOTE_SHA" != "$RELEASE_SHA" ]]; then
    echo "Error: origin/main has advanced since release started." >&2
    echo "  release base : ${RELEASE_SHA}" >&2
    echo "  current main : ${CURRENT_REMOTE_SHA}" >&2
    echo "Rebase and retry: git reset --hard ${TAG}^ && git tag -d ${TAG} && git pull --ff-only && $0 ${VERSION}" >&2
    exit 1
fi

echo "Pushing to origin..."
git push origin HEAD "$TAG"

# ---- GITHUB RELEASE ------------------------------------------------------

echo "Creating GitHub release ${TAG}..."
NOTES=$(awk "/^## \[${VERSION}\]/{found=1; next} found && /^## \[/{exit} found" CHANGELOG.md)
gh release create "$TAG" --title "$TAG" --notes "$NOTES" ${ARTIFACTS[@]+"${ARTIFACTS[@]}"}

# ---- CLEANUP -------------------------------------------------------------

echo ""
echo "Released ${TAG} successfully!"
echo "  https://github.com/${REPO}/releases/tag/${TAG}"
