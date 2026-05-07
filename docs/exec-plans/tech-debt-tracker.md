# Tech debt tracker

Known shortcuts, deferrals, and rough edges. One row per item. Keep entries short — link to the exec plan that introduced or will resolve the debt.

| Item | Surfaced in | Severity | Owner | Notes |
|------|-------------|----------|-------|-------|
| <one-line description> | <exec-plan slug or PR #> | low / med / high | <name or `unowned`> | <link or context> |

## Conventions

- Add a row whenever an exec plan accepts a known shortcut. Linking back from the plan is required.
- `gc-sweep` may add rows automatically when it detects a deviation it can't safely auto-refactor.
- Resolve a row by deleting it in the same PR that pays the debt; the commit message is the audit trail.
