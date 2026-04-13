# Bell Sound Assets

Each WAV in this directory is played by `internal/audio.Play()` when the user
selects the matching `BellSound` value in settings.

## Status

The files currently checked in (`bee.wav`, `chime.wav`, `ping.wav`,
`knock.wav`) are **44-byte silent PCM placeholders**. They exist so that
`//go:embed sounds/*.wav` compiles and the end-to-end flow (settings UI →
config → audio dispatcher → platform player) can be exercised.

**Before the next release**, each placeholder must be replaced with a real,
CC0-licensed short sound (≤50 KB, ≤1 s). Recommended sources:

- <https://freesound.org> — filter by CC0 license
- <https://kenney.nl/assets/category:Audio> — all CC0

## Attribution

Replace the rows below as you swap placeholders for real assets.

| File        | Source | Author | License |
|-------------|--------|--------|---------|
| `bee.wav`   | —      | —      | placeholder (silent) |
| `chime.wav` | —      | —      | placeholder (silent) |
| `ping.wav`  | —      | —      | placeholder (silent) |
| `knock.wav` | —      | —      | placeholder (silent) |

The `normal` option does **not** use a WAV — it writes `\a` to stdout so the
user's terminal emulator produces its configured bell. The `silent` option
produces no output at all.
