---
type: fixed
bump: patch
---
- **macOS now explains why Hive asks for Local Network access.** Commands run in a session (agents, `curl`, SSH, MCP servers) reach your LAN, VMs and dev servers under Hive's permission; the prompt previously gave no reason, making it easy to deny and silently break those connections. If you denied it, re-enable Hive under System Settings → Privacy & Security → Local Network.
