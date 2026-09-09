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

# Membership, not ordering. An earlier revision compared two sorted, joined
# strings and had to pin LC_ALL=C to make them agree — jq's `sort` orders
# bytewise while a locale-collated shell `sort` ignores case, so
# "checksums.txt" and "Hive-..." landed on different sides and the comparison
# could never match. That bug is silent in the worst way: the stand-down
# simply never fires, and every local release quietly gets rebuilt by CI.
#
# Asking "is each expected asset present, and are there exactly three?"
# removes the ordering question instead of pinning it, so there is no
# collation left to get wrong and nothing locale-dependent to test.
# A `while read` loop rather than `mapfile`: macOS ships bash 3.2, where
# mapfile does not exist, and this script runs on the macOS release runner.
have=()
while IFS= read -r line; do
    [[ -n "$line" ]] && have+=("$line")
done < <(gh release view "$TAG" --repo "$REPO" --json assets --jq '.assets[].name')

want=(
    "Hive-${VERSION}-macos-universal.zip"
    "Hive-${VERSION}-windows-amd64.zip"
    "checksums.txt"
)

if [[ "${#have[@]}" -ne "${#want[@]}" ]]; then
    echo "run"
    exit 0
fi

for name in "${want[@]}"; do
    found=0
    for got in "${have[@]}"; do
        [[ "$got" == "$name" ]] && { found=1; break; }
    done
    if [[ "$found" -eq 0 ]]; then
        echo "run"
        exit 0
    fi
done

echo "skip"
