---
type: security
bump: patch
pr: 510
issue: 505
---

ACP Claude sessions no longer run a folder's project hooks, `.mcp.json` servers, `env` or `apiKeyHelper` unless the folder is a Hive project or you trusted it in Claude. In any other folder those do not run (your own user settings and user-level hooks still apply) and the transcript says so. Codex and Pi sessions in a folder that isn't a Hive project start with a warning, because their adapters offer no way to hold the folder's config back.
