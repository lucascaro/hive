---
type: fixed
bump: patch
issue: 494
---

Restarting or reviving a Claude session now resumes its conversation when the session's folder name contains `_`, a space, or other punctuation, instead of failing with "Session ID … is already in use". Transcript search finds those sessions' history too.
