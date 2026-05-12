#!/usr/bin/env bash
set -euo pipefail

# test.sh — run the full Hive test harness.
#
# Layers:
#   go        Go unit + integration tests (daemon, session, registry, ...)
#   unit      Vitest unit tests for the frontend lib/ modules
#   dom       Vitest jsdom tests (sidebar tree, visibility gate)
#   e2e       Playwright E2E driven by the Wails-mock bridge
#
# Usage:
#   scripts/test.sh           # all layers
#   scripts/test.sh go        # just Go
#   scripts/test.sh unit dom  # subset

cd "$(dirname "$0")/.."

LAYERS=("$@")
if [ ${#LAYERS[@]} -eq 0 ]; then
  LAYERS=(go unit dom e2e)
fi

run_go() {
  echo "==> go test ./..."
  go test ./... -count=1 -timeout 120s
}

run_frontend() {
  # Lazy-install: only run npm install if node_modules is missing.
  if [ ! -d cmd/hivegui/frontend/node_modules ]; then
    echo "==> npm install (frontend)"
    (cd cmd/hivegui/frontend && npm install --silent)
  fi
}

run_unit() {
  run_frontend
  echo "==> vitest run test/unit"
  (cd cmd/hivegui/frontend && ./node_modules/.bin/vitest run test/unit)
}

run_dom() {
  run_frontend
  echo "==> vitest run test/dom"
  (cd cmd/hivegui/frontend && ./node_modules/.bin/vitest run test/dom)
}

run_e2e() {
  run_frontend
  # Install browsers on demand; Playwright is idempotent here.
  echo "==> playwright install (chromium)"
  (cd cmd/hivegui/frontend && ./node_modules/.bin/playwright install chromium >/dev/null)
  echo "==> playwright test"
  (cd cmd/hivegui/frontend && ./node_modules/.bin/playwright test)
}

for layer in "${LAYERS[@]}"; do
  case "$layer" in
    go)   run_go ;;
    unit) run_unit ;;
    dom)  run_dom ;;
    e2e)  run_e2e ;;
    *) echo "unknown layer: $layer" >&2; exit 2 ;;
  esac
done

echo "==> all tests passed"
