// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// ClaudeStatePath is Claude Code's global state file, which records the
// folders whose workspace-trust dialog the user accepted. It also holds
// account data, so ClaudeFolderTrusted decodes only the trust flag.
func ClaudeStatePath(home string) string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, ".claude.json")
	}
	return filepath.Join(home, ".claude.json")
}

// ClaudeFolderTrusted reports whether the Claude state file at statePath
// marks cwd, or any ancestor of it, as trusted (projects[path].
// hasTrustDialogAccepted), which is the check Claude's own trust dialog
// makes. It answers false when it cannot tell: no file, unreadable, or
// not JSON. The file's schema is Claude's, not documented, and Claude
// has been inconsistent about ancestors, so a caller treats false as
// "not known to be trusted", never as "known to be untrusted".
func ClaudeFolderTrusted(statePath, cwd string) bool {
	b, err := os.ReadFile(statePath)
	if err != nil {
		return false
	}
	var st struct {
		Projects map[string]struct {
			Trusted bool `json:"hasTrustDialogAccepted"`
		} `json:"projects"`
	}
	if json.Unmarshal(b, &st) != nil {
		return false
	}
	var trusted []string
	for k, v := range st.Projects {
		if v.Trusted {
			trusted = append(trusted, k)
		}
	}
	for _, start := range trustStarts(cwd) {
		for dir := start; ; {
			for _, k := range trusted {
				if samePathOne(k, dir) {
					return true
				}
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return false
}

// trustStarts is cwd as given and with symlinks resolved, because Claude
// records whichever spelling it was launched in (macOS /tmp is
// /private/tmp).
func trustStarts(cwd string) []string {
	cwd = filepath.Clean(cwd)
	out := []string{cwd}
	if real, err := filepath.EvalSymlinks(cwd); err == nil && real != cwd {
		out = append(out, real)
	}
	return out
}

// trustKey normalizes a path for exact comparison: slashes forward, no
// trailing separator. It does not fold case; see samePathOne.
func trustKey(p string) string {
	return strings.TrimRight(filepath.ToSlash(filepath.Clean(p)), "/")
}

// samePathOne reports whether a and b are the same folder spelled the
// same way, or spelled with different case on a filesystem that treats
// the two as one. Case-insensitivity is a property of the volume, not of
// the OS (APFS and NTFS can each be either), so a case-only difference is
// settled by asking the filesystem, never by folding case: folding would
// trust "Repo" because "repo" was trusted, on a volume where they differ.
func samePathOne(a, b string) bool {
	ka, kb := trustKey(a), trustKey(b)
	if ka == kb {
		return true
	}
	if !strings.EqualFold(ka, kb) {
		return false
	}
	fa, errA := os.Stat(a)
	fb, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(fa, fb)
}

// SamePath reports whether a and b name the same folder, through
// symlinks and, where the volume is case-insensitive, case.
func SamePath(a, b string) bool {
	for _, x := range trustStarts(a) {
		for _, y := range trustStarts(b) {
			if samePathOne(x, y) {
				return true
			}
		}
	}
	return false
}
