---
title: Let a plugin ask for a refused power through its manifest
type: enhancement
complexity: M
priority: P3
stage: TRIAGE
---

# Let a plugin ask for a refused power through its manifest

## Problem

A plugin's own socket is refused the decisions that belong to the user:
typing into a session (attach `DATA`/`RESIZE`, `SEND_TO_SESSION`),
answering an agent's permission request (`ANSWER_PERMISSION`), deciding a
plan review (`RESOLVE_PLAN_REVIEW`), and installing, enabling or removing
plugins. No bundled plugin needs any of them today. A legitimate plugin may
one day need one, such as a chat bridge that types the user's reply into a
session, and today the only answer is "no". The protocol has no way for the
user to say "yes, this plugin may".

## Desired behavior

- A plugin's manifest declares the refused capabilities it needs, each by a
  fixed name, such as `session.input` or `permission.answer`.
- The app's trust prompt lists each declared capability in plain words, next
  to the command the plugin will run. Enabling the plugin grants exactly
  those.
- `hived` serves a refused verb to a plugin only if the user granted the
  matching capability. Everything else is still refused with
  `mode_not_allowed`.
- A plugin whose manifest asks for a new capability after an update is asked
  about again before it runs.
- Settings → Plugins shows each plugin's granted capabilities, and the user
  can revoke one.

## Success criteria

- A plugin with no declared capabilities behaves exactly as today: every
  refused verb gets `mode_not_allowed`.
- A plugin granted `session.input` can type into a session over attach. Its
  other refused verbs are still refused.
- A manifest naming an unknown capability is refused at install with a clear
  error.
- A grant cannot be made from a plugin's own socket. Granting is part of
  enabling, which a plugin is refused.
- An update that adds a capability leaves the plugin disabled until the user
  confirms the new trust prompt.
- docs/plugins.md lists every capability, the verbs it unlocks, and what it
  puts at risk.

## Non-goals

- A sandbox. A plugin still runs as the user, so this narrows what its socket
  does, not what its process can do.
- Granting plugin management (install, enable, remove) to a plugin. That
  would let a plugin grant itself more.
- Per-session or per-project scoping of a grant. Add it only if a real plugin
  needs it.

## Notes

Only needed if and when a plugin needs a refused power. Until then the
refusals stand, and this spec stays in TRIAGE.

Context: the plugin-limits change ("Refuse terminal input, approvals and
plugin management to plugins"), docs/plugins.md "What a plugin cannot do",
and the confused-deputy discussion in
[agent-orchestration.md](../design-docs/agent-orchestration.md).
