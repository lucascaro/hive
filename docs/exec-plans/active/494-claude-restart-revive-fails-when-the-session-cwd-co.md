# Claude Restart/Revive fails when the session cwd contains "_"

- **Spec:** [docs/product-specs/494-claude-restart-revive-fails-when-the-session-cwd-co.md](../../product-specs/494-claude-restart-revive-fails-when-the-session-cwd-co.md)
- **Issue:** #494
- **Status:** active
- **PR:** #497
- **Branch:** feature/494-claude-resume-cwd-encoding

## Summary

Make `encodeClaudeProjectDir` produce exactly the directory name Claude
writes under `~/.claude/projects/`, so `claudeSessionExists` (Restart/Revive)
and `claudeTranscriptPaths` (transcript search) find the transcript for any
cwd.

## Research

- `internal/agent/claude.go:28-34` — `encodeClaudeProjectDir`, folds only
  `/ . :`. Two callers, both in the same file: `claudeSessionExists` (feeds
  `claudeResumeArgs`) and `claudeTranscriptPaths` (transcript search). Fixing
  the shared encoder fixes both.
- `internal/agent/claude_test.go:9-55` — table test for the encoder; existing
  rows all still hold under the new rule.
- `internal/agent/pi_paths_test.go:40,147` — asserts pi's encoder differs from
  Claude's and builds a Claude dir for a fixture; must stay green.
- **Claude's real encoder** (read from the claude 2.1.288 bundle, the version
  the bug was observed on):
  `k(e)=e.replace(/[^a-zA-Z0-9]/g,"-")`; `uP(e)`: if `k(e).length<=200` →
  `k(e)`, else `k(e).slice(0,200)+"-"+Math.abs(h(e)).toString(36)` where
  `h(e)` is `(h<<5)-h+e.charCodeAt(n)|0` over the **raw** cwd. JS regex without
  `/u` works per UTF-16 code unit, so a non-BMP rune becomes `--`.
  Claude passes its own `process.cwd()` (native separators on Windows), so
  `C:\Users\u\repo` → `C--Users-u-repo`, which the new rule also gives
  without the `ToSlash` step.
- `scripts/acp-probe/agents.mjs:12-35` — JS mirror of the Go encoder plus an
  `Observed` variant documenting the divergence; `hive_branch` is computed
  from the mirror.
- `docs/design-docs/acp-workflows.md:85-99,272-276` — F1 and follow-up 0
  describe this bug as current.
- DaemonContract: `internal/agent/` is outside the gated dirs and a new GUI
  against an old daemon still works (the bug just remains), so no bump.

### Prior lessons

No prior lessons matched.

### Conventions card

- Build: `./build.sh` · Tests: `scripts/test.sh [go|unit|dom|e2e]`.
- Verify under CI toolchain: `GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod)`.
- Static analysis per GOOS: `for os in darwin linux windows; do GOOS=$os staticcheck ./... ; GOOS=$os go vet ./... ; done`.
- TDD: every fix ships with the test that would have caught it.
- User-visible change → `.changesets/<slug>.md` (`type: fixed`, `bump: patch`); never edit `CHANGELOG.md`.
- Shell-outs via `internal/proc` only (not relevant here: no new spawns).

## Approach

Replace the body of `encodeClaudeProjectDir` with a port of Claude's `uP`:
iterate `utf16.Encode([]rune(filepath.Clean(cwd)))`, map each unit outside
`[A-Za-z0-9]` to `-`; if the result exceeds 200 units, keep the first 200 and
append `-` + `strconv.FormatInt(abs(int64(hash)), 36)` with `hash` the int32
Java-style hash over the same UTF-16 units. Iterating UTF-16 units (not Go
runes or bytes) is what makes the length cut and the non-BMP `--` match
Claude byte-for-byte. `int64` abs avoids the `MinInt32` overflow that JS's
float `Math.abs` does not have.

Alternative ruled out: a plain `regexp.ReplaceAllString("[^A-Za-z0-9]", "-")`
— shorter, but works per rune (emoji → one `-`) and misses the 200-char
truncation, so deep worktree/temp paths would still miss.

### Files to change

1. `internal/agent/claude.go` — new `encodeClaudeProjectDir` + doc comment
   citing Claude's encoder and version, stating the hash runs over the raw
   cleaned cwd with native separators (which is why Windows matches).
2. `internal/agent/claude_test.go` — new table rows; new on-disk resume test;
   fix the Windows-row comment that mentions `ToSlash`.
3. `scripts/acp-probe/agents.mjs` — `encodeClaudeProjectDir` ports the full
   encoder (replace + 200 cut + hash); delete `encodeClaudeProjectDirObserved`
   / `claudeSessionExistsObserved`; `hiveResume` computes one branch and
   returns `hiveBranch: branch` (as codex/pi already do). The `hive_branch`
   results field stays (schema shared by all agents); the committed
   `results/claude-2026-10-03.json` is historical evidence and is not rewritten.
   Refresh stale `claude.go:` line citations.
