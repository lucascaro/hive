# Tech-debt entries

Per-item contribution files that drive `docs/exec-plans/tech-debt-tracker.md` generation. Each tracked debt is one file here. A GitHub Action regenerates the tracker on every push to `main` from these sources.

This directory exists so multiple parallel PRs (especially `gc-sweep` outputs) do not conflict on the tracker.

## File naming

```
.tech-debt/<slug>.md
```

`<slug>` — short kebab-case identifier. Use the exec-plan slug or PR number when relevant.

## Schema

```markdown
---
item: One-line description of the debt
surfaced_in: <exec-plan slug or PR #>
severity: low | med | high
owner: <name or unowned>
notes: <link or context>
---
```

Required: `item`, `severity`. Optional: `surfaced_in`, `owner`, `notes`.

## How to add one

When an exec plan accepts a known shortcut:

1. Create `.tech-debt/<slug>.md` with the schema above.
2. Link back to it from the exec plan's Decision log.
3. Open the PR.

You **never** edit `docs/exec-plans/tech-debt-tracker.md` directly — `block-generated-edits` will fail the PR if you do.

## Resolving debt

Delete the corresponding `.tech-debt/<slug>.md` in the same PR that pays the debt. The commit message is the audit trail.
