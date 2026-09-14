---
type: fixed
bump: minor
---

Custom agents whose command runs `claude` or `pi` now report their state like the
built-in agents do: working, waiting for you, finished turns and errors, instead of
guessing from terminal output. A wrapper script around them (for example a
`claude-lite` shell script) is not recognised and still guesses.

The command field in Settings ▸ Agents now keeps what you type, including a trailing
space, and no longer lets macOS turn `--` into an em dash.
