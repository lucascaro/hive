# Laya state classifier

Spec: [458](../product-specs/458-classify-session-state-with-a-local-laya-model-whe.md).
Plan: [exec plan](../exec-plans/completed/458-classify-session-state-with-a-local-laya-model-whe.md).

Hive knows what a session is doing exactly when the agent says so through
hooks (Claude) or its extension (Pi). Everything else (Aider, Codex, a
shell, custom agents, and a Pi whose reports stopped arriving) falls back
to the heuristic tier, which only knows that bytes arrived and then
stopped. The Laya classifier fills that gap by asking a user-run Laya
decision model what the visible screen shows.

## When a session is classified

All of these must hold, checked in `registry.classifyDueLocked`:

- **Enabled.** `agent-settings.json` has `laya_enabled`, which is off by
  default.
- **No live tier.** `Machine.Classifiable`: the session is alive, not
  waiting on the user, and either no tier is trusted, or the trusted one
  has sent no *state-bearing* event for `HookStaleAfter` (30 s).
  - Heartbeats (`Replay`) and pings do not count. The Pi extension
    re-sends its last report every 5 s, which keeps `hookSeenAt` fresh
    forever. `lastEventAt` is the separate clock that answers "is the
    agent still telling us anything new".
- **Settled screen.** The screen digest the sampler recorded equals the
  live one, and the screen has not changed for `ClassifyQuietAfter`
  (1 s). A streaming reply changes the digest every tick and is never
  sent.
- **Not already answered.** The screen differs from the last one Laya
  answered, or a state-bearing agent event has arrived since that answer
  (ping, `Replay`, plan, plan item and subagent activity do not count;
  the answer predates the agent speaking, so once the agent is stale
  again the screen is re-asked). A failed call does not count as an answer; the
  backoff below paces those retries.
- The one exception is a Laya `working` on an unchanged screen, which is
  re-asked after `LayaRecheckAfter` (30 s). The recheck clock is stamped
  on every attempt, so a static screen costs one call per window.
- Selection reads only what the sampler already recorded. A screen is
  rendered, and its live digest checked, only for a session that is due.

## Trust rules

- A classification is source `laya`. It never touches `hookSeenAt`,
  `orderAt`, `reportedAt` or `lastEventAt`, so a stale tier stays stale
  and the agent's next real event takes the session straight back
  through `Apply`.
- An agent's or a bell's wait, and `error`, are never overridden. They
  stand until the user acts, exactly as they do against `Output`.
- **A Laya wait is not sticky.** It is Hive's guess about one screen:
  - A screen change clears it (`Output` gives it back to heuristic
    `working`).
  - On a heartbeating Pi, where `Output` leaves the tier alone, Laya
    may revise its own wait.

  This was found end to end. A shell's startup banner read as
  `waiting_input`, and when that wait stayed up it pulsed until
  someone looked, and it hid the permission prompt printed after it.
- `Tick` does not time out a Laya state. Doing so would flip it to idle
  until the next classification put it back.
- `Replay` does not restore the extension's stale state over a Laya
  state, for the same flicker reason.
- On a session without hooks, `Output` hands a Laya state back to the
  heuristic tier's `working` (the screen moved).
- On a Pi that is heartbeating, the tier is still trusted, so `Output`
  only stamps the time and the digest gate asks Laya again.
- A bell on a Laya-sourced session takes the tier back from Laya: to the
  extension when one is keyed on the session (as its last word, so a
  heartbeat keeps the wait), otherwise to the heuristic tier. A bell is
  the program asking, not a guess.
- Events that move no state (ping, plan, plan item, subagent activity)
  do not relabel a Laya state as the agent's.
- Laya-produced waits and errors raise `needs_attention` and desktop
  notifications. Missing a blocked agent costs more than a false alert.

## The call

- It is made with `r.mu` **released**: snapshot under the lock, ask, then
  re-take the lock and re-check that the entry is the same, still running
  the same process (a restart attaches a new one, and resets the attempt
  record), and showing the same screen. The screen is checked live,
  because the sampler's digest can be a tick behind.
- The text sent and the digest checked on apply come from one
  `ScreenSnapshot`, taken after the lock is released. If the screen has
  moved since selection, the call is skipped. Rendered separately, a
  screen that went A→B→A during the call would get B's answer as A's.
- Anything else that happened meanwhile (an agent event, a bell, the
  user answering) is caught by `Classify`'s own `Classifiable` check.
- On any error the state is left as the heuristic tier had it, and the
  loop backs off: 2 s, doubling up to 60 s, reset on success. A dead
  server costs one call per window, not one per session per cycle. A
  screen that sat still through an outage is asked about once the server
  answers again.
- `Close` cancels the loop's context and waits for it before closing
  listeners, so an in-flight call can never broadcast into a closing
  registry.

## Transport

The client (`internal/laya`) speaks Laya's own `POST /v1/systemone`,
which is what every Laya server implements: upstream `laya-serve`,
laya-server, and Rapid-MLX's laya-mlx integration. An LLM server such as
oMLX does not serve Laya. Laya is a decision encoder, not a generative
model, so a `laya-mlx` checkpoint in oMLX's model directory is neither
listed nor loadable there (checked against oMLX 0.6.4). `GET /health` is
the connection test, and it is unauthenticated on every server above.

## Why a tail and opaque labels

- Laya's English checkpoint reads 512 tokens and silently truncates to
  the *first* window. The part of a terminal that says "waiting for you"
  is the bottom, so the client sends only the tail of the screen.
- Laya's docs note that option order and boolean-looking labels bias
  its answers, so the five states go out as opaque labels, each with a
  description.

## Accuracy

- The base checkpoint is near chance before any training, on Laya's own
  benchmark. Terminal text is untested.
- The feature ships off by default, with a labelled corpus
  (`internal/laya/testdata/corpus/`) and an env-gated scoring test.
- Reaching ≥90% overall and ≥95% on the waits is a follow-up: fine-tune
  a checkpoint and name it in the `laya_model` setting.

## Privacy

- Screen text goes only to the configured URL, which defaults to
  localhost. Settings warns when the host is not a loopback address.
- Screens pass the same secret scrubber as the corpus
  (`laya.Scrub`) before they are sent. Scrubbing is defence in depth,
  not the guarantee.
- The guarantee that text reaches only the configured URL comes from
  `laya.NewClient`, which never follows a redirect. Go's default client
  would re-send the POST body on a 307 or 308 to any host. A redirect
  fails the call instead.
- HTTP and HTTPS are both accepted. A local server is plain HTTP, and
  a remote URL gets the Settings warning.
- The API key comes from the daemon's `HIVE_LAYA_API_KEY` and is never
  written to disk by Hive.
- Corpus captures (`HIVE_LAYA_CAPTURE_DIR`) are written as 0600 files in
  a 0700 directory. They pass a regex scrub and an LLM review before
  commit.
