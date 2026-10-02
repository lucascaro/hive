---
type: added
bump: minor
issue: 477
pr: 490
---

Settings → Shortcuts can export your shortcuts to a file and import one. An import first shows a preview: you choose whether it replaces your shortcuts or adds to them, every key it skips says why (unknown command, reserved or unreadable key), and each conflict asks you to reassign the key or skip it. Nothing changes until you confirm, and confirming says so. Escape cancels an open import wherever focus is in Settings, rather than closing Settings. A `null` entry in a hand-edited keymap.json now costs only that entry instead of resetting every shortcut (an entry of the wrong type still stops the file loading, so saving can't overwrite your edit), and the Shortcuts and Editor sections say why they can't be edited when their file could not be read.
