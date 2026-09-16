---
type: added
bump: minor
---

Pi sessions now report their tool calls and plan the same way Claude sessions
do. The plan pie fills as Pi works through its plan, and hovering the pie
names the tool Pi is running.

Pi has no task list of its own, so Hive's Pi extension adds a `hive_todo` tool
to the Pi sessions Hive starts. It uses some of the model's context; turn it off
under Settings → Agents ("Show Pi's plan progress in the sidebar"). The change
applies to newly started sessions. A resumed Pi session shows its plan straight
away, and a new conversation no longer shows the previous one's.

As with Claude, a tool's arguments never leave the machine: the extension sends
only the tool name and a short label, such as a file name, a command name or a
host.
