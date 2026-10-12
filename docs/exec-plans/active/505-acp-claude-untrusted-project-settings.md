# ACP Claude sessions load untrusted project hooks and MCP servers

- **Spec:** [docs/product-specs/505-acp-claude-untrusted-project-settings.md](../../product-specs/505-acp-claude-untrusted-project-settings.md)
- **Issue:** #505
- **Status:** active
- **PR:** #510
- **Branch:** feature/505-acp-untrusted-project-settings

## Summary

Stop the Claude ACP adapter from loading project-scoped hooks and MCP servers in untrusted folders. Spec holds the why.

## Research

- Adapter claude-agent-acp 0.85.1 spreads session/new+load `_meta.claudeCode.options` over its default settingSources (dist/acp-agent.js:6718-6830). startACP (internal/registry/acp.go) is the only NewSession/LoadSession caller.
- codex-acp 2.1.1 force-writes projects[cwd].trust_level="trusted" (dist/index.js:34203); pi-acp 0.0.34 loads <cwd>/.pi/ always. Neither has a settings knob.
- Claude trust store ~/.claude.json projects[path].hasTrustDialogAccepted; ancestor walk; undocumented, known inconsistencies.
- Conventions: scripts/test.sh; GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod); per-GOOS staticcheck/vet; proc.Command for exec; DaemonContract bump for daemon-side changes; changeset in .changesets/.
- Prior lessons: none searched.

## Approach

(Draft approved by the operator; see Second opinion and Decision log for changes.)

# Draft plan — #505 (repo root: /Users/lucascaro/checkout/hive/.worktrees/gentle-wind)

Decisions (operator): Claude → trust-gated settingSources; trust = Claude store (~/.claude.json projects[path].hasTrustDialogAccepted, exact or any ancestor) OR cwd inside a registered Hive project; store unreadable/no entry → fall back to Hive-project rule. Codex/Pi (no settings knob; codex-acp force-writes projects[cwd].trust_level="trusted", pi loads <cwd>/.pi always): start anyway; if cwd not in a Hive project → warn with a transcript notice.

## Approach
1. internal/agent: `ClaudeFolderTrusted(home, cwd) (trusted, known bool)` parses only `projects.<path>.hasTrustDialogAccepted` from ~/.claude.json via a minimal struct (never logs/returns other fields; file holds OAuth data). Walks cwd and ancestors (EvalSymlinks'd + raw).
2. internal/registry: `acpFolderTrust(cwd)`: Hive-project rule = cwd equals/under any Project.Cwd (symlink-resolved, path-boundary-safe). Claude: trusted = claudeStore || hiveProject. 
3. internal/acp: NewSession/LoadSession accept an `opts` that adds `_meta:{claudeCode:{options:{settingSources:["user"]}}}` when set. (Adapter 0.85.1 spreads `_meta.claudeCode.options` over its default at acp-agent.js:6718-6830; settingSources is a rebuild key so session/load honors it.)
4. registry.startACP: for Claude, untrusted → settingSources ["user"]; trusted → leave default. For Codex/Pi untrusted-by-hive → add a notice item to the transcript.
5. wire: new AcpItemNotice kind ("notice"); transcript.AddNotice; GUI AcpTranscript renders it (role="note"), lib/acp.ts type already open string. Bump DaemonContract? A GUI without the renderer drops the item (default → null), a daemon without it just omits it → no bump; confirm w/ check-daemon-contract.sh and use daemon-contract-override label if CI demands it.
6. Docs: docs/design-docs/acp-session-kind.md section "Project trust" (Claude gate, Codex/Pi verified behavior w/ adapter versions + source lines), DESIGN.md untouched, changeset (type fixed, patch).

## Files to change
internal/agent/claude_trust.go(+_test), internal/acp/agent.go + types.go, internal/registry/acp.go(+ acp_trust.go, tests), internal/wire/acp.go, internal/acp/transcript.go, cmd/hivegui/frontend/src/components/acp/AcpTranscript.tsx (+css, test), docs/design-docs/acp-session-kind.md, .changesets/acp-untrusted-project-settings.md, internal/acp/acptest/fake.go (record session/new params).

## Tests
- TestClaudeFolderTrusted: fixture claude.json (temp HOME) exact, ancestor, false, missing file, malformed JSON, symlinked cwd.
- TestACPFolderTrust (registry): under project, sibling-prefix path (/a/proj vs /a/proj2) not trusted, worktree under project trusted.
- TestACPNewSessionSendsSettingSources (acp, fake adapter echoes params): untrusted → settingSources ["user"] in session/new AND session/load; trusted → no _meta.
- Acceptance test (fake project with .claude/settings.json hook + .mcp.json): registry-level test starting a fake adapter in an untrusted dir asserts `_meta.claudeCode.options.settingSources == ["user"]`; and a real-adapter smoke (manual, no creds) described in verification.
- Codex/Pi notice test: untrusted cwd → transcript contains a notice; in a Hive project → none.
- AcpTranscript dom test renders notice.

## Verification
scripts/test.sh go unit dom; GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod); per-GOOS vet+staticcheck; scripts/check-daemon-contract.sh origin/main HEAD; real-adapter smoke: run pinned adapter in temp dir with a SessionStart hook that touches a marker file, drive initialize+session/new with and without the settingSources override, assert marker absent/present (no credentials needed if hook fires before auth; if not, report as unverified).

