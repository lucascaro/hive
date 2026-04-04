---
name: feature-next
description: Show feature pipeline status and recommend the next action
disable-model-invocation: true
allowed-tools: Read Glob Grep Bash
---

# Feature Pipeline Status

Show the current state of the feature pipeline and recommend the next action.

## Steps

1. Read `features/BACKLOG.md` to get all active features
2. For each active feature, read its file in `features/active/` to get the current stage
3. Display a summary table:

```
Feature Pipeline Status
=======================
#  | Issue | Title                  | Stage    | Complexity
---|-------|------------------------|----------|----------
1  | #16   | Stale preview on exit  | RESEARCH | M
2  | #13   | Fix mouse support      | TRIAGE   | —
```

4. Check for un-ingested GitHub issues: run `gh issue list --state open --json number,title` and compare against existing feature files in `features/active/` and `features/completed/`
5. Recommend the next action based on priority:
   - If there are IMPLEMENT-stage features → "Run `/feature-implement <number>` to implement"
   - If there are PLAN-stage features → "Run `/feature-plan <number>` to create implementation plan"
   - If there are RESEARCH-stage features → "Run `/feature-research <number>` to research"
   - If there are TRIAGE-stage features → "Run `/feature-triage <number>` to triage"
   - If there are un-ingested issues → "Run `/feature-ingest <number>` to ingest"
   - Otherwise → "Pipeline is clear. No pending work."

## Rules
- Always show the full table, even if empty
- List un-ingested issues separately below the table
- Recommend only ONE next action (the highest-priority, most-advanced stage)
