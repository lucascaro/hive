# Bundled fonts

Wails has no CDN; the brand presets need these files inside the binary.

| File | Family / weight | Version | Licence | Source |
|---|---|---|---|---|
| IBMPlexSans-Regular.woff2 | IBM Plex Sans 400 | 6.4.0 | OFL 1.1 (see Licences) | github.com/IBM/plex releases |
| IBMPlexSans-Medium.woff2 | IBM Plex Sans 500 | 6.4.0 | " | " |
| IBMPlexSans-SemiBold.woff2 | IBM Plex Sans 600 | 6.4.0 | " | " |
| JetBrainsMono-Regular.woff2 | JetBrains Mono 400 | 2.304 | OFL 1.1 (see Licences) | github.com/JetBrains/JetBrainsMono releases |
| JetBrainsMono-Medium.woff2 | JetBrains Mono 500 | 2.304 | " | " |
| JetBrainsMono-Bold.woff2 | JetBrains Mono 700 | 2.304 | " | " |

Files are unmodified upstream releases (no subsetting — the OFL Reserved Font
Name rules make a renamed subset more paperwork than the ~480KB it would save).

## Licences

Both families are SIL OFL 1.1. The licence permits bundling and selling the
fonts *with software* (only selling the font files by themselves is forbidden),
and requires no attribution in the app UI — but it does require the notice to
travel with the redistributed font files.

So the two texts live in `../../../public/fonts/`, NOT beside the woff2 here:
Vite copies `public/**` into `dist/` verbatim, and `//go:embed all:frontend/dist`
then carries them into the binary. Anything under `src/` only reaches `dist/` if
something imports it, and nothing imports a licence — which is exactly how a
licence file gets left out of a shipped build.

`cmd/hivegui/assets_test.go` asserts both are embedded. Do not "tidy" them back
next to the fonts.

The other OFL obligation is the Reserved Font Names ("IBM Plex", "JetBrains
Mono"): the files are unmodified and unrenamed, so it is met by doing nothing.
Subsetting or renaming would make them Modified Versions and the names would
have to change.

## Provenance

Exact archives these came from, with the digests taken at download time:

| Archive | sha256 |
|---|---|
| https://github.com/IBM/plex/releases/download/v6.4.0/IBM-Plex-Sans.zip | `e9cac220220f66e8c3b903320a1ebd0e0bcfbfd216772879d33cc71de94570ab` |
| https://github.com/JetBrains/JetBrainsMono/releases/download/v2.304/JetBrainsMono-2.304.zip | `6f6376c6ed2960ea8a963cd7387ec9d76e3f629125bc33d1fdcd7eb7012f7bbf` |

Paths inside the archives: `IBM-Plex-Sans/fonts/complete/woff2/` (and that
directory's own `license.txt`, committed as
`public/fonts/LICENSE-IBMPlexSans.txt`); `fonts/webfonts/` and `OFL.txt` for
JetBrains Mono (committed as `public/fonts/LICENSE-JetBrainsMono.txt`).

The v6.4.0 release has no `WOFF2.zip` asset — the per-family `IBM-Plex-Sans.zip`
is the one that carries the complete (unsplit) woff2 files. The `split/woff2/`
directory in the same archive holds `unicode-range`-partitioned subsets; we take
`complete/` because the app renders arbitrary user text (project names, window
titles) in any script.
