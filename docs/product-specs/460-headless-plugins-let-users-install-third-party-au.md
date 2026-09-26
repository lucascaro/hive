---
issue: 460
title: "Headless plugins: let users install third-party automations that react to and drive Hive sessions"
type: enhancement
complexity: L
priority: P2
pr: 463
stage: IMPLEMENT
---

# Headless plugins: let users install third-party automations that react to and drive Hive sessions

- **Issue:** #460
- **Exec plan:** [docs/exec-plans/active/460-headless-plugins-let-users-install-third-party-au.md](../exec-plans/active/460-headless-plugins-let-users-install-third-party-au.md)

## Problem

Users ask for niche features, such as a webhook when an agent is waiting, auto-restarting on exit, or bridging session messages to chat. They are too specialised for Hive core, so they get declined or sit in the backlog. The only alternatives today are forking Hive or writing a standalone client against an undocumented, internal wire protocol. So these features don't get built at all, and every one of them lands on a single maintainer's review queue.

## Desired behavior

A user can install a plugin from a local folder or a git URL. Hive shows a clear warning first that the plugin runs with the user's full privileges. Once enabled, the plugin receives session events (state changes, attention, exit, session messages) and can act on them: send input, send session messages, create or kill sessions. Settings has a Plugins tab that lists installed plugins and lets the user enable, disable or remove each one. A plugin that crashes, hangs or floods events is contained: it gets disabled or restarted, and running sessions and the daemon are unaffected. The plugin API is documented, versioned as experimental (0.x) until 1.0 and then follows semver. It exposes the same capabilities an out-of-process wire client has. The repo ships a reference plugin as a working example.

## Success criteria

1. The repo contains a reference plugin (e.g. a webhook on `waiting_input`). It installs from a local folder and fires end to end against a real `hived` in an isolated e2e run.
2. A plugin installed from a git URL loads and runs, just as one installed from a local folder does.
3. Installing a plugin shows a warning that it runs with full user privileges, and nothing runs until the user confirms.
4. Settings has a Plugins tab that lists installed plugins with enabled/disabled state. Enable, disable and remove each take effect without restarting the daemon.
5. A test plugin that panics or crashes, one that never returns, and one that floods events each leave every running session alive and responsive, and the daemon keeps serving other clients.
6. With zero plugins installed, idle daemon CPU and memory stay within noise of the pre-change baseline (measured and recorded in the PR).
7. The plugin API can subscribe to every event and invoke every control operation that an out-of-process wire client (like `hivebar`) can. A test or table in the docs proves this parity.
8. Published plugin-author docs cover the manifest, lifecycle, events, actions, the trust model and the 0.x stability promise. The reference plugin's README is written against those docs only.

## Non-goals

- GUI extension points (panels, palette commands, sidebar items). That is a separate follow-up spec.
- Cosmetic plugins (theme packs, keybinding packs). That is a separate follow-up spec.
- Sandboxing, declared permissions or capability enforcement. Plugins are fully trusted.
- A marketplace, registry, curated index, ratings or auto-update for plugins.
- Accounts, identity, relays or any networking provided by Hive. "Chat with other users" is something a plugin could build on its own network access; core does not support it.
- A stability guarantee before plugin API 1.0.
- Moving existing core features out into plugins.

## Notes

- Spec 1 of 3 in a plugin decomposition: headless runtime first, then GUI surfaces, then cosmetic. The later ones get their own `/hs-brainstorm`.
- Open: behaviour when a plugin targets an incompatible API version (refuse to load vs. warn). Decide in plan.
- Open: whether git-URL installs pin a commit or track a branch.
- **Phases.** Phase 1 of 2 is gated on criteria 1, 2, 5, 6, 7, 8 and the daemon-side half of 3 (installs land disabled; nothing runs until enabled). Phase 2 of 2 is gated on criteria 3 (GUI trust confirm) and 4 (Settings tab).
- Depends on #461 (a stuck wire client can stall a session or hang shutdown); criterion 5's hang case needs it.
- Prior art in-repo: custom agents (spec 240), `hivebar` as a pure wire client, the hook/extension tiers in `docs/design-docs/control-plane.md`.
