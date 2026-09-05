---
issue: null
pr: 345
type: changed
bump: patch
---
- CI is a little faster and its cancellation rules are now correct. The
  Windows leg spent three minutes of every run enabling a Windows media
  codec feature that no test uses, and two pushes to the same pull request
  both ran the full three-platform matrix instead of the first being
  superseded. Developer-facing only — nothing about the app itself changes.
