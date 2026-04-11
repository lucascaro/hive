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
   - **Test Strategy:** Concrete, named test functions covering both unit tests and functional (flow) tests — AGENTS.md requires both for all changes. List each test with its file path, function name, and what it verifies. Follow existing patterns (e.g. `flowRunner` for flow tests in `internal/tui/flow_*_test.go`, direct `Update()` calls for component tests). Do not leave this section vague — every behavioral change must have a corresponding test.
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
- **Tests are mandatory.** AGENTS.md requires both unit tests and functional tests for all changes. The Test Strategy section must list concrete test function names, not vague descriptions. If a plan lacks tests, it is incomplete.
- **Keep the codebase clean.** Reuse existing functions, patterns, and helpers — do not duplicate logic. If a new abstraction is needed, check whether an existing one can be extended. Prefer small, focused changes over sprawling ones. Flag any dead code or unused imports the plan would introduce.
- Always get user approval before advancing to IMPLEMENT
- Follow the project's existing patterns — check AGENTS.md for conventions
