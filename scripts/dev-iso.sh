#!/usr/bin/env bash
# dev-iso.sh — Build and launch an isolated dev Hive that doesn't
# touch your prod daemon, sessions, or registry. Uses the
# HIVE_SOCKET / HIVE_STATE_DIR env-var overrides.
#
# Usage:
#   scripts/dev-iso.sh           # build + run
#   scripts/dev-iso.sh --reset   # also wipe the iso state dir first
#   scripts/dev-iso.sh --no-build  # skip ./build.sh, just relaunch
#   scripts/dev-iso.sh --dir /tmp/foo  # use a custom iso dir
#
# Run the binary directly (not via `open`) so the env vars survive —
# launchctl strips most env from .app bundles opened with `open`.
set -euo pipefail

cd "$(dirname "$0")/.."

iso_dir=/tmp/hive-iso
do_build=1
do_reset=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dir)      iso_dir="$2"; shift 2 ;;
    --reset)    do_reset=1; shift ;;
    --no-build) do_build=0; shift ;;
    -h|--help)
      sed -n '2,12p' "$0" | sed 's/^# \{0,1\}//'
      exit 0 ;;
    *) echo "unknown flag: $1" >&2; exit 2 ;;
  esac
done

if [[ $do_reset -eq 1 ]]; then
  echo "==> Wiping $iso_dir"
  rm -rf "$iso_dir"
fi
# 0700 because $iso_dir defaults to /tmp/hive-iso — keep the socket and
# registry out of reach of other local users on shared machines.
(umask 077 && mkdir -p "$iso_dir/state")

if [[ $do_build -eq 1 ]]; then
  ./build.sh
fi

app="cmd/hivegui/build/bin/hivegui.app/Contents/MacOS/hivegui"
if [[ ! -x "$app" ]]; then
  echo "error: $app not found — run ./build.sh first or omit --no-build" >&2
  exit 1
fi

echo "==> Launching isolated Hive"
echo "    HIVE_SOCKET=$iso_dir/hived.sock"
echo "    HIVE_STATE_DIR=$iso_dir/state"
HIVE_SOCKET="$iso_dir/hived.sock" \
HIVE_STATE_DIR="$iso_dir/state" \
  "$app"
