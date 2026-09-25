---
issue: 457
pr: 459
type: added
bump: minor
---
- **Review agent plans in Hive before they run.** Turn on "Review agent plans in Hive" under Settings → Agents, and Claude leaving plan mode, or Pi calling the `hive_submit_plan` tool Hive gives it, waits for you. Hive shows the plan as formatted markdown. You select passages to comment on, then approve or request changes, and the agent gets each comment with the passage it is about. Esc keeps the agent waiting, and a bar brings the review back. With no Hive window open, the agent falls back to its own approval prompt.
- If plannotator or another plan reviewer is installed, it keeps reviewing Claude's plans unless you choose Hive, which disables that plugin for the Claude sessions Hive starts.
