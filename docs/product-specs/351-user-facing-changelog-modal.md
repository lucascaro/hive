---
issue: 351
pr: 357
title: "Show a user-facing changelog behind a gift icon in the sidebar"
type: enhancement
complexity: M
priority: P2
stage: GATE
---

# Show a user-facing changelog behind a gift icon in the sidebar

- **Issue:** #351
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P2

## Problem

Hive ships user-visible changes every release, but the only place a user can read what changed is `CHANGELOG.md` in the repo — invisible from inside the app. New features land and nobody notices.

## Desired behavior

A gift icon sits to the right in the sidebar header, alongside "New project" and "Check for updates". Clicking it opens a modal showing a user-facing rendering of the changelog: what's new, per version, in the prose `CHANGELOG.md` already uses.

## Success criteria

- A gift icon button sits in the sidebar header, to the right of "Check for updates", at the same 22px size as its siblings, with an accessible name and a keyboard-reachable focus.
- Clicking it opens a modal listing what has shipped, grouped by version, newest version first, each entry showing the feature's name and its one-line description.
- The modal also shows what is coming next, drawn from the same list's `planned` entries.
- The list the modal renders is the same one the website renders — one file, no second copy to keep in sync.
- The gift carries an unread marker when the bundled list contains a version the user has not opened the modal on; opening it clears the marker, and the marker stays cleared across restarts.
- The marker clears whichever way the modal is opened — the gift or the command palette — and clears without a reload.
- A user who has never opened the modal sees the marker, so the release that introduces it is discoverable.
- The modal closes on Escape, on the close button, and on a backdrop click, like every other Hive modal.
- The website build still produces the landing page's shipped/planned sections from that same list, and fails loudly if a shipped entry is missing its version stamp.

## Non-goals

- Rendering the engineering `CHANGELOG.md` in the app. It stays the website's `changelog.html` and the repo's generated file; the in-app list is the curated user-facing one.
- Any change to the changeset schema or how `.changesets/*.md` are rolled up into the changelog. Both are unchanged.
- **Not a non-goal, though the approved plan said it was:** `scripts/regen-generated.py` and `scripts/release.sh` are both touched. Review forced this in two steps. First, the AGENTS.md rule the plan introduces was unfollowable — a `since` must name a released version and a new feature has none — so `--release <version>` now also stamps `since: "Unreleased"` entries in the feature list, exactly as it promotes the changelog's `[Unreleased]`. Then that stamp turned out never to be committed: `release.sh` staged a hand-written file list that did not include it, which would have dropped every released feature from the website and aborted the *next* release on its clean-tree guard. `release.sh` now asks `regen-generated.py --list-targets` what to stage instead of keeping its own copy.
- Fetching release notes over the network. The list is bundled with the build and works offline.
- A CI check that forces a feature-list entry per user-visible release. Named as follow-up in the exec plan, not built here.
- Any daemon, wire-protocol or persistence change. The only state is one `localStorage` key.
