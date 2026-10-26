---
issue: 530
title: "Workflow engine C: GUI (workflow list, trust dialog, run start, human answers, graph and run overlay)"
type: enhancement
complexity: M
priority: P2
stage: PLAN
---

# Workflow engine C: GUI (workflow list, trust dialog, run start, human answers, graph and run overlay)

- **Issue:** #530
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P2
- **Parent:** [527](527-build-the-workflow-engine-bundled-plugin-running-typ.md)
- **Depends on:** [528](528-workflow-engine-a-daemon-and-sdk-prerequisites-prov.md), [529](529-workflow-engine-b-headless-engine-core-validator-in.md)

## Problem

With the engine running headless, users still can't find their workflows, trust one, start a run, answer a human node, or see where a run is.

## Desired behavior

The GUI lists a project's workflows, asks for trust, starts a run with typed input, shows the reviewed graph, overlays live and replayed run state on it, and lets the user answer human nodes.

## Success criteria

1. **Workflow list.** A project's workflows are listed on demand (no file watcher). With no usable Node, the panel shows the required version.
2. **Trust dialog.** Shows what trusting means, with `y/enter` and `esc/n` bindings visible, per the overlay rules.
3. **Run start.** Typed input is collected from the workflow's input schema.
4. **Graph and overlay.** The IR renders as a graph (Mermaid or equivalent; deciding which is part of the plan). Events keyed by node id overlay live and replayed runs. A run whose source changed since it started is marked. Loop and map copies appear under their one reviewed template (`<map>[<index>]/<node>`).
5. **Human nodes.** The user answers a typed question from the GUI.
6. **Permission waits.** A `node.waiting` with reason `permission` is visible and answerable.
7. **Keybindings and commands.** Each new command follows the Keybindings Policy: binding data, `commands.ts`, menu default, help row, README table.
8. **Tests.** Playwright mock-layer specs for each surface and an `e2e-real` run through `hived-ws-bridge`, which carries A's new frame. All new frames work under the lock-step wire rule.
9. **Docs.** Changeset, `site/features.json` entry (`since: "Unreleased"`), README.

## Non-goals

- A workflow editor (492 follow-up 4) and monitoring beyond the overlay (follow-up 3).
- Engine, daemon and wire work (A and B).
