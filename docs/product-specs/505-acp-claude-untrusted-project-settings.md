---
issue: 505
title: "ACP Claude sessions load untrusted project hooks and MCP servers without a trust prompt"
type: bug
complexity: M
priority: P1
stage: DONE
pr: 510
shipped: 2026-10-11
---

# ACP Claude sessions load untrusted project hooks and MCP servers without a trust prompt

- **Issue:** #505
- **Exec plan:** [docs/exec-plans/completed/505-acp-claude-untrusted-project-settings.md](../exec-plans/completed/505-acp-claude-untrusted-project-settings.md)

## Problem

ACP sessions launch Claude through `@agentclientprotocol/claude-agent-acp`, whose default is `settingSources: ["user","project","local"]`; Hive passes no override. A headless/SDK run shows neither Claude's workspace-trust dialog nor the `.mcp.json` approval prompt, so a repo the user never trusted gets its `.claude` hooks, `env`, `apiKeyHelper` and `.mcp.json` servers run silently. The same repo as a PTY session shows the trust dialog first. Workflow nodes (#495) will start ACP sessions in fresh worktrees, widening the exposure.

## Desired behavior

An ACP Claude session never runs project-scoped hooks or MCP servers from a folder the user has not trusted.

## Success criteria

- A test with a fake project holding a hook and a `.mcp.json` server shows neither runs in an ACP Claude session in an untrusted folder.
- Codex and Pi behaviour for a never-trusted path is verified and documented in `docs/design-docs/acp-session-kind.md`.

## Non-goals

- Reimplementing Claude's trust dialog inside Hive.
- Changing PTY sessions.

## Notes

Source: GitHub issue #505, found during #495 research.
