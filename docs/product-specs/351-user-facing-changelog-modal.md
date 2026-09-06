---
issue: 351
title: "Show a user-facing changelog behind a gift icon in the sidebar"
type: enhancement
complexity: M
priority: P2
stage: IMPLEMENT
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
- A user who has never opened the modal sees the marker, so the release that introduces it is discoverable.
- The modal closes on Escape, on the close button, and on a backdrop click, like every other Hive modal.
- The website build still produces the landing page's shipped/planned sections from that same list, and fails loudly if a shipped entry is missing its version stamp.

## Non-goals

- Rendering the engineering `CHANGELOG.md` in the app. It stays the website's `changelog.html` and the repo's generated file; the in-app list is the curated user-facing one.
- Any change to how `.changesets/*.md` are written or rolled up. `scripts/regen-generated.py` and `scripts/release.sh` are untouched.
- Fetching release notes over the network. The list is bundled with the build and works offline.
- A CI check that forces a feature-list entry per user-visible release. Named as follow-up in the exec plan, not built here.
- Any daemon, wire-protocol or persistence change. The only state is one `localStorage` key.
