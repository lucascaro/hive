# Bundled fonts

Wails has no CDN; the brand presets need these files inside the binary.

| File | Family / weight | Version | Licence | Source |
|---|---|---|---|---|
| IBMPlexSans-Regular.woff2 | IBM Plex Sans 400 | 6.4.0 | OFL 1.1 (LICENSE-IBMPlexSans.txt) | github.com/IBM/plex releases |
| IBMPlexSans-Medium.woff2 | IBM Plex Sans 500 | 6.4.0 | " | " |
| IBMPlexSans-SemiBold.woff2 | IBM Plex Sans 600 | 6.4.0 | " | " |
| JetBrainsMono-Regular.woff2 | JetBrains Mono 400 | 2.304 | OFL 1.1 (LICENSE-JetBrainsMono.txt) | github.com/JetBrains/JetBrainsMono releases |
| JetBrainsMono-Medium.woff2 | JetBrains Mono 500 | 2.304 | " | " |
| JetBrainsMono-Bold.woff2 | JetBrains Mono 700 | 2.304 | " | " |

Files are unmodified upstream releases (no subsetting — the OFL Reserved Font
Name rules make a renamed subset more paperwork than the ~480KB it would save).

## Provenance

Exact archives these came from, with the digests taken at download time:

| Archive | sha256 |
|---|---|
| https://github.com/IBM/plex/releases/download/v6.4.0/IBM-Plex-Sans.zip | `e9cac220220f66e8c3b903320a1ebd0e0bcfbfd216772879d33cc71de94570ab` |
| https://github.com/JetBrains/JetBrainsMono/releases/download/v2.304/JetBrainsMono-2.304.zip | `6f6376c6ed2960ea8a963cd7387ec9d76e3f629125bc33d1fdcd7eb7012f7bbf` |

Paths inside the archives: `IBM-Plex-Sans/fonts/complete/woff2/` (and that
directory's own `license.txt`, committed here as `LICENSE-IBMPlexSans.txt`);
`fonts/webfonts/` and `OFL.txt` for JetBrains Mono.

The v6.4.0 release has no `WOFF2.zip` asset — the per-family `IBM-Plex-Sans.zip`
is the one that carries the complete (unsplit) woff2 files. The `split/woff2/`
directory in the same archive holds `unicode-range`-partitioned subsets; we take
`complete/` because the app renders arbitrary user text (project names, window
titles) in any script.
