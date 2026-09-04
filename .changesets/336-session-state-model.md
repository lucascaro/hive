---
issue: null
pr: null
type: added
bump: minor
---
- Every session now shows what it is doing. The sidebar row and the grid
  tile header carry a state glyph — working, idle, waiting for you,
  exited — so a screen of ten agents can be read at a glance instead of
  clicked through one at a time. Hovering a row names the state in
  words. This first pass derives the state from the terminal itself, so
  it works for every agent and for plain shells; agents that can report
  their own state, including telling a permission prompt apart from an
  ordinary wait, follow in a later release.
