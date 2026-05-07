# Golden principles

Mechanical, opinionated rules that keep Hive legible and consistent for future agent runs. `gc-sweep` reads this file, scans the code for deviations, and opens small targeted refactor PRs.

Each principle has:

- A short rule.
- A **Why** line — the failure mode it prevents.
- A **Detection** line — how `gc-sweep` (or a human) can find a deviation.
- A **Fix shape** line — what the refactor PR should look like.

---

## 1. Session and project state mutates only through `Registry` methods

**Why:** `internal/registry/` owns persistence and the cross-process invariants for sessions, projects, and ordering. Callers that reach into `Session` or `Project` fields directly bypass dirty-tracking, atomic writes, and worktree-state checks — the registry can no longer guarantee what it's persisting.

**Detection:** writes to fields of `*registry.Session` / `*registry.Project` (or whatever struct types live under `internal/registry/`) from any package other than `internal/registry/`. Grep starting points: `\.Name\s*=`, `\.Color\s*=`, `\.Status\s*=`, `\.Cwd\s*=` on registry-typed values outside the package.

**Fix shape:** add or extend a method on `Registry` (e.g., `UpdateSessionName`, `SetProjectColor`) that performs the mutation, validates pre/post-conditions, and triggers persistence. Replace the direct field write with the method call. Add a unit test in `internal/registry/`.

---

## 2. Validate at the boundary; never probe shapes inside the system

**Why:** internal code shouldn't carry uncertainty about its inputs. Probing leads to defensive paths that mask real bugs and let bad data drift deeper.

**Detection:** ad-hoc shape checks on values that crossed a boundary — wire frames, config files, hook env, escape-sequence parses. Grep: untyped `map[string]any` access past the parser, `interface{}` type switches outside `internal/wire/`.

**Fix shape:** define or extend a typed struct at the entry point (in `internal/wire/`, the config loader, etc.); replace internal probing with the parsed type. If the boundary parser doesn't exist yet, that's the PR.

---

## 3. Cross-platform splits use build tags or filename suffixes, never `runtime.GOOS` switches in domain code

**Why:** `internal/notify/`, `internal/worktree/`, and the `cmd/hivegui/os_terminal*` / `restart_*` / `spawn_*` / `window_*` files already gate platform code at the package boundary via `_darwin.go` / `_linux.go` / `_windows.go` suffixes. `if runtime.GOOS == "darwin"` checks scattered through domain logic produce dead branches on other platforms, untested code paths, and silent skew between the macOS, Linux, and Windows release builds.

**Detection:** `runtime.GOOS` or `runtime.GOARCH` references anywhere outside `cmd/`, `internal/buildinfo/`, and the platform-suffixed files themselves. Grep: `runtime\.GOOS` across `internal/`.

**Fix shape:** lift the platform decision to a small interface in the relevant package and add filename-suffixed implementations (`thing_darwin.go`, `thing_linux.go`, `thing_windows.go`). Domain code calls the interface; the platform split disappears from the call site.

---

## 4. Tests ship with the change — behaviour changes carry a regression test

**Why:** "boil the lake now, not later." A bug fix without a test means the same bug can come back unannounced. A new feature without a test means the next refactor breaks it silently.

**Detection:** PRs that change behaviour in `internal/daemon/`, `internal/registry/`, `internal/session/`, `internal/wire/`, or `internal/worktree/` without a corresponding `*_test.go` change. Bug-fix commits whose message names the bug but whose diff has no new test asserting the bug is gone.

**Fix shape:** add a unit test alongside the changed code that fails on the pre-fix / pre-feature behaviour and passes after. For protocol changes, include a wire round-trip test (see principle 6). For session/registry changes, exercise the public method, not internal fields.

---

## 5. Background goroutines have explicit lifecycle

**Why:** `hived` is a long-lived process that survives GUI restarts. Fire-and-forget goroutines leak across reconnects and produce ghost sessions, dangling PTY readers, and zombie watchers — exactly the v1 tmux-era pain `hived` was meant to eliminate.

**Detection:** `go func()` or `go someFunc()` in `internal/daemon/`, `internal/session/`, or `internal/registry/` whose body neither (a) selects on a `context.Context.Done()` / shutdown channel, nor (b) is owned by a struct with a `Close()` / `Stop()` that signals it. Grep starting points: `^\s*go\s+` in those packages, then read the body.

**Fix shape:** give the goroutine an explicit owner. Either pass `ctx` and `select` on `ctx.Done()` in the loop, or attach the goroutine to a struct that stores a cancel func / quit channel and exposes `Close()`. Add a test that creates the owner, calls `Close()`, and asserts the goroutine exits (via `sync.WaitGroup` or by observing the goroutine's effect stop).

---

## 6. Every wire frame type has a round-trip encode/decode test

**Why:** the wire protocol is the contract between `hived` and `hivegui`. A field added on one side without test coverage drifts silently — the GUI parses it as zero, the daemon thinks it sent it, nobody notices until a user reports the symptom. JSON-tag policy is documented in DESIGN.md as a hard rule; this principle enforces *test* coverage of that contract.

**Detection:** new exported types or fields in `internal/wire/` (control frames, payloads, mode specs) without a corresponding test in `internal/wire/wire_test.go` that encodes a populated value to JSON and decodes it back into an equal value.

**Fix shape:** add a table-driven test case in `internal/wire/wire_test.go` that round-trips the type — populate every field with a non-zero value, encode to JSON, decode, and assert deep equality. If the test reveals a missing or wrong `json:"snake_case"` tag, fix the tag in the same PR.

---

## 7. Auto-fix high-confidence, low-risk review findings in the same PR

**Why:** deferred nits become permanent. The "boil the lake now" stance means review feedback that's mechanical (rename, comment, constant, helper extraction, API consistency) gets applied before merge — not in a follow-up that never comes.

**Detection:** open review comments on a PR marked resolved-without-change or punted to "follow-up" without a linked issue, when the suggested fix is a pure refactor (no behaviour change).

**Fix shape:** apply the fix in the same PR, push as a fixup commit. Only defer when the change is genuinely high-risk (cross-cutting refactor, behaviour change) or low-confidence (taste call) — and link an issue if so.
