#!/usr/bin/env bash
# check-plan-lifecycle.sh — flag exec plans left in active/ after their PR merged.
#
# docs/exec-plans/active/ has drifted twice (247 sat in REVIEW for a month
# after #248 merged; 142 pointed at a PR that was closed unmerged). This is
# the cheap check: for every `- **PR:** #N` in active/, ask GitHub what
# happened to N.
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
[ "$drift" -eq 0 ] && echo "no plan-lifecycle drift"
exit "$drift"
