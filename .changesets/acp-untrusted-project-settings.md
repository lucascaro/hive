---
type: security
bump: patch
pr: 510
issue: 505
---

ACP Claude sessions no longer run a folder's project hooks, `.mcp.json` servers, `env` or `apiKeyHelper` unless the folder is a Hive project or you trusted it in Claude. In any other folder the session loads only your own Claude user settings (your user-level hooks still run) and says so in its transcript. Codex and Pi sessions in a folder that isn't a Hive project start with a warning, because their adapters offer no way to hold the folder's config back.
