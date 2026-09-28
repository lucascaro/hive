---
type: added
bump: minor
issue: 471
pr: 473
---
Hive now ships its first bundled plugin, **Plan review**. It is listed under Settings → Plugins on every install, disabled until you turn it on, and it can be disabled but not removed. Plan review is on while the plugin is enabled, and its reviewer choice (let another installed reviewer such as plannotator handle Claude's plans, or review them in Hive) is in the plugin's own settings rather than on the Agents tab. A plan now waits for you only when a Hive window is actually running the plugin; otherwise the agent falls back to its own terminal prompt at once.
