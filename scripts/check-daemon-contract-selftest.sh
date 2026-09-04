#!/usr/bin/env bash
# Self-test for check-daemon-contract.sh, run in CI beside the gate
# itself.
#
# The gate is the only thing standing between a forgotten contract bump
# and a GUI silently reloading into a daemon it does not understand, so
# it needs its own evidence that it still fires. It builds a throwaway
# git repo rather than using fixture files: the gate reads history via
# `git diff` and `git show`, so a fixture directory could not exercise
# it at all.
set -euo pipefail
cd "$(dirname "$0")/.."
GATE="$PWD/scripts/check-daemon-contract.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

git -C "$tmp" init -q
git -C "$tmp" config user.email t@example.com
git -C "$tmp" config user.name test
mkdir -p "$tmp/internal/buildinfo" "$tmp/internal/daemon" "$tmp/docs"

write_contract() { printf 'package buildinfo\n\nconst DaemonContract = %s\n' "$1" > "$tmp/internal/buildinfo/contract.go"; }
commit() { git -C "$tmp" add -A; git -C "$tmp" commit -qm "$1"; }

write_contract 1
echo "package daemon" > "$tmp/internal/daemon/daemon.go"
commit base
base="$(git -C "$tmp" rev-parse HEAD)"

expect() { # expect <want-exit> <description>
  local want="$1" desc="$2" got=0
  ( cd "$tmp" && "$GATE" "$base" HEAD >/dev/null 2>&1 ) || got=$?
  if [[ "$got" != "$want" ]]; then
    echo "FAIL: $desc (exit $got, want $want)" >&2
    exit 1
  fi
  echo "ok: $desc"
}

reset_to_base() { git -C "$tmp" reset -q --hard "$base"; }

# 1. Daemon-side change, no bump -> fail. The case the gate exists for.
echo "// changed" >> "$tmp/internal/daemon/daemon.go"
commit "daemon change, no bump"
expect 1 "daemon-side change without a bump is refused"

# 2. Same change with a bump -> pass.
reset_to_base
echo "// changed" >> "$tmp/internal/daemon/daemon.go"
write_contract 2
commit "daemon change with bump"
expect 0 "daemon-side change with a bump is accepted"

# 3. A comment edit in contract.go must NOT satisfy the gate: it is the
#    value that matters, not whether the file was touched.
reset_to_base
echo "// changed" >> "$tmp/internal/daemon/daemon.go"
printf 'package buildinfo\n\n// a new comment\nconst DaemonContract = 1\n' \
  > "$tmp/internal/buildinfo/contract.go"
commit "daemon change, contract.go touched but value unchanged"
expect 1 "touching contract.go without changing the value is refused"

# 4. Test-only daemon-side changes are exempt — they change constantly
#    and cannot change what a client observes.
reset_to_base
echo "package daemon" > "$tmp/internal/daemon/daemon_test.go"
commit "test only"
expect 0 "a test-only daemon-side change needs no bump"

# 5. So are docs.
reset_to_base
echo "notes" > "$tmp/internal/daemon/NOTES.md"
commit "docs only"
expect 0 "a markdown-only daemon-side change needs no bump"

# 6. A change outside the watched trees is not the gate's business.
reset_to_base
echo "hello" > "$tmp/docs/thing.md"
commit "unrelated"
expect 0 "a change outside the daemon-side trees is ignored"

echo "check-daemon-contract self-test: all cases passed"
