---
type: added
bump: minor
issue: 496
pr: 499
---

Plugins can now start Claude or Codex — or Pi, where the user allows it unattended tool use — as an ACP session: the agent runs over the Agent Client Protocol instead of in a terminal. A plugin can read the session's transcript, send it prompts, and answer its permission requests, and the session reports exact working, idle and waiting-for-permission states. Sessions a plugin creates record which plugin created them.
