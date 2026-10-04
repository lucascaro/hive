---
type: added
bump: minor
issue: 496
pr: 501
---

ACP sessions now run in a permission mode you choose per agent in **Settings → Agents** — Claude starts in Manual and Codex in Read-only unless you allow more — and Hive switches an agent back if it raises its own mode without you choosing it on a permission card. Pi runs as an ACP session only once you allow unattended tool use for it there, since Pi never asks before using a tool. Gemini and Copilot stay off as ACP until Hive can cap their modes. An ACP session can also return a typed result through a `submit_result` tool, which plugins read from the session's transcript; it is the only tool request Hive answers on its own.
