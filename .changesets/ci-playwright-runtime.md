---
issue: null
pr: null
type: changed
bump: patch
---
- CI finishes in about half the time. Three things were paying for it:
  the Windows leg spent three minutes on every run enabling a Windows
  media codec feature that no test uses, the Playwright suite ran on
  half the available CPUs, and two pushes to the same pull request both
  ran to completion instead of the first being cancelled. Developer-facing
  only — nothing about the app itself changes.
