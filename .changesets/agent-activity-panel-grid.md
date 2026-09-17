---
type: added
bump: minor
pr: 422
---

See what a Claude or Pi session is doing without reading its scrollback. ⌘J
(Ctrl+Shift+J on Windows and Linux) opens an inspector beside the terminal: the
agent's plan, with the tool calls each step ran nested under it, and the full
timeline below. In a grid, the same key turns every tile into its session's plan
and live tool feed, and ⇧⌘J (Ctrl+Alt+Shift+J) gets there from any view. Grid
shortcuts keep working, and typing never reaches a hidden terminal. If an agent
stops reporting mid-turn, its activity greys out and says how long it has been
quiet; shells and other agents that report nothing say so instead.
