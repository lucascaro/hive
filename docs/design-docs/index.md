# Design docs

Design documents — the *how it should work* layer between product specs and code. Each entry records a non-obvious architectural decision, the constraints that drove it, and the alternatives considered.

Index entries are short. Detailed rationale belongs in the per-doc files.

## Active

<!-- One row per design doc. Add: `- [Title](slug.md) — one-line description` -->

## Core beliefs

See [core-beliefs.md](core-beliefs.md) for project-wide design principles that span individual docs.

## How agents use this directory

- A design doc is the right place to record any decision a future agent run would otherwise need to re-derive.
- Cross-link from exec plans (`docs/exec-plans/`) when a plan implements or depends on a decision recorded here.
- `doc-garden` watches this directory for staleness against the code it describes.
