#!/usr/bin/env bash
# measure-gui-idle.sh — sample the Hive app frontend while idle.
#
# Serves the frontend (this tree, or --ref <git-ref> from a temporary
# worktree) on the Playwright mock bridge, opens it in headless Chromium
# with no plugins installed, and records:
#   boot   — navigation to the first sidebar row (ms)
#   busy   — main-thread task time per second of idle (ms/s), from CDP
#   heap   — JS heap in use at the end (MiB)
# The frontend is the only part of the GUI a UI-plugin change touches:
# the Go side of the app is a thin client. It is a proxy for the real
# webview (WKWebView / WebView2), measured the same way on both sides.
#
#   scripts/measure-gui-idle.sh                     # this tree
#   scripts/measure-gui-idle.sh --ref origin/main   # a baseline
#   scripts/measure-gui-idle.sh --seconds 30 --runs 3
#
# Needs the frontend's node_modules (npm ci) in this checkout; a --ref
# baseline borrows them.
set -euo pipefail

ref=""
seconds=30
runs=3
while [[ $# -gt 0 ]]; do
  case "$1" in
    --ref) ref="$2"; shift 2 ;;
    --seconds) seconds="$2"; shift 2 ;;
    --runs) runs="$2"; shift 2 ;;
    -h|--help) sed -n '2,20p' "$0"; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

root="$(git rev-parse --show-toplevel)"
fe_root="$root/cmd/hivegui/frontend"
[[ -d "$fe_root/node_modules" ]] || { echo "run npm ci in $fe_root first" >&2; exit 1; }
tmp="$(mktemp -d /tmp/hive-gui-idle.XXXXXX)"
src="$root"
vite=""
cleanup() {
  if [[ -n "$vite" ]]; then kill "$vite" 2>/dev/null || true; wait "$vite" 2>/dev/null || true; fi
  if [[ "$src" != "$root" ]]; then git -C "$root" worktree remove --force "$src" >/dev/null 2>&1 || true; fi
  rm -rf "$tmp"
}
trap cleanup EXIT

if [[ -n "$ref" ]]; then
  src="$tmp/src"
  git -C "$root" worktree add --detach --quiet "$src" "$ref"
  ln -s "$fe_root/node_modules" "$src/cmd/hivegui/frontend/node_modules"
fi
fe="$src/cmd/hivegui/frontend"
port=$((20000 + RANDOM % 20000))
(cd "$fe" && VITE_WAILS_MOCK=1 VITE_PORT="$port" ./node_modules/.bin/vite >"$tmp/vite.log" 2>&1) &
vite=$!
for _ in $(seq 1 60); do
  curl -sf "http://localhost:$port/" >/dev/null && break
  sleep 0.5
done
curl -sf "http://localhost:$port/" >/dev/null || { echo "vite did not start; log:" >&2; cat "$tmp/vite.log" >&2; exit 1; }

cat >"$tmp/measure.mjs" <<'JS'
import { chromium } from 'playwright';
const [url, seconds] = [process.argv[2], Number(process.argv[3])];
const browser = await chromium.launch();
const page = await browser.newPage();
const cdp = await page.context().newCDPSession(page);
await cdp.send('Performance.enable');
const t0 = Date.now();
await page.goto(url);
await page.waitForFunction(() => document.querySelectorAll('#projects li').length > 0);
const boot = Date.now() - t0;
await page.waitForTimeout(3000); // let boot chores settle
const metric = async (name) =>
  (await cdp.send('Performance.getMetrics')).metrics.find((m) => m.name === name).value;
const task0 = await metric('TaskDuration');
await page.waitForTimeout(seconds * 1000);
const busy = ((await metric('TaskDuration')) - task0) * 1000 / seconds;
const heap = (await metric('JSHeapUsedSize')) / 1048576;
console.log(JSON.stringify({ boot, busy, heap }));
await browser.close();
JS
cp "$tmp/measure.mjs" "$fe_root/.measure-gui-idle.mjs"
trap 'rm -f "$fe_root/.measure-gui-idle.mjs"; cleanup' EXIT

label="${ref:-working tree}"
for ((r = 1; r <= runs; r++)); do
  out="$(cd "$fe_root" && node .measure-gui-idle.mjs "http://localhost:$port/" "$seconds")"
  node -e 'const m = JSON.parse(process.argv[1]); console.log(`${process.argv[2]} run ${process.argv[3]}: boot ${m.boot} ms  busy ${m.busy.toFixed(2)} ms/s  heap ${m.heap.toFixed(1)} MiB`)' "$out" "$label" "$r"
done
