# QUALITY_SCORE.md

Per-domain and per-layer quality grades. A snapshot of where the codebase is healthy and where it has known gaps.

Update this file when:

- A `gc-sweep` run lowers a grade because deviations have accumulated.
- A targeted refactor raises a grade.
- A new domain or layer is introduced.

## Grading scale

- **A** — fully consistent with the architecture rules; high test coverage; well-documented.
- **B** — minor inconsistencies; tests cover the happy path.
- **C** — known shortcuts in place; tests partial.
- **D** — actively rotting; refactor planned.
- **F** — unsafe to extend without rework.

## Domains

| Domain | Grade | Notes |
|--------|-------|-------|
| <name> | <A–F> | <one line — link to the gc-sweep run or refactor plan that justifies it> |

## Layers

| Layer | Grade | Notes |
|-------|-------|-------|
| Types | <A–F> | |
| Config | <A–F> | |
| Repo | <A–F> | |
| Service | <A–F> | |
| Runtime | <A–F> | |
| UI | <A–F> | |
