---
name: feature-research
description: Research a feature — explore codebase, document findings
disable-model-invocation: true
argument-hint: [issue-number]
allowed-tools: Read Glob Grep Edit Bash Agent
---

# Research Feature

Research feature **#$ARGUMENTS** (or the next feature in RESEARCH stage if no argument given).

## Steps

1. **Find the feature:** If `$ARGUMENTS` is provided, find the matching file in `features/active/`. If not, read `features/BACKLOG.md` and pick the first feature with Stage = RESEARCH.
2. **Read the feature file** to understand the request and any triage notes.
3. **Explore the codebase:** Use Explore agents to investigate:
   - Which files and functions are relevant to this feature
   - Existing patterns that could be reused or extended
   - How similar functionality is implemented elsewhere in the codebase
   - Edge cases and potential complications
4. **Document findings** in the feature file's Research section:
   - **Relevant Code:** List specific files with paths and line numbers, explaining why each matters
   - **Constraints / Dependencies:** Anything that blocks or complicates the work
   - Add any other findings that will help planning
5. **Deep research (if needed):** For complex features (M/L), create a detailed research doc at `research/<slug>/RESEARCH.md` following the existing pattern in `research/tui-rendering/RESEARCH.md`, and link to it from the feature file.
6. **Assess readiness:** Is there enough information to write an implementation plan? If not, note what's missing and continue researching.
7. **Advance stage:** When research is sufficient:
   - Update Stage to PLAN in the feature file
   - Update Stage to PLAN in `features/BACKLOG.md`
   - Update GitHub labels: `gh issue edit <number> --remove-label triaged --add-label researching`
   (Note: we set "researching" even as we advance to PLAN — the `/feature-plan` skill will update to "planned")
8. **Report:** Summarize key findings and remind user to run `/feature-plan <number>` next

## Rules
- Be thorough but focused — research what's needed for planning, not everything about the area
- Always include file paths with line numbers for relevant code
- If deep research is warranted, create a research/ doc rather than bloating the feature file
