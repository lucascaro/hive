---
name: feature-implement
description: Implement a planned feature — code, test, open PR
disable-model-invocation: true
argument-hint: [issue-number]
---

# Implement Feature

Implement feature **#$ARGUMENTS** (or the next feature in IMPLEMENT stage if no argument given).

## Steps

1. **Find the feature:** If `$ARGUMENTS` is provided, find the matching file in `features/active/`. If not, read `features/BACKLOG.md` and pick the first feature with Stage = IMPLEMENT.
2. **Read the feature file** — verify the Plan section is filled and actionable. If not, tell the user to run `/feature-plan` first.
3. **Read AGENTS.md** for project conventions (build commands, testing, documentation rules).
4. **Create a feature branch:** `git checkout -b feature/<issue-number>-<slug>`
5. **Implement the plan:**
   - Follow the steps in the Plan section
   - Follow all conventions in AGENTS.md
   - Update `CHANGELOG.md` under `[Unreleased]` per Documentation Maintenance rules
   - Update any relevant docs (README, docs/, etc.) if the feature adds user-visible behavior
6. **Run checks:**
   - `go build ./...`
   - `go vet ./...`
   - `go test ./...`
7. **Fill in Implementation Notes** in the feature file:
   - Any deviations from the plan and why
   - Decisions made during coding
8. **Mark feature as done** (these changes go in the PR so they land in main on merge):
   - Set Stage to `DONE` in the feature file
   - Move feature file from `features/active/` to `features/completed/`
   - Update `features/BACKLOG.md`: remove the feature row from the Active table, renumber remaining rows sequentially, and add it to the Completed table with PR link and merge date
9. **Commit** with a descriptive message referencing `Fixes #<issue-number>`
10. **Offer to open a PR:** Ask the user if they want to push and create a PR. If yes:
    - `git push -u origin <branch>`
    - Create PR with `gh pr create` referencing the issue
    - Update GitHub labels: `gh issue edit <number> --remove-label planned --add-label implementing`

## Already-Merged Detection

Before starting implementation, check if the feature already has a PR link in its file. If it does, check if that PR is merged (`gh pr view <number> --json state`). If merged, run step 8 on the current branch (main), commit, and skip the rest.

## Rules
- Do not skip tests — all checks must pass before committing
- Follow AGENTS.md conventions exactly
- Ask before pushing or creating PRs
- One feature at a time — finish this before starting the next
