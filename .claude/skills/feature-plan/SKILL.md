---
name: feature-plan
description: Create implementation plan for a researched feature
disable-model-invocation: true
argument-hint: [issue-number]
allowed-tools: Read Glob Grep Edit Bash Agent
---

# Plan Feature Implementation

Create an implementation plan for feature **#$ARGUMENTS** (or the next feature in PLAN stage if no argument given).

## Steps

1. **Find the feature:** If `$ARGUMENTS` is provided, find the matching file in `features/active/`. If not, read `features/BACKLOG.md` and pick the first feature with Stage = PLAN.
2. **Read the feature file** — verify the Research section is filled in. If not, tell the user to run `/feature-research` first.
3. **Read referenced files:** Open the relevant code files identified during research to understand the current implementation.
4. **For complex features (M/L):** Use Plan agents to design the approach, considering trade-offs.
5. **Write the Plan section** in the feature file:
   - **Files to Change:** Numbered list with file paths and what to change in each
   - **Test Strategy:** How to verify the changes work (unit tests, manual testing, specific scenarios)
   - **Risks:** What could go wrong, edge cases to watch for
6. **Present the plan to the user.** Walk through the key decisions and ask for approval before advancing.
7. **On approval:**
   - Update Stage to IMPLEMENT in the feature file
   - Update Stage to IMPLEMENT in `features/BACKLOG.md`
   - Update GitHub labels: `gh issue edit <number> --remove-label researching --add-label planned`
8. **Report:** Confirm plan is locked in, remind user to run `/feature-implement <number>` next

## Rules
- The plan must be specific enough that someone (human or AI) could implement it without re-reading the research
- Include file paths for every file that will be changed
- Always get user approval before advancing to IMPLEMENT
- Follow the project's existing patterns — check AGENTS.md for conventions
