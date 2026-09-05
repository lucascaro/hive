---
issue: 340
pr: 342
type: fixed
bump: patch
---
- Minimized and collapsed projects stay that way across a restart when
  you run more than one Hive daemon. Every GUI process shares one
  webview storage area, so instances attached to different state
  directories were overwriting each other's sidebar state — each launch
  pruned the other's project ids as "projects that no longer exist" and
  emptied the tray. The two keys (`hive.minimizedProjects`,
  `hive.collapsedProjects`) are now suffixed with an id derived from the
  daemon's state directory. Your existing sidebar state carries over to
  one instance; the others start from a clean slate once and keep their
  state from then on.
