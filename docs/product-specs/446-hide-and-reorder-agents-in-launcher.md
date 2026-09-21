---
issue: 446
title: "Let users hide agents from the launcher and reorder them"
type: enhancement
complexity: M
priority: P2
pr: 448
stage: REVIEW
---

# Let users hide agents from the launcher and reorder them

- **Issue:** #446
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/active/446-hide-and-reorder-agents-in-launcher.md](../exec-plans/active/446-hide-and-reorder-agents-in-launcher.md)

## Problem

The new-session launcher lists every built-in and custom agent, ordered by launch count (`hive.agentUsage` in localStorage, ties broken by the agent package's display order). A user who only uses two or three agents still scrolls past the rest every time, including agents that are not even installed. There is also no way to put a preferred agent in a fixed spot: the order is whatever the launch counts say.

## Desired behavior

Settings → Agents shows a checklist of every agent (built-in and custom). Unchecking one hides it from the launcher; existing sessions of that agent keep running, rendering, restarting and duplicating normally. The user can also reorder agents without giving up the automatic usage-based ordering.

## Success criteria

- Settings → Agents lists every agent with a checkbox; unchecking an agent and saving removes it from the launcher list.
- A hidden agent's existing sessions still render with their agent colour/badge, and restart/duplicate still work.
- The user can reorder agents from Settings, and the launcher respects that order while still applying usage-based ordering (exact rule in the exec plan).
- Preferences survive a GUI restart.

## Non-goals

- Changing which agents the daemon knows about or can spawn.
- Syncing preferences across machines.

## Notes

Launcher ordering: `cmd/hivegui/frontend/src/components/modals/Launcher.tsx` (usage sort), `app/modals/launcher.ts` (`loadAgentUsage` / `bumpAgentUsage`).
