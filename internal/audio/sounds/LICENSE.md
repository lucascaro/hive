# Bell Sound Assets

Each WAV in this directory is played by `internal/audio.Play()` when the user
selects the matching `BellSound` value in settings.

All four files are royalty-free and cleared for commercial redistribution.
Normalized to 16-bit PCM, mono, 22050 Hz for consistent playback across
platforms and minimal binary footprint.

## Attribution

| File        | Source                                                                 | License                      | Notes                                                                 |
|-------------|------------------------------------------------------------------------|------------------------------|-----------------------------------------------------------------------|
| `bee.wav`   | [Hummel_bee.ogg, Wikimedia Commons](https://commons.wikimedia.org/wiki/File:Hummel_bee.ogg) (via [PDsounds.org](https://www.pdsounds.org/)) | Public Domain                | Trimmed to 0.9 s; 5 ms fade-in, 150 ms fade-out; +6 dB boost.          |
| `chime.wav` | [Kenney Interface Sounds](https://kenney.nl/assets/interface-sounds), `glass_001.ogg`                                  | CC0 1.0                      | Transcoded OGG → WAV, unchanged content.                               |
| `ping.wav`  | [Kenney Interface Sounds](https://kenney.nl/assets/interface-sounds), `confirmation_001.ogg`                           | CC0 1.0                      | Transcoded OGG → WAV, unchanged content.                               |
| `knock.wav` | [Knocking_on_wood_or_door.ogg, Wikimedia Commons](https://commons.wikimedia.org/wiki/File:Knocking_on_wood_or_door.ogg) (via [PDsounds.org](https://www.pdsounds.org/)) | Public Domain                | Silence trimmed from start, first 0.8 s kept.                          |

The `normal` option does **not** use a WAV — it writes `\a` to stdout so the
user's terminal emulator produces its configured bell. The `silent` option
produces no output at all.

## Replacing or adding sounds

To swap a sound, drop a new short WAV (≤50 KB, ≤1 s, mono, 22050 Hz, 16-bit
PCM) under the same filename and update the row above. Add new entries to
`internal/audio/bell.go` (`Bells` slice + constant) and they'll appear in
Settings automatically.

Normalization command used for every file:

```
ffmpeg -i <input> -t 0.9 -ac 1 -ar 22050 -sample_fmt s16 <name>.wav
```
