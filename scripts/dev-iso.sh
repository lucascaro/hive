#!/usr/bin/env bash
# dev-iso.sh — Build and launch an isolated dev Hive that doesn't
# touch your prod daemon, sessions, or registry. Uses the
# HIVE_SOCKET / HIVE_STATE_DIR env-var overrides.
#
# Usage:
#   scripts/dev-iso.sh           # build + run
#   scripts/dev-iso.sh --reset   # stop the iso daemon and wipe its state
#   scripts/dev-iso.sh --no-build  # skip ./build.sh, just relaunch
#   scripts/dev-iso.sh --dir /tmp/foo  # use a custom iso dir
#
# The iso dir defaults to /tmp/hive-iso-<checkout>, one per worktree.
#
# Run the binary directly (not via `open`) so the env vars survive —
# launchctl strips most env from .app bundles opened with `open`.
set -euo pipefail

cd "$(dirname "$0")/.."

# Per-checkout by default. A single shared /tmp/hive-iso is what let a
# daemon from ANOTHER worktree be listening on this socket: every
# checkout raced for one path, and whichever hived bound it first
# served all of them — with its own binary, its own --cwd, and its own
# sessions. Keyed on the checkout dir (the worktree name), so parallel
# worktrees never collide.
# stop_iso_daemon terminates the hived owning $1/hived.sock, if any.
# --reset promises a clean slate, but wiping the state dir does nothing
# to a daemon already running on that socket: hived holds its registry
# in memory, so the GUI dials the survivor, adopts its sessions, and the
# reset you asked for silently did not happen. Worse, closing the window
# leaves that daemon and its agent PTYs alive for the next run to
# inherit — which is exactly how four orphaned agents outlived a test.
#
# hived writes <socket>.pid next to the socket for precisely this (see
# cmd/hived/main.go) and shuts down cleanly on SIGTERM.
stop_iso_daemon() {
  local dir="$1" sock pid=""
  sock="$dir/hived.sock"
  if [[ -r "$sock.pid" ]]; then
    pid=$(tr -dc '0-9' < "$sock.pid" || true)
  fi
  # No pidfile (removed, or never written) but something may still hold
  # the socket — ask the OS rather than giving up on the reset.
  if [[ -z "$pid" && -S "$sock" ]] && command -v lsof >/dev/null 2>&1; then
    pid=$(lsof -t "$sock" 2>/dev/null | head -1 || true)
  fi
  [[ -n "$pid" ]] || return 0
  # Guard against pid reuse: only ever signal a process whose command
  # line still names this socket.
  if ! ps -p "$pid" -o command= 2>/dev/null | grep -qF -- "$sock"; then
    return 0
  fi
  echo "==> Stopping hived on $sock (pid $pid)"
  kill -TERM "$pid" 2>/dev/null || return 0
  for _ in $(seq 1 50); do
    ps -p "$pid" >/dev/null 2>&1 || return 0
    sleep 0.1
  done
  echo "    hived $pid ignored SIGTERM after 5s; sending SIGKILL" >&2
  kill -KILL "$pid" 2>/dev/null || true
}

iso_slug=${PWD##*/}
iso_slug=${iso_slug//[^A-Za-z0-9._-]/-}
iso_dir=/tmp/hive-iso-${iso_slug:-default}
do_build=1
do_reset=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dir)      iso_dir="$2"; shift 2 ;;
    --reset)    do_reset=1; shift ;;
    --no-build) do_build=0; shift ;;
    -h|--help)
      # Every comment line after the shebang, up to the first line of
      # code — so editing the header can't silently truncate --help.
      awk 'NR>1 && /^#/ { sub(/^# ?/, ""); print; next } NR>1 { exit }' "$0"
      exit 0 ;;
    *) echo "unknown flag: $1" >&2; exit 2 ;;
  esac
done

if [[ $do_reset -eq 1 ]]; then
  # Guard against catastrophic --dir values. --reset must only ever
  # wipe a scratch dir under /tmp or /var/folders.
  # Reject any `..` in the raw input first — defeats prefix bypass
  # like `/tmp/../etc` even before canonicalization.
  case "/$iso_dir/" in
    */../*|*/..*|*../*)
      echo "refusing: --dir must not contain '..' segments ($iso_dir)" >&2
      exit 2 ;;
  esac
  # Canonicalize. macOS realpath has no -m, so fall back via the
  # parent dir if iso_dir doesn't exist yet.
  if [[ -e "$iso_dir" ]]; then
    abs_iso_dir=$(realpath "$iso_dir")
  else
    parent=$(dirname "$iso_dir")
    base=$(basename "$iso_dir")
    if [[ -d "$parent" ]]; then
      abs_iso_dir="$(cd "$parent" && pwd -P)/$base"
    else
      echo "refusing: parent of --dir does not exist: $parent" >&2
      exit 2
    fi
  fi
  # Strip trailing slashes for the comparison so `/tmp/` and `/tmp`
  # both reject as "the prefix itself, no subpath".
  abs_iso_dir="${abs_iso_dir%/}"
  case "$abs_iso_dir" in
    ""|/|/Users|/home|/tmp|/var|/var/folders|/private|/private/tmp|/private/var|/private/var/folders|"$HOME")
      echo "refusing to wipe $abs_iso_dir — must be a subpath under /tmp or /var/folders" >&2
      exit 2 ;;
  esac
  case "$abs_iso_dir" in
    /tmp/*|/var/folders/*|/private/tmp/*|/private/var/folders/*) ;;
    *)
      echo "refusing to wipe $abs_iso_dir — --reset only operates under /tmp or /var/folders" >&2
      exit 2 ;;
  esac
  stop_iso_daemon "$iso_dir"
  echo "==> Wiping $abs_iso_dir"
  rm -rf "$abs_iso_dir"
fi
mkdir -p "$iso_dir/state"

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
# `env -u ...`: launching from a terminal (unlike Finder/launchd) passes
# this shell's environment straight through to every session PTY.
#
#   dirstack — an exported `dirstack` makes zsh's `zmodload zsh/parameter`
#   fail, so every session opens with a wall of "Can't add module
#   parameter" errors.
#
#   CLAUDE_CODE_CHILD_SESSION / CLAUDECODE — set when this script is run
#   from inside a Claude Code session. An agent that inherits them starts
#   with "Transcript saving is off — inherited CLAUDE_CODE_CHILD_SESSION
#   marker": no transcript is written, so the conversation never appears
#   in /resume and Restart has nothing to resume by id. Silent, and it
#   looks exactly like a Hive bug.
env -u dirstack -u CLAUDE_CODE_CHILD_SESSION -u CLAUDECODE \
  HIVE_SOCKET="$iso_dir/hived.sock" \
  HIVE_STATE_DIR="$iso_dir/state" \
  "$app"
