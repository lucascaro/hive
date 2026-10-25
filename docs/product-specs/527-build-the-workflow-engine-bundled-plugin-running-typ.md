---
issue: 527
title: "Build the workflow engine: bundled plugin running typed TS workflows over ACP sessions"
type: enhancement
complexity: L
priority: P1
stage: RESEARCH
---

# Build the workflow engine: bundled plugin running typed TS workflows over ACP sessions

- **Issue:** #527
- **Type:** enhancement
- **Complexity:** L
- **Priority:** P1
- **Design:** [docs/design-docs/workflow-engine.md](../design-docs/workflow-engine.md)

## Problem

Users who run multi-agent work (implement, review, fix; fan out and verify; plan, implement, verify) do it by hand today. They click between sessions, paste outputs, can't see where each agent is in the overall flow, and lose the run on a crash or restart. Spec 495 designed the engine and gave a go/no-go of **go, custom**, conditional on gate B (#503, passed) and #505 (fixed). Nothing is built yet. After review the work was split into three specs.

## Desired behavior

A user drops a typed TypeScript workflow file in `<project>/.hive/workflows/`, reviews its graph, and runs it. Agent nodes run as `acp` sessions owned by `hived`. The run survives a plugin crash, a `hived` restart and a reboot, and the GUI overlays live and replayed state on the reviewed graph.

## Success criteria

This is the umbrella. It is done when all three parts are done:

1. [#528](528-workflow-engine-a-daemon-and-sdk-prerequisites-prov.md): daemon and SDK prerequisites. Independently useful to plain ACP sessions.
2. [#529](529-workflow-engine-b-headless-engine-core-validator-in.md): headless engine core, depends on #528.
3. [#530](530-workflow-engine-c-gui-workflow-list-trust-dialog-ru.md): GUI, depends on #528 and #529.
4. README, `site/features.json` and a changeset land with the part that makes the feature user-visible (#530).

## Non-goals

- An `llm` node or any direct model API call; Hive holds no API keys.
- A durability layer that needs an always-on service.
- Resuming an agent session mid-turn.
- Installable workflow packages and a workflow editor (492 follow-ups 3 and 4).
- Cross-project, remote and A2A workflows.
- Hivesmith as baseline, host or evidence.

## Notes

- Preconditions: gate B (#503) passed; #505 fixed in #510.
- Gates C and D in the design decide later whether monitoring and authoring get built; they don't gate this work.
