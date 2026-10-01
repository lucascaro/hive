#!/usr/bin/env bash
# check-test-hooks-stripped.sh — proves the production frontend bundle
# carries none of the e2e test hooks.
#
#   scripts/check-test-hooks-stripped.sh [DIST_DIR]
#
# The hooks (window.__hive_state in store/store.ts, window.__hive_commandLog
# in app/command-registry.ts, spec 481) sit behind
# `import.meta.env.VITE_WAILS_MOCK/REAL === '1'`. A plain `npm run build`
# inlines those to literals and the bundler drops the block — this checks
# that it really did. Run it after `npm run build`; CI does, on every leg.
# DIST_DIR defaults to cmd/hivegui/frontend/dist, resolved from this
# script's location, not the caller's cwd.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
dist="${1:-$here/../cmd/hivegui/frontend/dist}"
HOOKS=(__hive_commandLog __hive_state)

if [[ ! -d "$dist" ]]; then
  echo "check-test-hooks-stripped: no bundle at $dist (run npm run build)" >&2
  exit 2
fi

# An empty dist would pass the grep below vacuously.
js=()
while IFS= read -r -d '' f; do js+=("$f"); done \
  < <(find "$dist" -type f -name '*.js' -print0)
if [[ ${#js[@]} -eq 0 ]]; then
  echo "check-test-hooks-stripped: no .js files under $dist" >&2
  exit 2
fi

status=0
for hook in "${HOOKS[@]}"; do
  if grep -l -F -- "$hook" "${js[@]}"; then
    echo "check-test-hooks-stripped: '$hook' is in the production bundle (files above)" >&2
    status=1
  fi
done
[[ $status -eq 0 ]] && echo "check-test-hooks-stripped: OK (${#js[@]} files, no test hooks)"
exit $status
