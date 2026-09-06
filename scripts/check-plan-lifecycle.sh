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
# The PR header is written three ways across the plans in active/:
#   - **PR:** #338 · #341        (list form; a phased plan names each PR)
#   **PR:** #310                 (no leading dash)
#   **PR:** https://github.com/lucascaro/hive/pull/301   (URL)
# The first version of this check matched only the first, so
# ui-design-system-phase4 sat in active/ for a week after #301 merged and
# this script reported no drift. Take the text after `**PR:**`, then read
# every `#N` and `/pull/N` in it.
#
# `— (#160 was closed unmerged; ...)` must NOT match: that is prose about a
# dead PR on a plan whose PR field is deliberately empty. Hence the anchor —
# a number is only a PR reference when it starts the field or follows a
# separator, not when it appears mid-sentence.
for f in docs/exec-plans/active/*.md; do
  [ -e "$f" ] || continue
  field=$(sed -n 's/^[[:space:]]*-\{0,1\}[[:space:]]*\*\*PR:\*\*[[:space:]]*//p' "$f" | head -1)
  prs=$(printf '%s\n' "$field" \
    | grep -oE '(^|[·,] *)(#[0-9]+|https?://[^ ]*/pull/[0-9]+)' \
    | grep -oE '[0-9]+$')
  [ -n "$prs" ] || continue
  # A phased plan is only drifted when EVERY PR it names is finished — 336
  # sat in active/ with phases 1-3 merged and phase 4 still open, which is
  # correct, not drift.
  all_merged=1 all_closed=1 states=""
  for pr in $prs; do
    state=$(gh pr view "$pr" --json state --jq .state 2>/dev/null) || {
      echo "?? $(basename "$f"): PR #$pr — could not query gh"
      all_merged=0 all_closed=0
      continue
    }
    states="$states #$pr=$state"
    [ "$state" = MERGED ] || all_merged=0
    [ "$state" = CLOSED ] || all_closed=0
  done
  [ -n "$states" ] || continue
  if [ "$all_merged" -eq 1 ]; then
    echo "!! $(basename "$f"):$states — all MERGED, move to completed/"; drift=1
  elif [ "$all_closed" -eq 1 ]; then
    echo "!! $(basename "$f"):$states — all CLOSED unmerged, plan is stale"; drift=1
  else
    echo "ok $(basename "$f"):$states"
  fi
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
