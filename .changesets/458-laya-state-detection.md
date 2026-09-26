---
type: added
bump: minor
pr: 464
---

Optional Laya state detection: point Settings → Agents → Agent state detection at a Laya decision model you run locally, and sessions without agent hooks (Aider, Codex, shells, custom agents) — or hooked ones that have stopped reporting — get their state classified from the visible screen instead of guessed. Off by default; screen text only goes to the URL you set, and the API key comes from `HIVE_LAYA_API_KEY`.
