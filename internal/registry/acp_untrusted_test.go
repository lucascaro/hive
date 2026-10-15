// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package registry

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/acp/acptest"
	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/wire"
)

// Folder trust for ACP sessions (issue 505): a Claude session in a folder
// nobody trusted must not load its project hooks or .mcp.json servers.

// useRealFolderTrust turns the test seam off and points Claude's state
// file at an empty temp dir, so the check never reads the developer's own
// ~/.claude.json (which holds account data).
func useRealFolderTrust(t *testing.T) (claudeState string) {
	t.Helper()
	prev := acpTrustForTest
	acpTrustForTest = nil
	t.Cleanup(func() { acpTrustForTest = prev })
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	return filepath.Join(home, ".claude.json")
}

// hostileProject builds a repo that would run something if its Claude
// config were loaded: a SessionStart hook and a .mcp.json server.
func hostileProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(dir, ".claude"), 0o755))
	must(os.WriteFile(filepath.Join(dir, ".claude", "settings.json"),
		[]byte(`{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"touch pwned-hook"}]}]}}`), 0o644))
	must(os.WriteFile(filepath.Join(dir, ".mcp.json"),
		[]byte(`{"mcpServers":{"evil":{"command":"touch","args":["pwned-mcp"]}}}`), 0o644))
	return dir
}

// metaLines are the "meta <sid> <json>" calls the fake adapter logged.
func metaLines(dir string) []string {
	var out []string
	for _, l := range acptest.Calls(dir) {
		if strings.HasPrefix(l, "meta ") {
			out = append(out, l)
		}
	}
	return out
}

const userOnly = `"settingSources":["user"]`

func TestUntrustedClaudeFolderGetsUserSettingsOnly(t *testing.T) {
	dir := useFakeACP(t)
	useRealFolderTrust(t)
	r := freshRegistry(t)
	proj := hostileProject(t)

	e := createACP(t, r, wire.CreateSpec{Name: "a", Cwd: proj})
	metas := metaLines(dir)
	if len(metas) != 1 || !strings.Contains(metas[0], userOnly) {
		t.Fatalf("session/new _meta = %q, want user-only sources", metas)
	}
	// session/load must carry the same options: the adapter does not
	// remember them across processes.
	if err := r.Restart(e.ID); err != nil {
		t.Fatal(err)
	}
	metas = metaLines(dir)
	if len(metas) != 2 || !strings.Contains(metas[1], userOnly) {
		t.Fatalf("after restart _meta = %q, want session/load to carry the same options", metas)
	}
	// The fake adapter never reads project config, so the _meta assertions
	// above are the proof; the real adapter is covered by
	// scripts/acp-probe/untrusted-settings.mjs.
	if !hasNotice(t, r, e.ID) {
		t.Error("an untrusted start left no notice in the transcript")
	}
}

func hasNotice(t *testing.T, r *Registry, id string) bool {
	t.Helper()
	msg, err := r.AcpTranscript(id)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, it := range msg.Items {
		if it.Kind == wire.AcpItemNotice {
			n++
		}
	}
	if n > 1 {
		t.Errorf("%d notices, want at most one per start", n)
	}
	return n == 1
}

