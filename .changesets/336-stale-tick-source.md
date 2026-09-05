---
issue: null
pr: 344
type: fixed
bump: patch
---
- A session whose agent stopped reporting no longer claims the agent
  said it was idle. When Hive falls back to guessing because an agent's
  hooks went quiet, the state icon's tooltip now says "guessed from
  terminal output" instead of continuing to credit the agent for a
  state it never sent.
