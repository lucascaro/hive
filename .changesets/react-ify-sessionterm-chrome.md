---
issue: null
pr: 334
type: changed
bump: patch
---
- The terminal tile's chrome — its header, the dead-session card and the
  loading panel a starting session shows — now renders from the same React
  tree as the rest of the app, and the frontend's last imperative DOM
  primitives are gone with it. The terminal itself is untouched: hosts are
  still reparented rather than recreated, so scrollback, WebGL slots and PTY
  attachments survive every repaint exactly as before. No visible change; the
  markup, classes and keyboard behaviour are identical.
