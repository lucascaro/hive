---
issue: null
pr: null
type: fixed
bump: patch
---
- Clicking a link in a terminal now only opens http, https and mailto
  URLs. A program could previously print a link labelled with one thing
  that pointed at a local file or another app's URL scheme, and a click
  would launch it.
