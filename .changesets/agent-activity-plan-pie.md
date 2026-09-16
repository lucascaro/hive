---
type: added
bump: minor
pr: 417
---

Sessions running Claude now show how far the agent is through its own plan.
A small pie under the session's state dot fills as the agent ticks off its
todo list, taking its colour from the session state and fading to grey when
the agent stops reporting. Hive already received the plan and every tool call
on each hook and threw them away; it now keeps the last 200 tool calls per
session in memory, ready for the activity panel and grid to come.

Tool arguments never leave the machine that ran them: the hook derives a short
label — a file's basename, a command's first word, a URL's host — and only
that is sent, so a command carrying a token or password cannot reach the
daemon, another window, or the menu bar.
