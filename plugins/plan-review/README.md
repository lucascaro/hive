# Plan review

Review an agent's plan in Hive before it runs. When Claude leaves plan
mode, or Pi calls the `hive_submit_plan` tool Hive gives it, the agent
waits while you read the plan, comment on passages, and approve it or
ask for changes. Your comments reach the agent together with the text
they refer to.

This plugin ships inside Hive. It is installed on every machine and
starts **disabled**: enable it in **Settings → Plugins** to turn plan
review on. It cannot be removed, only disabled.

It is written only against [docs/plugins.md](../../docs/plugins.md), so
it uses nothing a third-party plugin could not. The agent side (Claude's
blocking `ExitPlanMode` hook, Pi's tool, detecting other reviewers, and
the fallback when no Hive window is open) stays in Hive itself.

## Use

| Action | How |
|---|---|
| Review a plan | It opens by itself when an agent submits one |
| Decide later | `[esc]`. The agent keeps waiting; nothing is answered |
| Come back to a deferred plan | **Review plan…** on the bar above the terminal, or the command palette → *Review pending plan* |
| Comment on a passage | Select text in the plan, then **Comment on selection** |
| Answer | **Approve**, or **Request changes** once you have left a comment or feedback |

A session waiting on a review shows a **Review** badge in the sidebar.

With no Hive window running this plugin, a plan does not wait: the
agent falls back to its own approval prompt in the terminal.

## Settings

**When another plan reviewer is installed.** If you also use a tool
such as plannotator, the default leaves Claude's plans to that tool.
Choosing Hive switches a Claude Code plugin reviewer off for each Claude
session Hive starts, so newly started sessions are reviewed here. A
reviewer hook written directly in a Claude `settings.json` cannot be
switched off; with Hive chosen, the section names that file.
