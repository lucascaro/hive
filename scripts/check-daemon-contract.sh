#!/usr/bin/env bash
# check-daemon-contract.sh — fail when daemon-side behaviour changes
# without bumping buildinfo.DaemonContract.
#
# The GUI reads that constant to choose between relaunching itself
# (every session survives) and restarting hived (they all die). A
# missed bump silently reloads a GUI into a daemon it does not
# understand, which nothing downstream catches; a needless bump costs
# every user their running agents. Neither failure is visible in a
# diff, which is why this exists.
#
# Usage:
#   scripts/check-daemon-contract.sh <base-ref> <head-ref>
#
# Exit codes: 0 ok · 1 bump missing · 2 usage error.
#
# Bypass in CI with the `daemon-contract-override` label (handled by
# the workflow, not here) for refactors and test-only changes. The
# label is a claim you are making, not a formality — see
# docs/design-docs/daemon-contract.md.
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <base-ref> <head-ref>" >&2
  exit 2
fi
base="$1"
head="$2"

contract_file="internal/buildinfo/contract.go"

# Trees whose contents a client can observe: the protocol, the
# connection state machine, session semantics, and the state the
# registry hands back. cmd/hived is the binary that assembles them.
watched=(
  "internal/wire"
  "internal/daemon"
  "internal/session"
  "internal/registry"
  "cmd/hived"
)

# Test files are excluded: they change constantly and cannot change
# what a client observes. So are .md files in those trees.
changed="$(git diff --name-only "$base...$head" -- "${watched[@]}" \
  | grep -v '_test\.go$' \
  | grep -v '\.md$' \
  || true)"

if [[ -z "$changed" ]]; then
  echo "OK: no daemon-side changes."
  exit 0
fi

# Compare the constant's VALUE, not whether the file was touched: a
# comment edit in contract.go must not satisfy the gate.
# `|| true` because the file legitimately does not exist on a base ref
# from before the contract was introduced — and under `set -o pipefail`
# git's non-zero exit would otherwise kill the script instead of
# yielding the empty "unset" value the caller handles.
value_at() {
  { git show "$1:$contract_file" 2>/dev/null \
      | sed -n 's/^const DaemonContract = \([0-9][0-9]*\).*/\1/p' \
      | head -1; } || true
}

before="$(value_at "$base")"
after="$(value_at "$head")"

if [[ -z "$after" ]]; then
  echo "error: could not read DaemonContract from $contract_file at $head." >&2
  echo "  Expected a line of the form: const DaemonContract = <n>" >&2
  exit 1
fi

if [[ "$before" == "$after" ]]; then
  echo "error: this PR changes daemon-side code but DaemonContract is still $after." >&2
  echo >&2
  echo "Changed files:" >&2
  echo "$changed" | sed 's/^/  /' >&2
  echo >&2
  echo "If a GUI built from this tree could NOT correctly drive a daemon" >&2
  echo "built without it, bump DaemonContract in $contract_file and add a" >&2
  echo "line to the history comment above it." >&2
  echo >&2
  echo "If the change is invisible to clients (a refactor, a log line, a" >&2
  echo "comment), apply the 'daemon-contract-override' label instead — a" >&2
  echo "needless bump costs every user their running sessions." >&2
  echo >&2
  echo "Background: docs/design-docs/daemon-contract.md" >&2
  exit 1
fi

echo "OK: DaemonContract ${before:-unset} -> $after alongside daemon-side changes."
