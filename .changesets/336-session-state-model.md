---
issue: null
pr: null
type: added
bump: minor
---
- Shell sessions now show what they are doing. The sidebar row and the
  grid tile header carry a state glyph — working, waiting for you,
  exited — so a screen of sessions can be read at a glance instead of
  clicked through one at a time. Hovering names the state in words.
  Agent sessions are unchanged for now: the state is inferred from the
  terminal, which is only honest for a program that goes quiet when it
  finishes, and agents redraw continuously. They report their own state
  in a later release.