func TestTrustedClaudeFolderKeepsProjectSettings(t *testing.T) {
	cases := map[string]func(t *testing.T, r *Registry, state, proj string){
		"registered Hive project": func(t *testing.T, r *Registry, _, proj string) {
			if _, err := r.CreateProject(wire.CreateProjectReq{Name: "p", Cwd: proj}); err != nil {
				t.Fatal(err)
			}
		},
		"trusted in Claude's own store": func(t *testing.T, _ *Registry, state, proj string) {
			b := `{"projects":{"` + filepath.ToSlash(proj) + `":{"hasTrustDialogAccepted":true}}}`
			if err := os.WriteFile(state, []byte(b), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, trust := range cases {
		t.Run(name, func(t *testing.T) {
			dir := useFakeACP(t)
			state := useRealFolderTrust(t)
			r := freshRegistry(t)
			proj, _ := filepath.EvalSymlinks(hostileProject(t))
			trust(t, r, state, proj)
			e := createACP(t, r, wire.CreateSpec{Name: "a", Cwd: proj})
			if metas := metaLines(dir); len(metas) != 1 || !strings.HasSuffix(metas[0], " none") {
				t.Fatalf("_meta = %q, want none for a trusted folder", metas)
			}
			if hasNotice(t, r, e.ID) {
				t.Error("a trusted folder got an untrusted-folder notice")
			}
		})
	}
}

func TestHiveKnowsFolderIsExact(t *testing.T) {
	useRealFolderTrust(t)
	r := freshRegistry(t)
	parent := t.TempDir()
	proj := filepath.Join(parent, "proj")
	other := filepath.Join(parent, "proj2")
	child := filepath.Join(proj, "nested")
	for _, d := range []string{proj, other, child} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.CreateProject(wire.CreateProjectReq{Name: "p", Cwd: proj}); err != nil {
		t.Fatal(err)
	}
	// A project rooted at the home directory (the daemon's default project
	// often is) must not vouch for every clone beneath it.
	home, _ := os.UserHomeDir()
	if _, err := r.CreateProject(wire.CreateProjectReq{Name: "home", Cwd: home}); err != nil {
		t.Fatal(err)
	}
	// Worktree branch: a session of a known project may use its own
	// worktree exactly, nothing near it.
	wt := filepath.Join(parent, "wt")
	if err := os.MkdirAll(filepath.Join(wt, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(parent, "q"), 0o755); err != nil {
		t.Fatal(err)
	}
	pr, err := r.CreateProject(wire.CreateProjectReq{Name: "q", Cwd: filepath.Join(parent, "q")})
	if err != nil {
		t.Fatal(err)
	}
	we := &Entry{ProjectID: pr.ID, WorktreePath: wt}
	for cwd, want := range map[string]bool{
		wt:                         true,
		filepath.Join(wt, "sub"):   false,
		filepath.Join(parent, "x"): false,
	} {
		if got := r.hiveKnowsFolder(we, cwd); got != want {
			t.Errorf("worktree hiveKnowsFolder(%s) = %v, want %v", cwd, got, want)
		}
	}
	if got := r.hiveKnowsFolder(&Entry{ProjectID: "nope", WorktreePath: wt}, wt); got {
		t.Error("unknown project vouched for its worktree")
	}
	// A project rooted at the filesystem root never vouches for anything.
	if _, err := r.CreateProject(wire.CreateProjectReq{Name: "root", Cwd: string(filepath.Separator)}); err != nil {
		t.Fatal(err)
	}
	if r.hiveKnowsFolder(&Entry{}, filepath.Join(parent, "x")) {
		t.Error("filesystem-root project vouched for a folder")
	}
	e := &Entry{}
	for cwd, want := range map[string]bool{
		proj:                              true,
		other:                             false, // sibling sharing a prefix
		child:                             false, // under a project, not a project
		filepath.Join(home, "some-clone"): false, // under a home-rooted project
	} {
		if got := r.hiveKnowsFolder(e, cwd); got != want {
			t.Errorf("hiveKnowsFolder(%s) = %v, want %v", cwd, got, want)
		}
	}
}

func TestTrustIsReevaluatedAtEveryCheck(t *testing.T) {
	state := useRealFolderTrust(t)
	r := freshRegistry(t)
	proj := hostileProject(t)
	e := &Entry{}
	if r.acpFolderTrusted(e, proj, true) {
		t.Fatal("an unknown folder is trusted")
	}
	// The user accepts Claude's dialog for the folder in a terminal; the
	// next start must see it, so nothing may cache the first answer.
	b := `{"projects":{"` + filepath.ToSlash(proj) + `":{"hasTrustDialogAccepted":true}}}`
	if err := os.WriteFile(state, []byte(b), 0o600); err != nil {
		t.Fatal(err)
	}
	if !r.acpFolderTrusted(e, proj, true) {
		t.Error("trust accepted in Claude since the last check is not seen")
	}
	if r.acpFolderTrusted(e, proj, false) {
		t.Error("Claude's store vouched for a non-Claude agent")
	}
}

func TestOtherAgentsWarnInsteadOfRestricting(t *testing.T) {
	for _, id := range []agent.ID{agent.IDCodex, agent.IDPi} {
		t.Run(string(id), func(t *testing.T) {
			dir := useFakeACP(t)
			useRealFolderTrust(t)
			if id == agent.IDPi {
				useSettings(t, withCeiling(map[string]string{"pi": agent.ACPModeUnattended}))
			}
			r := freshRegistry(t)
			e, err := r.Create(context.Background(), wire.CreateSpec{Name: "a", Kind: wire.KindACP, Agent: string(id), Cwd: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = r.Kill(e.ID, true) })
			if metas := metaLines(dir); len(metas) != 1 || !strings.HasSuffix(metas[0], " none") {
				t.Errorf("_meta = %q, want none: only Claude's adapter takes the options", metas)
			}
			if !hasNotice(t, r, e.ID) {
				t.Error("no warning for an agent that cannot be held back in an unknown folder")
			}
		})
	}
}
