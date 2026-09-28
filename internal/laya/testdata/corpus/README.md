# Laya screen corpus

Real terminal screens with their correct session state, used to score a
Laya server against Hive's classification question (spec 458).

```
<agent>/<state>/<name>.txt
```

- `agent`: `aider`, `codex`, `shell` or `pi`.
- `state`: `working`, `idle`, `waiting_input`, `waiting_permission` or `error`.
  The directory is the label. It uses Hive's meaning of each state: a
  plain shell back at its prompt after a failed command is `idle`, not
  `error`, because `error` is an agent reporting a failed turn.
- The file is the plain visible screen, as `VT.ScreenText` produces it.

`TestCorpusCoverage` requires at least one capture from each of `codex`,
`shell` and `pi`, and at least one in every state. `aider` captures are
welcome but not required. `TestCorpusHasNoSecrets` fails on anything that
still matches a secret pattern.

## Adding captures

Screens can contain anything a program printed, including credentials.
Nothing goes in without both review passes.

1. **Capture.** Start the daemon with `HIVE_LAYA_CAPTURE_DIR=<dir>` and
   Laya enabled in Settings. Every screen the daemon asks about is
   written there as `<agent>-<ts>-<answer>.txt`. The directory is 0700
   and the files are 0600. `<answer>` is whatever Laya said, or
   `unclassified` if the call failed. Treat it as a guess, not a label.
   Drive each agent into each state: a long silent tool run for Pi, a
   permission prompt, a failed command, a finished turn.
2. **Scrub.** Run `go run ./scripts/laya-scrub <dir>`. It redacts
   deterministic secret shapes (API keys, tokens, JWTs, private keys,
   emails, home directories) in place, and exits non-zero if anything
   still matches.
3. **Review.** Have an LLM read every scrubbed file for anything
   sensitive the patterns missed: hostnames, customer data, internal
   URLs, names. Run it in your own local agent session, never a
   shared or remote one. Delete or hand-redact what it flags.
4. **Label.** Move each file to `<agent>/<state>/`, correcting Laya's
   guess to the true state, and keep only the screen text.

## Scoring a server

```
HIVE_LAYA_URL=http://127.0.0.1:8000 go test ./internal/laya/ -run TestCorpusAccuracy -v
```

This prints the confusion matrix, overall accuracy and recall on the two
waiting states. The target is ≥90% overall and ≥95% on waiting. Meeting
it is a follow-up (a fine-tuned checkpoint), not a merge gate.
