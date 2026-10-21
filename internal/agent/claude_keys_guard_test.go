// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package agent

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Hive must never read Claude Code's per-session key files
// (~/.claude/sessions/*.key): they are bearer credentials for Claude's
// cross-session inbox, and Hive's messaging (spec 338) deliberately does
// not use them. This test fails the build when a source file could be
// building such a path.
//
// It is per FILE rather than per literal, because Go builds the path as
// filepath.Join(home, ".claude", "sessions", id+".key") — three tokens
// that no single-literal pattern would ever see together.
var (
	claudeToken   = regexp.MustCompile(`["'` + "`" + `/]\.claude["'` + "`" + `/]`)
	sessionsToken = regexp.MustCompile(`["'` + "`" + `/]sessions["'` + "`" + `/]`)
	keyToken      = regexp.MustCompile(`\.key["'` + "`" + `]|["'` + "`" + `]\*\.key`)
)

func mentionsClaudeSessionKeys(src string) bool {
	return claudeToken.MatchString(src) && sessionsToken.MatchString(src) && keyToken.MatchString(src)
}

func TestClaudeSessionKeyGuardFires(t *testing.T) {
	for name, src := range map[string]string{
		"split Go literals": `p := filepath.Join(home, ".claude", "sessions", id+".key")`,
		"single literal":    `os.ReadFile(home + "/.claude/sessions/abc.key")`,
		"glob":              "glob := `.claude/sessions/*.key`",
		"ts path.join":      `path.join(os.homedir(), '.claude', 'sessions', name + '.key')`,
	} {
		if !mentionsClaudeSessionKeys(src) {
			t.Errorf("%s: guard did not fire on %q", name, src)
		}
	}
	for name, src := range map[string]string{
		"unrelated key property": `const k = obj.key; sessions.push(k)`,
		"claude sessions json":   `filepath.Join(home, ".claude", "sessions", pid+".json")`,
		"no claude":              `filepath.Join(dir, "sessions", id+".key")`,
	} {
		if mentionsClaudeSessionKeys(src) {
			t.Errorf("%s: guard fired on %q", name, src)
		}
	}
}

func TestNoClaudeSessionKeyReads(t *testing.T) {
	root := moduleRoot(t)
	var hits []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "dist", "wailsjs", ".worktrees", "graphify-out", "testdata":
				return fs.SkipDir
			}
			return nil
		}
		name := d.Name()
		isSrc := strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") ||
			(strings.HasSuffix(name, ".ts") || strings.HasSuffix(name, ".tsx")) && !strings.Contains(name, ".test.")
		if !isSrc {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if mentionsClaudeSessionKeys(string(b)) {
			rel, _ := filepath.Rel(root, path)
			hits = append(hits, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) > 0 {
		t.Fatalf("source builds a ~/.claude/sessions/*.key path (Hive never reads Claude's session keys): %v", hits)
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}
