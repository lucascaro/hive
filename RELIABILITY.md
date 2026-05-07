# RELIABILITY.md

Reliability requirements and verification. What "good" looks like for availability, latency, error budgets, and recovery — and how the project mechanically checks it.

## SLOs

<One row per user-visible operation that has a target. Example:>

| Operation | Target | How measured |
|-----------|--------|--------------|
| Service startup | < 800 ms p95 | Boot trace |
| Critical request paths | < 2 s p99 | OTel span |

## Error budget

<If the project tracks one, document the policy here.>

## Failure modes

<Known ways this system breaks, and what the recovery looks like for each.>

- <Mode> — <recovery>

## Verification

<How reliability is checked: synthetic probes, load tests, chaos exercises, replay harnesses. Link to the harness or runbook.>
