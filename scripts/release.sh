#!/usr/bin/env bash
set -euo pipefail

# release.sh — cut a release for this project.
#
# Usage: ./scripts/release.sh <version> [--local-artifacts]
#        e.g. ./scripts/release.sh 0.2.0
#
# What it does:
#   1. Validates inputs and working tree
#   2. Bumps the version in VERSION_FILE
#   3. Rolls .changesets/*.md into a stamped CHANGELOG section and deletes
#      them (falls back to a plain date stamp if the repo has no changesets)
#   4. Commits and tags
#   5. Verifies origin/main has not advanced, then pushes the commit and tag
#
# The tag push triggers .github/workflows/release.yml, which builds, signs,
# notarizes and publishes on a macOS runner. That is where notarization's
# unbounded wait now happens, instead of on your terminal.
#
# Flags:
#   --local-artifacts  Do the build/sign/publish half here instead of in CI,
#                      via scripts/release-artifacts.sh. Requires the signing
#                      credentials this script otherwise does not need.
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
# loop the hivesmith template provides. The BUILD_CMD/PLATFORMS knobs from
# the template have been removed. That half of the release now lives in
# scripts/release-artifacts.sh, run on a macOS runner by
# .github/workflows/release.yml (or here, with --local-artifacts).

# ---- VALIDATION ----------------------------------------------------------

cd "$(git rev-parse --show-toplevel)"

VERSION=""
local_artifacts=0
check_preflight=0
while [[ $# -gt 0 ]]; do
    case "$1" in
        --local-artifacts) local_artifacts=1; shift ;;
        # Test hook: run the pre-flight, print a marker describing what it
        # concluded, and stop before mutating anything. The marker matters —
        # a flag that merely exited 0 would also be satisfied by a version
        # that short-circuits before reaching any pre-flight logic at all,
        # so scripts/release-artifacts-selftest.sh greps for this line
        # rather than trusting the exit code.
        --check-preflight) check_preflight=1; shift ;;
        -h|--help) sed -n '4,24p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
        -*) echo "unknown flag: $1" >&2; exit 2 ;;
        *)  [[ -z "$VERSION" ]] || { echo "unexpected argument: $1" >&2; exit 2; }
            VERSION="$1"; shift ;;
    esac
done

if [[ -z "$VERSION" ]]; then
    echo "Usage: $0 <version> [--local-artifacts]"
    echo "  e.g. $0 0.2.0"
    exit 1
fi

TAG="v${VERSION}"
TODAY=$(date +%Y-%m-%d)

command -v gh >/dev/null || { echo "Error: gh (GitHub CLI) required"; exit 1; }
# --check-preflight never mutates the tree, so it does not need a clean one —
# and must not require it: the pin-guard test below mutates signing.go inside
# a scratch worktree, and a clean-tree refusal there would mask the very
# guard the test is trying to exercise.
if [[ "$check_preflight" == "0" ]]; then
    [[ -z "$(git status --porcelain)" ]] || { echo "Error: working tree not clean"; exit 1; }
    ! git rev-parse "$TAG" &>/dev/null || { echo "Error: tag $TAG already exists"; exit 1; }
    grep -q '## \[Unreleased\]' CHANGELOG.md || { echo "Error: CHANGELOG.md has no [Unreleased] section"; exit 1; }
fi

# ---- PRE-FLIGHT ----------------------------------------------------------
#
# Deliberately here, next to the other refusals, rather than next to the
# work it guards: everything below the commit/tag step is expensive to
# unwind, and these are all knowable now.
#
# The block splits in two, and the split is not cosmetic. Under
# `set -euo pipefail`, touching $HIVE_SIGN_IDENTITY when it is unset is a
# hard crash — so every line that reads it has to sit behind the flag that
# promises it exists.

# Credential-FREE, and deliberately outside any `uname` test: refuse to
# publish a build that pins no team. It would skip signature verification
# for every user who installs it, permanently — which is true no matter
# which machine or OS produced the build, so this must also fire on the
# Linux CI legs where the selftest runs.
pinned_team_id="$(sed -n 's/^var signingTeamID = "\(.*\)"$/\1/p' internal/buildinfo/signing.go)"
if [[ -z "$pinned_team_id" ]]; then
    echo "Error: internal/buildinfo/signing.go pins no Team ID." >&2
    echo "  A release built from it would skip update signature verification for" >&2
    echo "  every user. Set signingTeamID — see docs/releasing-signed-macos.md." >&2
    exit 1
fi

