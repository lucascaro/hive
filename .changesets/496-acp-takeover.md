---
type: added
bump: minor
issue: 496
---

An ACP session can now be taken over in a terminal and handed back. **Take Over in Terminal** (File menu or command palette) reopens the same conversation in the agent's own CLI, and **Hand Back to ACP** returns it to the transcript view with every turn, including the ones you typed in the terminal. Hive refuses a takeover it cannot do safely — before the agent has saved the conversation, or mid-turn — and when Codex still holds the conversation open, a hand-back says so and leaves the session in its terminal.
