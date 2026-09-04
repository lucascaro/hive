# The daemon contract

Why `buildinfo.DaemonContract` is an integer, why it is not a version
string or a hash, and why `wire.PROTOCOL_VERSION` stayed at 1.

## The question it answers

`hived` is spawned detached and has no idle exit, so it outlives the GUI
that started it. That means picking up new GUI code never required
killing anyone's sessions — relaunching `hivegui` and leaving `hived`
alone was always possible.

What was missing was a trustworthy answer to: *can this GUI drive that
daemon?* Without one, "Restart Hive" had to assume the worst and replace
both halves, which ends every PTY and every running agent.

`DaemonContract` is that answer. It names the compatibility generation
of everything the daemon exposes to a client — wire frames, session
semantics, registry layout. Two builds with the same contract can be
mixed; two with different contracts cannot.

## Why not the build ID

This is what the code did before, and it is the bug the contract
replaces. `buildinfo.BuildID` is a git revision, so it changes for a
comment. Comparing it made the GUI declare a stale daemon after a CSS
tweak and demand a full restart — the user paid for every agent they had
running to learn the frontend had been rebuilt.

Build IDs still decide *whether anything changed*. They just no longer
decide what it costs.

## Why not a version string per binary

The obvious alternative was independent semver: `hivegui 0.6.0` against
`hived 0.4.2`, bumped separately at release time.

It was rejected on two counts. It needs real machinery — `release.sh`
inferring a bump per component from which paths a changeset touched, two
tags, two changelogs, a compatibility matrix. And after all that the
strings still do not answer the question: knowing the daemon is 0.4.2
tells you nothing about whether *this* GUI can drive it without
consulting a table somebody has to maintain by hand. The contract is
that table, collapsed to the one number the table would have produced.

## Why not a hash of the daemon binary

Tempting, because it needs no human judgement: hash the staged `hived`,
compare it to the running one, reload when they match.

It fails in the expensive direction. Any recompile changes the hash — a
Go toolchain bump, different `-ldflags`, a comment in a file `cmd/hived`
imports — and each one would force a full restart that kills every
session for a daemon that behaves identically. The same objection sinks
hashing the daemon-side *source*.

A number a human bumps is worse in one way (it can be forgotten) and
better in the way that matters (it tracks behavior, not bytes). The
forgetting is what `scripts/check-daemon-contract.sh` is for.

## Why PROTOCOL_VERSION stayed at 1

The contract work added two frames, and the reflex is to bump the
protocol version alongside them. That would have broken the feature.

`internal/daemon/daemon.go` refuses any HELLO whose `Version` differs
from its own and closes the connection. So a GUI that bumped
`PROTOCOL_VERSION` could not handshake with the running daemon at all —
and would therefore never receive the WELCOME carrying the contract it
needs to decide between a reload and a restart. The feature would be
blind exactly when it was needed.

Unknown control frames, by contrast, are logged and ignored
(`handleControlFrame`'s `default:`), so *adding* frames is backward
compatible. `PROTOCOL_VERSION` is reserved for a genuine break — a frame
whose meaning changed, or a field an old peer would misread. When that
day comes, bump both: the protocol version to refuse the connection, and
the contract so the GUI knows what to offer.

## What the two comparisons are

There are two, and they are not the same:

- **The banner** compares this GUI's contract to the running daemon's
  (from WELCOME). It answers "the build on disk changed — what do I
  offer the user?"
- **The updater** compares the *staged* daemon's contract (read by
  running `hived --version --json` out of the staged bundle) to the
  running daemon's. It answers "after the swap, can the new GUI drive
  the daemon that is already up?" — and the new GUI carries the staged
  contract, not this one's.

Using the GUI's own constant in the second place would be right only by
coincidence, whenever this GUI and the running daemon already agreed.

## Bumping it

Bump `buildinfo.DaemonContract` when a GUI built against the new tree
cannot correctly drive a daemon built against the old one. Do not bump
it for GUI-only changes, or for daemon-side changes no client can
observe.

`scripts/check-daemon-contract.sh` fails CI when a PR touches
`internal/{wire,daemon,session,registry}` or `cmd/hived` without changing
the value; the `daemon-contract-override` label bypasses it for the
common case of a refactor or a test-only change. The bypass is the
weak point — a wrongly-applied override lets a new GUI reload into a
daemon it does not understand, and nothing downstream catches it. Treat
the label as a claim you are making, not a formality.