## Risks
- ~/.claude.json schema undocumented; known Claude inconsistency (ancestor walk vs exact match, github claude-code #79612) and persistence bugs (#71562) → store misses fall back to Hive-project rule, never to "trust".
- Trusted-by-Hive-project is weaker than Claude's dialog (user's call).
- User settings stay loaded; user-level hooks still run (intended).
- Credentials: code reads one boolean only; tests use fixtures; I never read the real file.


## REVISION 1 (applied must_fix from round 1)
- Hive-project rule narrowed: trusted iff cwd (symlink-resolved, cleaned, case-folded on darwin/windows) EQUALS a Project.Cwd that is not the auto-created default project (and not $HOME or a filesystem root), or lies under that project's own worktree dir (the paths Hive creates for it). Tests: project at a $HOME-like path + child repo => untrusted; default project => never trusts; sibling-prefix; case-fold; worktree of project => trusted.
- Real-adapter smoke is REQUIRED and has a control: temp dir with .claude/settings.json SessionStart hook writing marker A and .mcp.json server writing marker B; run pinned adapter with default settingSources (both markers present) and with ["user"] (both absent). If the CLI cannot start without credentials, the plan states the criterion is UNVERIFIED end-to-end and the PR says so; I will not touch credentials. 
- strictMcpConfig: sent only if the smoke shows ["user"] alone leaves .mcp.json servers connecting. It ignores user-level MCP servers too => surfaced to operator if needed.
- Residual: adapter's own SettingsManager reads project/local settings for permissions.defaultMode/bypass gating (acp-agent.js:6659,6711); verify no exec side effects (no apiKeyHelper/env use) and document as accepted residual.
- Contract: notice kind: old GUI ignores unknown kind, old daemon never sends it => no bump; run scripts/check-daemon-contract.sh; label only if it flags. Wire clients (hivegui app_*.go, hived-ws-bridge, testclient) checked for item-struct changes (none: kind is a string).
- Acceptance Go test builds the fake project dir, drives startACP for BOTH create (session/new) and restart (session/load), asserts the fake adapter's recorded params; missing recorded param fails.
- Trust re-evaluated at every startACP, never cached. Notice emitted once per start, not on replay (added after Reset, not persisted by adapter).


## Second opinion

Round 1: revise (7/10), 6 must_fix, applied. Round 2: revise (7/10), 4 must_fix, resolved by operator decisions below.

## Decision log

- Operator: Claude trust = Claude store, falling back to Hive-project rule; Codex/Pi start with a warning outside a Hive project.
- Operator approved with both recommendations: strictMcpConfig sent by default for untrusted Claude folders (drop only if smoke proves unneeded); bump DaemonContract (security fix).
- Complexity raised S -> M.


- **2026-10-11** — Dropped `strictMcpConfig` from the plan. Why: the real-adapter smoke (`scripts/acp-probe/untrusted-settings.mjs`) showed `settingSources: ["user"]` alone stops the project hook, `.mcp.json` server and `apiKeyHelper` (control run with defaults ran all three), and strict mode would also drop the user's own MCP servers. This was the operator's stated condition.
- **2026-10-11** — Bumped `go` to 1.27.2 inside this PR, at the operator's direction ("bump here"). Why: required `govulncheck` gate failed on GO-2026-6613 / GO-2026-6617 in the 1.27.1 standard library (pre-existing on `main`, fixed in 1.27.2).
- **2026-10-11** — Added `tools/staticcheck` (pinned module requiring x/tools v0.52.0) and pointed CI's staticcheck step and the AGENTS.md install note at it. Why: released staticcheck 2026.2.1 cannot read Go 1.27.2's export data ("export data version 5 is greater than maximum supported version 4"), so the Go bump alone turned the Linux leg red. Consequence of the operator-approved bump, not separate scope; drop it when a staticcheck release ships with x/tools >= v0.52.0.
- **2026-10-11** — Hive-project trust is exact-match only (project folder, or the session's own worktree). Why: ancestor matching would trust every clone under a home-rooted default project.

## Progress

- 2026-10-11: implemented; go, unit, dom green; per-GOOS vet green; staticcheck green on linux/windows (darwin cgo toolchain unusable locally); DaemonContract bumped to 26.

## PR convergence ledger

- **2026-10-11 iter 1** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: e9eaa6a95e13; threads_open: 0; action: escalated:ci-check-failed; head_sha: 62ceeb2.
- **2026-10-11 iter 2** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 344684328b58; threads_open: 2; action: autofix+push; head_sha: 3b010a2. The two open threads (case folding, notice wording) were resolved by hand in 5bbf082.
- **2026-10-11 iter 3** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: 9377592.
