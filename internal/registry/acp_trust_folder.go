// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package registry

import (
	"os"

	"github.com/lucascaro/hive/internal/agent"
)

// Folder trust for ACP sessions (issue 505). Claude's PTY shows a
// workspace-trust dialog before it runs a folder's hooks, .mcp.json
// servers, env or apiKeyHelper; an ACP session never shows one, so the
// daemon decides instead. Trusted means Claude's own store says so, or
// Hive already knows the folder as a project it created or the user
// registered. Everything else is untrusted.

// acpTrustForTest, when set, decides folder trust in place of the real
// check, so the many tests that drive a fake adapter in a temp dir do not
// each have to register it as a project.
var acpTrustForTest func(cwd string) bool

// acpFolderTrusted reports whether e may run cwd's project-scoped agent
// config. claudeStore additionally consults Claude's state file, which is
// meaningful only for Claude. Re-evaluated at every start, never cached:
// a dialog the user accepts later, or a project removed, applies on the
// next start. Must not be called with r.mu held.
func (r *Registry) acpFolderTrusted(e *Entry, cwd string, claudeStore bool) bool {
	if acpTrustForTest != nil {
		return acpTrustForTest(cwd)
	}
	if claudeStore {
		if home, err := os.UserHomeDir(); err == nil && agent.ClaudeFolderTrusted(agent.ClaudeStatePath(home), cwd) {
			return true
		}
	}
	return r.hiveKnowsFolder(e, cwd)
}

// hiveKnowsFolder is true when cwd is exactly a project's folder, or the
// worktree Hive made for e from its own project. Exact matches only: the
// default project is rooted at the daemon's cwd, often $HOME, and an
// ancestor match would trust every clone beneath it.
func (r *Registry) hiveKnowsFolder(e *Entry, cwd string) bool {
	home, _ := os.UserHomeDir()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.projects {
		if p.Cwd == "" || (home != "" && agent.SamePath(p.Cwd, home)) || isFSRoot(p.Cwd) {
			continue
		}
		if agent.SamePath(p.Cwd, cwd) {
			return true
		}
	}
	if _, ok := r.projects[e.ProjectID]; ok && e.WorktreePath != "" {
		return agent.SamePath(e.WorktreePath, cwd)
	}
	return false
}

func isFSRoot(p string) bool {
	return p == "/" || (len(p) == 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/'))
}
