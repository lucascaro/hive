---
name: feature-triage
description: Triage a feature — classify, estimate complexity, prioritize
disable-model-invocation: true
argument-hint: [issue-number]
allowed-tools: Read Glob Grep Edit Bash Agent
---

# Triage Feature

Triage feature **#$ARGUMENTS** (or the next untriaged feature if no argument given).

## Steps

1. **Find the feature:** If `$ARGUMENTS` is provided, find the matching file in `features/active/`. If not, read `features/BACKLOG.md` and pick the first feature with Stage = TRIAGE.
2. **Read the feature file** to understand the request.
3. **Classify:**
   - Type: `bug` or `enhancement`
   - Complexity: `S` (< 1 day, few files), `M` (1-3 days, moderate scope), `L` (3+ days, significant changes)
4. **Quick codebase scan:** Do a brief search (Glob/Grep) related to the feature to inform the complexity estimate. Don't do deep research — that's the next stage.
5. **Recommend priority:** Based on impact and complexity, suggest where this should sit in the backlog (P1 = top, higher number = lower priority).
6. **Present findings to user:** Show type, complexity, priority recommendation. Ask user to confirm or adjust.
7. **Update the feature file:**
   - Set Type, Complexity, Priority fields
   - Advance Stage to RESEARCH
8. **Update `features/BACKLOG.md`:**
   - Set the priority number and complexity in the Active table
   - Reorder rows by priority (P1 at top)
   - Update Stage to RESEARCH
9. **Update GitHub label:** `gh issue edit <number> --add-label triaged`
10. **Report:** Confirm triage is complete, remind user to run `/feature-research <number>` next

## Rules
- Always ask the user to confirm before writing changes
- If the feature should be rejected, close the GitHub issue (`gh issue close <number>`), delete the feature file, and remove it from BACKLOG.md
- One feature at a time
