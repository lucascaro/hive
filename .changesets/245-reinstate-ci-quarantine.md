---
issue: null
pr: null
type: fixed
bump: patch
---
- Re-instated the CI quarantine on the `e2e-real` test "viewport converges to
  the bottom after a mode switch", which PR #307 lifted on insufficient
  evidence. It failed CI macOS on both attempts with the same
  `resizeDecisions() === 0` symptom it was originally quarantined for. Test-only.
