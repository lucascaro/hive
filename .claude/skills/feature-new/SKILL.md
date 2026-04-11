---
name: feature-new
description: Create a GitHub issue and run it through ingest + triage
disable-model-invocation: true
argument-hint: [description]
allowed-tools: Read Glob Grep Edit Write Bash
---

# Create New Feature

Create a new GitHub issue from a description and run it through the feature pipeline (ingest + triage).

If `$ARGUMENTS` is provided, use it as the feature description. Otherwise, ask the user what feature they want.

## Steps

### Phase 1: Draft the issue

1. Based on the description in `$ARGUMENTS`, draft a GitHub issue:
   - **Title:** concise, imperative (e.g. "Add dark mode toggle")
   - **Body:** a `## Description` section explaining the problem and desired behavior (2-4 sentences)
2. Present the proposed title and body to the user. Wait for confirmation or edits before proceeding.

### Phase 2: Create the issue on GitHub

3. Run `gh issue create --title "..." --body "..."` and capture the new issue number from the output.

### Phase 3: Ingest into feature pipeline

4. Run `gh issue view <number> --json number,title,body,labels` to fetch the issue.
5. Check for duplicates: look for files in `features/active/` or `features/completed/` starting with the zero-padded issue number (e.g. `069-*`). If found, warn the user and stop.
6. Generate filename: zero-pad the issue number to 3 digits, slugify the title (lowercase, hyphens, max 50 chars). Example: `069-add-dark-mode-toggle.md`
7. Read the template at `features/templates/FEATURE.md`.
8. Create the feature file at `features/active/<filename>` filling in:
   - Title and issue number from the GitHub issue
   - Description section from the issue body
   - Stage remains TRIAGE
9. Append a new row to the Active table in `features/BACKLOG.md`:
   `| — | #<number> | <title> | TRIAGE | — |`

### Phase 4: Triage

10. **Classify** the feature:
    - Type: `bug` or `enhancement`
    - Complexity: `S` (< 1 day, few files), `M` (1-3 days, moderate scope), `L` (3+ days, significant changes)
11. **Quick codebase scan:** Do a brief Glob/Grep search related to the feature to inform the complexity estimate. Don't do deep research — that's the RESEARCH stage.
12. **Recommend priority:** Based on impact and complexity, suggest where this should sit in the backlog (P1 = top, higher number = lower priority). Consider existing items in `features/BACKLOG.md` when choosing.
13. **Present findings to user:** Show type, complexity, and priority recommendation. Wait for confirmation or adjustment.
14. **Update the feature file:**
    - Set Type, Complexity, Priority fields
    - Advance Stage to RESEARCH
15. **Update `features/BACKLOG.md`:**
    - Set the priority number and complexity in the Active table row
    - Reorder rows by priority (P1 at top)
    - Update Stage to RESEARCH
16. **Update GitHub label:** `gh issue edit <number> --add-label triaged`

### Phase 5: Report

17. Summarize what was created:
    - GitHub issue number and URL
    - Feature file path
    - Type, complexity, priority
    - Current stage (RESEARCH)
18. Remind user to run `/feature-research <number>` next.

## Rules
- Always show the proposed issue contents and get user confirmation before creating on GitHub
- Always show triage classification and get user confirmation before writing changes
- Single feature at a time
- Follow existing filename conventions (3-digit zero-pad, slugified title, max 50 chars)
- If no argument is provided, ask the user to describe the feature before proceeding
