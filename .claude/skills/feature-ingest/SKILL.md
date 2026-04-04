---
name: feature-ingest
description: Ingest a GitHub issue into the feature pipeline
disable-model-invocation: true
argument-hint: <issue-number>
allowed-tools: Read Glob Grep Edit Write Bash
---

# Ingest GitHub Issue into Feature Pipeline

Ingest GitHub issue **#$ARGUMENTS** into the feature tracking system.

## Steps

1. Run `gh issue view $ARGUMENTS --json number,title,body,labels` to fetch the issue
2. Check if a file already exists in `features/active/` or `features/completed/` starting with the zero-padded issue number (e.g., `016-*`). If so, warn the user and stop.
3. Generate a filename: zero-pad the issue number to 3 digits, slugify the title (lowercase, hyphens, max 50 chars). Example: `016-stale-preview-after-session-switch.md`
4. Read the template at `features/templates/FEATURE.md`
5. Create the feature file at `features/active/<filename>` by filling in:
   - `<title>` → issue title
   - `<number>` → issue number
   - Description section → issue body (cleaned up if needed)
   - Stage remains TRIAGE
6. Append a new row to the Active table in `features/BACKLOG.md`:
   `| — | #<number> | <title> | TRIAGE | — |`
7. Report what was created: filename, stage, and remind user to run `/feature-triage $ARGUMENTS` next

## Rules
- Do not modify the issue on GitHub at this stage (no labels yet)
- If no argument is provided, list open issues not yet ingested (compare `gh issue list` against existing feature files) and ask the user which to ingest
