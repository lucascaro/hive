# PLANS.md

How planning works in this repo.

- **Lightweight ephemeral plans** — for small, single-PR changes. Live in the conversation; do not check in.
- **Execution plans** — for any work that touches multiple files, takes more than one PR, or carries non-obvious decisions. Live under `docs/exec-plans/active/` and move to `docs/exec-plans/completed/` on merge.

## Exec plan rules

- One exec plan per feature. Filename is `<NNN>-<slug>.md` matching the spec.
- The plan is the system of record for *how* the work was done. Decisions and progress are append-only.
- A plan must cite the spec it implements (`docs/product-specs/<slug>.md`).
- A plan must list the files it expects to change, the tests it expects to add, and the alternatives it ruled out.
- When in doubt, write it down. Future agent runs cannot recover what was only said in chat.

## Plan lifecycle

1. **RESEARCH** — `/feature-research` populates the Research section.
2. **PLAN** — `/feature-plan` populates the Approach, Files, and Tests sections.
3. **IMPLEMENT** — `/feature-implement` appends to Decision log and Progress as code lands.
4. **DONE** — on merge, the file moves to `docs/exec-plans/completed/` and Status flips to `completed`.

See [docs/exec-plans/_template.md](docs/exec-plans/_template.md) for the file shape.
