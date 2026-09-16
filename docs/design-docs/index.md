# Design docs

Design documents — the *how it should work* layer between product specs and code. Each entry records a non-obvious architectural decision, the constraints that drove it, and the alternatives considered.

Index entries are short. Detailed rationale belongs in the per-doc files.

## Active

<!-- One row per design doc. Add: `- [Title](slug.md) — one-line description` -->
- [UI design system](ui/README.md) — tokens, themes, icons, components, patterns for the GUI; decisions + mocks
- [The control plane](control-plane.md) — daemon-owned session state across agent CLIs: the three knowledge tiers, the `event` wire mode, what is deliberately out of scope
- [Agent orchestration](agent-orchestration.md) — the per-session grant that lets an agent message, watch and spawn siblings; phased and evidence-gated
- [Agent activity](agent-activity.md) — what an agent is doing *inside* a turn: plan and tool events from the hook/extension tiers, the source-side privacy rule, one renderer in three placements

## Core beliefs

See [core-beliefs.md](core-beliefs.md) for project-wide design principles that span individual docs.

## How agents use this directory

- A design doc is the right place to record any decision a future agent run would otherwise need to re-derive.
- Cross-link from exec plans (`docs/exec-plans/`) when a plan implements or depends on a decision recorded here.
- `doc-garden` watches this directory for staleness against the code it describes.
