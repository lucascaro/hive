---
issue: null
pr: null
type: changed
bump: minor
---
- **New look by default.** Hive now follows your system light/dark setting out
  of the box instead of starting on the v2.4 pure-black theme. The v2.4
  appearance is still there as Settings > Appearance > Classic.
- Three more presets: Native Dark, Native Light and Terminal, each with its own
  terminal colour palette. Every preset — and every ANSI colour on a light
  background — is checked against WCAG AA contrast in CI, so no theme ships
  with text you cannot read.
- IBM Plex Sans and JetBrains Mono are bundled with the app, so the Hive themes
  look the same on macOS, Windows and Linux instead of falling back to whatever
  the machine happens to have. The terminal font follows the theme too.