4. `scripts/acp-probe/probe.test.mjs` — the "real encoding found where Hive's
   encoder misses" test (:166-176) becomes "transcript under a `_` cwd
   resumes": asserts `branch === hiveBranch === 'resume'`; import switches to
   `encodeClaudeProjectDir`. Add a truncation row asserting the mirror gives
   the same long-path value as the Go test.
5. `docs/design-docs/acp-workflows.md` — :70 table cell prose ("Hive today
   picks `--session-id`") → past tense + fixed in #494 (cell still starts with
   `pass [run]`, which is what `check-doc.mjs` compares); F1 (:85-99) and
   follow-up 0 (:272-276) marked fixed by #494; :96 `hive_branch` note kept
   accurate.

### New files

- `.changesets/494-claude-resume-cwd-underscore.md` — `type: fixed`, `bump: patch`.

### Tests

All expected strings below were produced by running Claude's own JS
(`uP`/`k`/hash copied verbatim) under node, not by the Go code.

- `TestEncodeClaudeProjectDir` (`internal/agent/claude_test.go`) — add rows:
  - `/var/folders/x_y/T/my repo` → `-var-folders-x-y-T-my-repo` (underscore + space; fails today).
  - `/Users/u/café/日本` → `-Users-u-caf----` (non-ASCII, one `-` per BMP unit).
  - `/Users/u/😀x` → `-Users-u---x` (non-BMP → two `-`).
  - `/Users/u/` + 191×`a` (exactly 200) → unchanged, no hash (boundary).
  - 13× `very_long_segment` (no separators, positive hash) → `…very-long-seg-qqxzx5`.
  - 101× `x_` (no separators, negative hash) → `…x--cuqrml`.
  Hash rows are separator-free so `filepath.Clean` leaves them identical on
  every GOOS (Windows CI runs `go test ./...`; a `/`-path would be hashed in
  `\` form there). Separator handling is covered by the non-hash rows.
  A comment notes `int64` abs makes the `MinInt32` case safe.
  - Existing rows (dotted, trailing slash, Windows drive) unchanged.
- `TestClaudeResumeArgsFindsTranscriptInUnderscoreCwd` — `t.Setenv("HOME", tmp)`
  (+`USERPROFILE` for Windows), write `~/.claude/projects/<claude-encoded>/<id>.jsonl`
  using the literal expected dir name, assert `claudeResumeArgs` returns
  `--resume`. Uses the real `claudeSessionExists`, no stub. Fails today.
- `scripts/acp-probe/probe.test.mjs` — rewritten `_`-cwd test + new truncation row (see Files 4).

### Verification

- `go test ./internal/agent/ -run 'TestEncodeClaudeProjectDir|TestClaudeResumeArgs|Pi' -v` — new rows fail on `main`, pass after.
- `GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod) scripts/test.sh go`
- `for os in darwin linux windows; do GOOS=$os go vet ./internal/agent/ && GOOS=$os staticcheck ./internal/agent/; done`
- `node --test 'scripts/acp-probe/*.test.mjs' && node scripts/acp-probe/check-doc.mjs docs/design-docs/acp-workflows.md` — the CI probe job. The rewritten `_`-cwd probe test fails on the current `agents.mjs`.

### Open questions / risks

- Claude changes its encoder again → lookups miss silently. The doc comment
  names the version it was read from; the resume test pins the expected dir
  name literally, so a deliberate update is one place.
- cwd symlink resolution (`/tmp` vs `/private/tmp`) is a separate, unverified
  gap — non-goal.

## Second opinion

- **Round 1:** revise, confidence 8. Must-fix (all 5 applied): Windows CI would hash `\`-form test paths (rows now separator-free); `probe.test.mjs:166-176` asserted the old divergence; dead `Observed` duplicates in the probe mirror; design-doc table cell :70 also stale; JS mirror's truncation path untested.
- **Round 2:** approve, confidence 8. Nice-to-have, adopted: mirror comment notes Go cleans the path first; Verification states the rewritten probe test fails on the current `agents.mjs`.

## Decision log

- **2026-10-03** — Port Claude's full `uP` (truncate + hash, UTF-16 units), not just the regex. Why: the issue's suggested regex alone still misses cwds over 200 chars and non-BMP names; read from the 2.1.288 bundle.
- **2026-10-03** — Spec ingested by the loop from the existing issue (no separate `/hs-feature-ingest` run). Why: issue was concrete and the ingest skill isn't installed here.

## Progress

- **2026-10-03** — Spec created, triaged (bug, S, P1), research done.
- **2026-10-03** — Plan approved (chat). Implemented on `feature/494-claude-resume-cwd-encoding`: Go encoder port, tests red-then-green, probe mirror + tests (new probe tests proven to fail on the old mirror), design doc, changeset. Go suite green under go1.27.1; vet + staticcheck clean on darwin/linux/windows; probe tests + check-doc green.
