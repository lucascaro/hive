#!/usr/bin/env bash
set -euo pipefail

# release.sh — create a new hive release
#
# Usage: ./scripts/release.sh <version>
#   e.g. ./scripts/release.sh 0.2.0
#
# What it does:
#   1. Validates inputs and working tree
#   2. Bumps the version constant in cmd/version.go
#   3. Stamps CHANGELOG.md with the release date
#   4. Commits and tags
#   5. Cross-compiles binaries for all supported platforms
#   6. Creates a GitHub release with binaries attached
#   7. Pushes the commit and tag
#   8. Cleans up build artifacts

cd "$(git rev-parse --show-toplevel)"

VERSION="${1:-}"
if [[ -z "$VERSION" ]]; then
    echo "Usage: $0 <version>"
    echo "  e.g. $0 0.2.0"
    exit 1
fi

TAG="v${VERSION}"
TODAY=$(date +%Y-%m-%d)

# --- Validation -----------------------------------------------------------

if ! command -v gh &>/dev/null; then
    echo "Error: gh (GitHub CLI) is required but not installed."
    exit 1
fi

if [[ -n "$(git status --porcelain)" ]]; then
    echo "Error: working tree is not clean. Commit or stash changes first."
    exit 1
fi

if git rev-parse "$TAG" &>/dev/null; then
    echo "Error: tag $TAG already exists."
    exit 1
fi

if ! grep -q '## \[Unreleased\]' CHANGELOG.md; then
    echo "Error: CHANGELOG.md has no [Unreleased] section."
    exit 1
fi

# --- Version bump ---------------------------------------------------------

echo "Bumping version to ${VERSION}..."
sed -i.bak "s/const Version = \".*\"/const Version = \"${VERSION}\"/" cmd/version.go
rm -f cmd/version.go.bak

# --- Changelog stamp ------------------------------------------------------

echo "Stamping changelog..."

# Find the previous version tag for the compare link
PREV_TAG=$(git tag -l 'v*' --sort=-v:refname | head -1)
if [[ -z "$PREV_TAG" ]]; then
    echo "Warning: no previous version tag found. Changelog links may be incomplete."
    PREV_TAG="v0.0.0"
fi

# Replace [Unreleased] heading with version + date, add new [Unreleased]
sed -i.bak "s/^## \[Unreleased\]/## [Unreleased]\n\n## [${VERSION}] — ${TODAY}/" CHANGELOG.md

# Update bottom links
# Replace the existing [Unreleased] compare link
sed -i.bak "s|\[Unreleased\]: https://github.com/lucascaro/hive/compare/.*\.\.\.HEAD|[Unreleased]: https://github.com/lucascaro/hive/compare/${TAG}...HEAD\n[${VERSION}]: https://github.com/lucascaro/hive/compare/${PREV_TAG}...${TAG}|" CHANGELOG.md

rm -f CHANGELOG.md.bak

# --- Commit and tag -------------------------------------------------------

echo "Committing and tagging ${TAG}..."
git add cmd/version.go CHANGELOG.md
git commit -m "release: ${TAG}"
git tag "$TAG"

# --- Cross-compile --------------------------------------------------------

echo "Cross-compiling binaries..."
mkdir -p dist

PLATFORMS=(
    "darwin/arm64"
    "darwin/amd64"
    "linux/amd64"
    "linux/arm64"
    "windows/amd64"
)

for platform in "${PLATFORMS[@]}"; do
    GOOS="${platform%/*}"
    GOARCH="${platform#*/}"
    output="dist/hive-${GOOS}-${GOARCH}"
    if [[ "$GOOS" == "windows" ]]; then
        output="${output}.exe"
    fi
    echo "  Building ${output}..."
    GOOS="$GOOS" GOARCH="$GOARCH" go build -ldflags="-s -w" -o "$output" .
done

# --- Push -----------------------------------------------------------------

echo "Pushing to origin..."
git push origin main "$TAG"

# --- GitHub release -------------------------------------------------------

echo "Creating GitHub release ${TAG}..."

# Extract the changelog section for this version (between ## [version] and the next ## [)
NOTES=$(awk "/^## \[${VERSION}\]/{found=1; next} found && /^## \[/{exit} found" CHANGELOG.md)

gh release create "$TAG" \
    --title "$TAG" \
    --notes "$NOTES" \
    dist/*

# --- Cleanup --------------------------------------------------------------

rm -rf dist
echo ""
echo "Released ${TAG} successfully!"
echo "  https://github.com/lucascaro/hive/releases/tag/${TAG}"
