---
type: added
bump: minor
issue: 496
pr: 500
---

New sessions can run as ACP instead of in a terminal: pick **ACP** in the new-session popup (Terminal stays the default) for Claude or Codex (or Pi, once you allow it unattended tool use in Settings → Agents), and the tile shows the conversation as a transcript (agent messages, plan steps and tool calls) with a prompt box. When the agent asks to use a tool, the request appears in the transcript and you allow or deny it in place. Agents that can't run as ACP on your machine are shown disabled with the reason, and adapters Hive hasn't tested yet are marked experimental.
