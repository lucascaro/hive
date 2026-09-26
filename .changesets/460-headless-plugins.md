---
type: added
bump: minor
issue: 460
pr: 463
---

Headless plugins: Hive can now run and supervise plugins — small programs in any language that watch your sessions and act on them, such as the included webhook plugin that notifies a URL when an agent is waiting on you. Plugins install from a folder or a git URL, always install disabled, and run with your full user privileges. This release adds the plugin runtime and the author guide (docs/plugins.md); managing plugins from Settings arrives with the Plugins tab.
