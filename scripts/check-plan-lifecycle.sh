#!/usr/bin/env bash
# check-plan-lifecycle.sh — flag exec plans left in active/ after their PR merged.
#
# docs/exec-plans/active/ has drifted twice (247 sat in REVIEW for a month
# after #248 merged; 142 pointed at a PR that was closed unmerged). Two cheap
# checks: for every `- **PR:** #N` in active/, ask GitHub what happened to N;
# and for every spec, check its `Exec plan:` link still resolves.
#
# Needs `gh` authenticated. Not wired into CI on purpose — CI has no gh token
# for this and the answer changes without the tree changing.
set -uo pipefail
cd "$(dirname "$0")/.."

drift=0
for f in docs/exec-plans/active/*.md; do
  [ -e "$f" ] || continue
  pr=$(sed -n 's/^- \*\*PR:\*\* #\([0-9][0-9]*\).*/\1/p' "$f" | head -1)
  [ -n "$pr" ] || continue
  state=$(gh pr view "$pr" --json state --jq .state 2>/dev/null) || {
    echo "?? $(basename "$f"): PR #$pr — could not query gh"
    continue
  }
  case "$state" in
    MERGED) echo "!! $(basename "$f"): PR #$pr is MERGED — move to completed/"; drift=1 ;;
    CLOSED) echo "!! $(basename "$f"): PR #$pr is CLOSED unmerged — plan is stale"; drift=1 ;;
    *)      echo "ok $(basename "$f"): PR #$pr is $state" ;;
  esac
done
# Second class of drift, and the one that slipped past the first version of
# this script: a plan moves to completed/ and the spec that points at it keeps
# a link into active/. The link 404s and the spec's own Stage: goes stale.
for f in docs/product-specs/*.md; do
  [ -e "$f" ] || continue
  # The template's link is a <slug> placeholder, not a real target.
  [ "$(basename "$f")" = "_template.md" ] && continue
  link=$(sed -n 's|^- \*\*Exec plan:\*\* .*(\(\.\./exec-plans/[^)]*\)).*|\1|p' "$f" | head -1)
  [ -n "$link" ] || continue
  target="docs/product-specs/$link"
  if [ ! -e "$target" ]; then
    echo "!! $(basename "$f"): exec-plan link is dead ($link)"
    drift=1
  fi
done

[ "$drift" -eq 0 ] && echo "no plan-lifecycle drift"
exit "$drift"
