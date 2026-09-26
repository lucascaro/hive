#!/usr/bin/env bash
# measure-idle.sh — sample an idle hived's CPU and memory.
#
# Builds hived (from the working tree, or from --ref <git-ref> in a
# temporary worktree), starts it fully isolated — its own socket, state
# dir and HOME under a temp dir, never your real Hive — with one idle
# shell session and no plugins, and samples `ps` once a second.
#
#   scripts/measure-idle.sh                     # this tree
#   scripts/measure-idle.sh --ref origin/main   # a baseline
#   scripts/measure-idle.sh --seconds 120
#
# Prints mean and max RSS (KiB) and %CPU. Compare a branch against its
# base to show a change adds no idle cost.
set -euo pipefail

ref=""
seconds=60
while [[ $# -gt 0 ]]; do
  case "$1" in
    --ref) ref="$2"; shift 2 ;;
    --seconds) seconds="$2"; shift 2 ;;
    -h|--help) sed -n '2,15p' "$0"; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

root="$(git rev-parse --show-toplevel)"
tmp="$(mktemp -d /tmp/hive-idle.XXXXXX)"
src="$root"
pid=""
cleanup() {
  if [[ -n "$pid" ]]; then kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; fi
  if [[ "$src" != "$root" ]]; then git -C "$root" worktree remove --force "$src" >/dev/null 2>&1 || true; fi
  rm -rf "$tmp"
}
trap cleanup EXIT

if [[ -n "$ref" ]]; then
  src="$tmp/src"
  git -C "$root" worktree add --detach --quiet "$src" "$ref"
fi
(cd "$src" && go build -o "$tmp/hived" ./cmd/hived)

export HIVE_SOCKET="$tmp/h.sock" HIVE_STATE_DIR="$tmp/state" HOME="$tmp"
"$tmp/hived" --socket "$HIVE_SOCKET" --shell /bin/sh --cols 80 --rows 24 2>"$tmp/hived.log" &
pid=$!

# Let boot chores (revive, orphan scan) finish before measuring idle.
sleep 5

rss_sum=0 rss_max=0 cpu_sum=0 cpu_max=0 n=0
for ((i = 0; i < seconds; i++)); do
  read -r rss cpu < <(ps -o rss=,%cpu= -p "$pid") || { echo "hived exited; log:" >&2; cat "$tmp/hived.log" >&2; exit 1; }
  rss_sum=$((rss_sum + rss)); ((rss > rss_max)) && rss_max=$rss
  cpu_sum=$(awk -v a="$cpu_sum" -v b="$cpu" 'BEGIN { print a + b }')
  cpu_max=$(awk -v a="$cpu_max" -v b="$cpu" 'BEGIN { print (b > a) ? b : a }')
  n=$((n + 1))
  sleep 1
done

label="${ref:-working tree}"
awk -v l="$label" -v n="$n" -v rs="$rss_sum" -v rm="$rss_max" -v cs="$cpu_sum" -v cm="$cpu_max" \
  'BEGIN { printf "%s: %d samples  rss mean %.0f KiB max %d KiB  cpu mean %.2f%% max %.2f%%\n", l, n, rs / n, rm, cs / n, cm }'
