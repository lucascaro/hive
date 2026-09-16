# Changesets

One file per user-visible change, named `<slug>.md`. `scripts/regen-generated.py`
rolls them into `CHANGELOG.md`'s `[Unreleased]` section on `main`, and
`scripts/release.sh` folds them into the version section and deletes them.
This README and `.gitkeep` are skipped by both.

```markdown
---
type: fixed
bump: patch
pr: 123
---

What changed and why it matters, in user-facing language.
```

Frontmatter is flat `key: value` scalars only.

| Key     | Required | Values |
|---------|----------|--------|
| `type`  | yes      | `added`, `changed`, `fixed`, `removed`, `deprecated`, `security` — the CHANGELOG subsection |
| `bump`  | yes      | `major`, `minor`, `patch`, `none` |
| `issue` | no       | GitHub issue number |
| `pr`    | no       | GitHub PR number |

The body must not be empty. The source of truth for these rules is
`validate_changeset` in `scripts/regen-generated.py`.
