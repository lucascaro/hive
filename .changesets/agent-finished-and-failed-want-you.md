---
type: fixed
bump: minor
---

A Claude or Pi session that finishes its turn now shows "Waiting for you" and pulses
in the sidebar, instead of dropping quietly to idle — the agent is done and ready for
you whether or not it asked a question. Switching to the session or typing into it
clears it, the same as any other wait.

A failed turn now stays visible too. A Claude API failure used to flip to error and
then fade back to idle the next time the screen redrew, and Pi never reported its
failures at all. Both now show the error glyph, raise a notification, and keep it
until you look.
