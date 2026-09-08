#!/usr/bin/env bash
set -euo pipefail

# release-standdown.sh — decide whether .github/workflows/release.yml should
# skip a run because the release is already published in full.
#
# Usage: scripts/release-standdown.sh <tag>
# Prints "skip" or "run" on stdout. Never fails on a missing release.
#
# This lives in a script rather than inline workflow bash so it can be tested:
# a sort or whitespace bug that wrongly answered "skip" would turn a repair
# dispatch into a silent no-op, and inline `run:` blocks are unreachable from
# the selftest.
#
# "Complete" deliberately means ALL THREE assets. A release left partial by a
# run that died mid-upload must NOT stand down — repairing exactly that is why
# the workflow_dispatch trigger exists.
#
# Environment:
#   REPO   owner/repo (default: lucascaro/hive)

REPO="${REPO:-lucascaro/hive}"

TAG="${1:-}"
[[ -n "$TAG" ]] || { echo "usage: $0 <tag>" >&2; exit 2; }
VERSION="${TAG#v}"

if ! gh release view "$TAG" --repo "$REPO" --json assets >/dev/null 2>&1; then
    echo "run"
    exit 0
fi

# A draft is never complete: the tag does not exist yet, and a draft left by a
# died run is precisely what a re-dispatch should finish.
if [[ "$(gh release view "$TAG" --repo "$REPO" --json isDraft --jq '.isDraft')" == "true" ]]; then
    echo "run"
    exit 0
fi

# LC_ALL=C is load-bearing. jq's `sort` orders bytewise, so "Hive-..." (0x48)
# comes before "checksums.txt" (0x63); a locale-collated shell `sort` ignores
# case and puts "checksums.txt" first. Without pinning the collation the two
# sides could never match, and the stand-down would silently never fire —
# which is a rebuild-forever bug rather than a loud one.
have="$(gh release view "$TAG" --repo "$REPO" --json assets --jq '[.assets[].name] | sort | join(" ")')"
want="$(LC_ALL=C; export LC_ALL; printf '%s\n' \
    "Hive-${VERSION}-macos-universal.zip" \
    "Hive-${VERSION}-windows-amd64.zip" \
    "checksums.txt" | sort | tr '\n' ' ' | sed 's/ $//')"

if [[ "$have" == "$want" ]]; then
    echo "skip"
else
    echo "run"
fi