# Credential-DEPENDENT. Only the local-artifacts path signs here; the default
# path pushes a tag and lets the macOS runner sign, so it needs no
# certificate, no keychain profile and no xcrun. Requiring them by default
# would defeat the entire point of moving the build to CI.
if [[ "$local_artifacts" == "1" && "$(uname -s)" == "Darwin" ]]; then
    : "${HIVE_SIGN_IDENTITY:?set HIVE_SIGN_IDENTITY to your Developer ID (see docs/releasing-signed-macos.md)}"
    : "${HIVE_NOTARY_PROFILE:?set HIVE_NOTARY_PROFILE to your notarytool keychain profile (see docs/releasing-signed-macos.md)}"

    identity_team_id="$(sed -n 's/.*(\([A-Z0-9]\{10\}\))$/\1/p' <<<"$HIVE_SIGN_IDENTITY")"
    [[ "$identity_team_id" == "$pinned_team_id" ]] || {
        echo "Error: signing identity team ($identity_team_id) does not match the pinned team ($pinned_team_id)." >&2
        echo "  No client could install this release." >&2
        exit 1
    }

    security find-identity -v -p codesigning | grep -qF "$HIVE_SIGN_IDENTITY" || {
        echo "Error: signing identity not in keychain: $HIVE_SIGN_IDENTITY" >&2
        exit 1
    }
    command -v xcrun >/dev/null || { echo "Error: xcrun required for notarization"; exit 1; }
    echo "Signing pre-flight OK (team ${pinned_team_id})."
fi

if [[ "$check_preflight" == "1" ]]; then
    if [[ "$local_artifacts" == "1" ]]; then signing=on; else signing=off; fi
    echo "preflight ok: team=${pinned_team_id} signing=${signing}"
    exit 0
fi

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
    # Stage every file regen-generated.py may have rewritten above, asking it
    # for the list rather than repeating one here. A hand-maintained copy is
    # exactly what went wrong once: site/features.json became a release-time
    # stamping target, this line still said CHANGELOG.md only, and the stamp
    # stayed uncommitted — so the released feature never reached the website
    # and the NEXT release aborted on the clean-tree guard above.
    # if/fi, not `[[ -e ]] && git add`: under `set -e` a false test as the
    # last command in the loop body exits the script, so a repo without one
    # of the optional targets would abort the release.
    while IFS= read -r generated; do
        if [[ -e "$generated" ]]; then
            git add "$generated"
        fi
    done < <(scripts/regen-generated.sh --list-targets)
fi
git commit -m "release: ${TAG}"
git tag "$TAG"

# ---- PUSH ----------------------------------------------------------------
#
# The push is now the LAST local step, not a step between signing and
# publishing. Pushing the tag is what triggers
# .github/workflows/release.yml, so everything after this happens on a
# macOS runner.
#
# The consequence is worth stating plainly: a CI run that fails leaves the
# tag pushed and no GitHub release behind it. That is why
# release-artifacts.sh publishes idempotently (create-or-clobber) and why
# the workflow accepts a workflow_dispatch — the repair is a re-dispatch,
# not deleting and re-pushing a tag.

# Pin check: refuse to push if main advanced after we started. This is the
# reason the commit/tag half stays local at all — a runner has no way to
# know whether main moved under the maintainer mid-release.
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

# ---- ARTIFACTS -----------------------------------------------------------
#
# The two paths push DIFFERENT things, and that is the whole trick.
#
# CI path: push the commit AND the tag. The tag push is the trigger; every-
# thing after it happens on a runner.
#
# --local-artifacts: push only the commit, never the tag. Build, sign and
# publish locally, and let `gh release create --target` create the remote
# tag as part of publishing — by which point the release already exists
# with all three assets, so the workflow that fires stands down (see the
# "Skip if this release is already complete" step in release.yml).
#
# Pushing the tag ourselves here would trigger release.yml and then race it:
# two macOS builds signing and `gh release upload --clobber`-ing the same
# assets at once, with --clobber quietly making the collision look like
# success. Publishing first also restores the pre-split failure mode, which
# was the better one — a build that fails leaves no tag on the remote at
# all, rather than a dangling tag with no release behind it.

if [[ "$local_artifacts" == "1" ]]; then
    echo "Pushing the release commit (not the tag — see above)..."
    git push origin HEAD

    echo "Building and publishing artifacts locally (--local-artifacts)..."
    ./scripts/release-artifacts.sh "$VERSION"

    # The remote tag now exists (created by `gh release create --target`).
    # Line the local ref up with it so a follow-up `git push --tags` is a
    # no-op rather than a surprise.
    git fetch origin --tags --quiet

    echo ""
    echo "Released ${TAG} successfully!"
    echo "  https://github.com/${REPO}/releases/tag/${TAG}"
else
    echo "Pushing to origin..."
    git push origin HEAD "$TAG"

    echo ""
    echo "Pushed ${TAG}. The release build is running on GitHub Actions."
    # A run URL would need a run id, which does not exist until the workflow
    # is queued — and cannot be known here. Point at the workflow instead.
    echo "  https://github.com/${REPO}/actions/workflows/release.yml"
    echo ""
    echo "It builds, signs, notarizes and publishes the release. Notarization"
    echo "alone can take anywhere from 2 minutes to over an hour, entirely on"
    echo "Apple's side. Nothing further is required from you."
    echo ""
    echo "If the run fails, fix the cause and re-publish without re-tagging:"
    echo "  gh workflow run release.yml -f tag=${TAG}"
fi