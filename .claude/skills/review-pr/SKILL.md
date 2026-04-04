---
name: review-pr
description: Deep PR review — correctness, safety, performance, UX, consistency
argument-hint: [pr-number]
allowed-tools: Read Glob Grep Bash Agent
---

# Review Pull Request

Perform a thorough review of PR **#$ARGUMENTS**.

## Setup

1. Read `AGENTS.md` to internalize project conventions, package map, key types, and data flows.
2. Fetch the PR diff: `gh pr diff $ARGUMENTS`
3. Read the PR description: `gh pr view $ARGUMENTS`
4. Identify every file changed and categorize them (production code, tests, config, CI, docs).

## Review Passes

Launch **3 parallel Agent** reviews, each focused on a different dimension. Each agent must read the full diff AND the surrounding context of changed files (not just the diff lines — read the full functions/methods being modified).

### Agent 1: Correctness & Logic

Check every changed function for:
- **Logic errors** — wrong conditions, off-by-one, missing cases, unreachable code
- **Type misuse** — using the wrong type or constructor for an API (e.g., sending `tea.KeyRunes` with multi-char strings like `"enter"` when `tea.KeyEnter` is the correct key type; calling a method with wrong argument types)
- **Interface compliance** — if a struct implements an interface, verify ALL methods are present and signatures match. `grep` the interface definition and compare.
- **Message chain integrity** — for Bubble Tea code, trace the full message chain: does the cmd from Update() produce the expected Msg? Does that Msg get handled? Does the handler return the right next cmd?
- **Error handling** — are errors silently swallowed? Are nil checks missing where a pointer could be nil?
- **Concurrency** — data races on shared state, missing locks, goroutine leaks
- **Edge cases** — empty inputs, zero values, nil slices/maps, boundary conditions

### Agent 2: Safety & Test Isolation

Check for:
- **Filesystem safety** — does ANY code path during tests touch real user files? Trace `config.Dir()`, `config.StatePath()`, `config.LogPath()`, `config.Save()`, `saveState()`, `saveUsage()`. Check `init()` functions — they run before `TestMain`.
- **Global state** — identify all package-level mutable variables. Are they safe in tests? Could parallel test runs corrupt them? Is cleanup happening in the right order?
- **Environment leaks** — does `t.Setenv()` cover all env vars that affect behavior? Could a missing override cause real-world side effects?
- **Golden file determinism** — scan every `.golden` file for: absolute paths, temp dir paths, timestamps, random values, color/ANSI codes, platform-specific rendering, CWD-dependent content. Any non-deterministic content = flaky CI.
- **Test assertions** — are tests actually asserting what they claim? Watch for `t.Skip` hiding failures, bifurcated assertions that pass on both branches, assertions on wrong fields.
- **Dependency safety** — new dependencies: are they maintained? Do they have known vulnerabilities? Are they pinned to a specific version?

### Agent 3: Performance, UX & Consistency

Check for:
- **Performance** — O(n²) loops, unnecessary allocations in hot paths (View(), Update()), blocking operations in the UI goroutine, unbounded growth of maps/slices
- **TUI/UX correctness:**
  - View() must return exactly `TermHeight` lines — no more, no less (prevents terminal scroll corruption)
  - No line in View() output should exceed `TermWidth` (prevents wrapping artifacts)
  - Key isolation: when a modal/overlay is active, global keys must not leak through
  - Focus management: does opening/closing overlays correctly save/restore focus?
  - Status bar: does it show accurate context for the current state?
- **Consistency with codebase:**
  - Read existing code patterns in the same package. Does the new code follow them?
  - Are existing helpers/utilities reused instead of reinvented? (grep for similar functions)
  - Naming: does it follow Go conventions and the project's naming patterns?
  - Comments: are they accurate? Do they describe "why" not "what"?
  - Does the code match the patterns documented in `AGENTS.md`?
- **CI/workflow changes** — are they correct for all platforms (Linux, macOS, Windows)? Do they introduce new dependencies that need to be installed?

## Output Format

After all 3 agents complete, synthesize their findings into a single structured review. Deduplicate overlapping findings.

### Structure

```
## BLOCKING (must fix before merge)
1. [File:line] Description — why it matters, suggested fix

## IMPORTANT (should fix, could be fast follow-up)
1. [File:line] Description — why it matters, suggested fix

## MINOR (nice to have)
1. [File:line] Description

## Verdict
APPROVE / REQUEST_CHANGES / COMMENT
One-sentence summary of overall assessment.
```

### Rules

- Cite **specific file paths and line numbers** from the diff for every finding.
- For each finding, explain **why** it's a problem (not just what's wrong).
- Include a **suggested fix** for BLOCKING and IMPORTANT items — be concrete, show code if helpful.
- Don't flag style-only issues unless they violate patterns established in AGENTS.md.
- Don't flag missing tests for code that is itself test infrastructure (test helpers, mocks).
- Do flag tests that don't actually test what they claim to test.
- If golden files are present, spot-check 2-3 for determinism issues.
- If the diff touches the `mux.Backend` interface, verify ALL implementations (mock, tmux, native) are updated.
- Limit to 15 findings max. Prioritize impact over quantity.
