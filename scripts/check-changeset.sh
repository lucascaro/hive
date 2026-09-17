#!/usr/bin/env bash
# check-changeset.sh — the changeset gate, shared by the pre-push hook and CI.
#
#   scripts/check-changeset.sh [BASE HEAD]
#
# Passes when the diff BASE...HEAD either adds a `.changesets/*.md` entry or
# touches only paths that never need one (EXEMPT below). With no arguments it
# compares HEAD against its merge-base with main — the local pre-push case.
# `.github/workflows/changesets.yml` calls it with the PR's base and head, so
# the hook and CI cannot disagree about what counts as exempt.
#
# Install the hook once per clone; the shared hooks dir covers every worktree:
#
#   cp scripts/hooks/pre-push "$(git rev-parse --git-common-dir)/hooks/pre-push"
#
# A change outside EXEMPT with no user-visible effect (an internal refactor)
# still needs the `no-changeset` label in CI; CI checks the label before
# running this script.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

# Extended regexes, matched against repo-relative paths. A path matching any
# of them is not user-visible: docs, planning files, root markdown, CI and dev
# tooling, tests. (CHANGELOG.md matches the root-markdown rule, but a PR that
# edits it fails the `block-generated-edits` job anyway.)
EXEMPT=(
  '^docs/'
  '^features/'
  '^[^/]+\.md$'
  '^\.github/'
  '^\.changesets/(README\.md|\.gitkeep)$'
  '^scripts/'
  '_test\.go$'
  '/testdata/'
  '(^|/)test/'
  '\.(test|spec)\.[cm]?[jt]sx?$'
)

if [[ $# -eq 2 ]]; then
  base="$1" head="$2"
elif [[ $# -eq 0 ]]; then
  head="$(git rev-parse HEAD)"
  base="$(git merge-base HEAD origin/main 2>/dev/null || git merge-base HEAD main 2>/dev/null || true)"
  # Detached from any main, or nothing new on this branch: nothing to gate.
  if [[ -z "$base" || "$base" == "$head" ]]; then
    exit 0
  fi
else
  echo "usage: $0 [BASE HEAD]" >&2
  exit 2
fi

changed="$(git diff --name-only "$base"..."$head")"

changesets="$(grep -E '^\.changesets/.+\.md$' <<<"$changed" | grep -vxF '.changesets/README.md' || true)"
if [[ -n "$changesets" ]]; then
  echo "OK: adds at least one changeset."
  exit 0
fi

pattern="$(IFS='|'; echo "${EXEMPT[*]}")"
needs="$(grep -vE "$pattern" <<<"$changed" || true)"
if [[ -z "$needs" ]]; then
  echo "OK: only docs, tooling and test paths changed — no changeset needed."
  exit 0
fi

echo "error: no .changesets/*.md entry, and these paths may be user-visible:" >&2
while IFS= read -r f; do echo "    $f"; done <<<"$needs" >&2
echo "  Add one (see .changesets/README.md for the schema)." >&2
echo "  No user-visible effect (e.g. an internal refactor)? Apply the" >&2
echo "  'no-changeset' label on the PR." >&2
exit 1
