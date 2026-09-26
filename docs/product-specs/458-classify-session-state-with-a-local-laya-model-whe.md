---
title: "Classify session state with a local Laya model when hooks can't tell"
type: enhancement
complexity: L
priority: P2
pr: 464
stage: REVIEW
---

# Classify session state with a local Laya model when hooks can't tell

- **Issue:** — (local-only, no GitHub issue)
- **Type:** enhancement
- **Complexity:** L
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/active/458-classify-session-state-with-a-local-laya-model-whe.md](../exec-plans/active/458-classify-session-state-with-a-local-laya-model-whe.md)

## Problem

Hive knows exactly what Claude and Pi are doing through hooks. Every other session (Aider, Codex, shell, custom agents) falls back to a PTY and bell heuristic. That heuristic rarely tells `working`, `waiting_input`, `waiting_permission` and `error` apart. Even Pi sometimes gets stuck in the wrong state when a hook doesn't fire. So several times a day the user has to click into tiles to see which agents are blocked. A missed "waiting" state is the costly failure, because an agent can sit blocked for an hour unnoticed.

## Desired behavior

The user can point Hive at a Laya decision-model server (Convai's open-weight 421M decision model) they run locally (Docker or Python, on macOS, Windows or Linux). Once that's enabled, sessions without hooks show the correct state from the existing model: `working`, `idle`, `waiting_input`, `waiting_permission`, `error`. Hive gets these by asking Laya to classify the session's current screen. For sessions with hooks, hooks stay authoritative. Laya overrides a hook's state only after the hook has been silent past a staleness threshold and the screen clearly shows a different state. Each state's tooltip names its source: hook, Laya, or heuristic. With no endpoint configured, or when Laya is unreachable or slow, Hive behaves exactly as it does today.

## Success criteria

- A committed corpus of real, labeled screen captures from Codex, shell and Pi (Aider optional — operator decision 2026-09-26) covers all five states, and an eval harness scores a running Laya server against it, reporting overall accuracy and recall on `waiting_input` + `waiting_permission` (target ≥90% / ≥95%). The feature ships **off by default**; meeting the target is a follow-up (fine-tuned checkpoint), not a merge criterion — operator decision 2026-09-25.
- In a hookless session, a state change is reflected in the UI within 3 s of the screen changing.
- In a Pi session where the screen shows a waiting prompt but no hook has fired within the staleness threshold, the session switches to the waiting state. While hooks are firing, Laya never overrides them.
- The state tooltip shows the source (hook / Laya / heuristic) for every session.
- With Laya disabled, unconfigured, stopped mid-session, or timing out, state detection matches today's behavior. No errors surface and no sessions hang.
- Screen text is sent only to the configured endpoint. The default endpoint is localhost.
- The feature works against the official PyTorch/Docker server on macOS, Windows and Linux, with no dependency on MLX.

## Non-goals

- Hive installing, downloading or running Laya itself (a later spec).
- Ranking or triaging which waiting session to look at first (a separate brainstorm once this ships).
- Exposing Laya to agents via MCP (a separate brainstorm).
- Adding new states to the state model (spec 336).
- Fixing the root cause of Pi hooks not firing. That's a separate bug; this spec only covers it with the staleness fallback.
- Cloud or remote classification endpoints as a default.
- Detecting `exited`, which stays driven by process exit.
- Fine-tuning a Laya checkpoint to reach the accuracy target (follow-up spec).

## Notes

Open questions:

- Staleness threshold N = 30 s with no new state-bearing agent event; heartbeats and pings do not count (decided at plan approval).
- Accuracy target moved to a follow-up (decided at plan approval).

Sources: <https://laya-ai.com/>, <https://dev.to/ayush7614/not-another-llm-i-tried-laya-a-421m-parameter-ai-decision-engine-6af>. Related: [336 session state model](336-session-state-model.md).
