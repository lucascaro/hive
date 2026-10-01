#!/usr/bin/env bash
# Self-test for check-test-hooks-stripped.sh, run in CI beside the gate.
#
# CI only ever hands the gate a clean bundle, so its pass path is all a
# normal run proves. This feeds it fixture dists that must fail — a
# leaked hook (exit 1), an empty dist and a missing one (exit 2) — plus
# a clean one (exit 0), so a broken gate cannot fail open.
set -euo pipefail
cd "$(dirname "$0")/.."
GATE="$PWD/scripts/check-test-hooks-stripped.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

expect() { # expect <want-exit> <dist> <description>
  local want="$1" dist="$2" desc="$3" got=0
  "$GATE" "$dist" >/dev/null 2>&1 || got=$?
  if [[ "$got" != "$want" ]]; then
    echo "FAIL: $desc (exit $got, want $want)" >&2
    exit 1
  fi
  echo "ok: $desc"
}

mkdir -p "$tmp/clean/assets"
echo 'console.log("app");' > "$tmp/clean/assets/index.js"
expect 0 "$tmp/clean" "clean bundle passes"

for hook in __hive_commandLog __hive_state; do
  mkdir -p "$tmp/leak-$hook/assets"
  echo 'console.log("app");' > "$tmp/leak-$hook/assets/index.js"
  echo "window.$hook=[];" > "$tmp/leak-$hook/assets/chunk.js"
  expect 1 "$tmp/leak-$hook" "leaked $hook fails"
done

mkdir -p "$tmp/empty"
echo '<html></html>' > "$tmp/empty/index.html"
expect 2 "$tmp/empty" "dist with no .js fails"

expect 2 "$tmp/missing" "missing dist fails"

echo "check-test-hooks-stripped selftest: OK"
