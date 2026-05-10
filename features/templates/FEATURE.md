# Feature: <title>

- **GitHub Issue:** #<number>
- **Stage:** TRIAGE | RESEARCH | PLAN | IMPLEMENT | REVIEW | QA | DONE
- **Type:** bug | enhancement
- **Complexity:** S | M | L
- **Priority:** —
- **Branch:** —
- **PR:** —

## Description

<Summarize the GitHub issue. What problem does this solve?>

## Research

<Filled during RESEARCH stage.>

### Relevant Code
- `path/to/file.go` — <why it matters>

### Constraints / Dependencies
- <anything blocking or complicating this>

## Plan

<Filled during PLAN stage.>

### Files to Change
1. `path/to/file.go` — <what and why>

### Test Strategy
- <how to verify>

### Risks
- <what could go wrong>

## Implementation Notes

<Filled during IMPLEMENT stage.>

## PR convergence ledger

<Append-only. One entry per `/ralph-loop` iteration.>

- **<date> iter <N>** — verdict: <APPROVE|COMMENT|REQUEST_CHANGES>; findings_hash: <hex|empty>; action: <stop|autofix+push|escalated:<reason>>; head_sha: <short-sha>.

## QA verdict

<Filled by `/feature-qa` after PR merges. Append-only.>

- **<date>** — verdict: <PASS|FAIL|NEEDS_FOLLOWUP>; checks: <bullet summary>; followups: <issue numbers or "none">; one-line: <summary>.
