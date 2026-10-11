package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeState(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), ".claude.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestClaudeFolderTrusted(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	sub := filepath.Join(repo, ".worktrees", "wt")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	state := func(path string, trusted bool) string {
		b := `{"oauthAccount":{"emailAddress":"x"},"projects":{` + quote(path) + `:{"hasTrustDialogAccepted":` + map[bool]string{true: "true", false: "false"}[trusted] + `}}}`
		return writeState(t, b)
	}
	cases := []struct {
		name string
		path string
		want bool
	}{
		{"exact", state(repo, true), true},
		{"ancestor", state(repo, true), true},
		{"declined", state(repo, false), false},
		{"other folder", state(filepath.Join(filepath.Dir(repo), "elsewhere"), true), false},
		{"sibling prefix", state(repo+"2", true), false},
		{"missing file", filepath.Join(t.TempDir(), "nope.json"), false},
		{"malformed", writeState(t, "{not json"), false},
		{"no projects key", writeState(t, `{"a":1}`), false},
	}
	for _, c := range cases {
		cwd := sub // the ancestor case; "exact" asks about repo itself
		if c.name == "exact" {
			cwd = repo
		}
		if got := ClaudeFolderTrusted(c.path, cwd); got != c.want {
			t.Errorf("%s: ClaudeFolderTrusted = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestClaudeFolderTrustedThroughSymlink(t *testing.T) {
	real := filepath.Join(t.TempDir(), "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("no symlinks:", err)
	}
	resolved, _ := filepath.EvalSymlinks(real)
	p := writeState(t, `{"projects":{`+quote(resolved)+`:{"hasTrustDialogAccepted":true}}}`)
	if !ClaudeFolderTrusted(p, link) {
		t.Error("a symlinked spelling of a trusted folder is not trusted")
	}
}

func TestSamePath(t *testing.T) {
	d := t.TempDir()
	if !SamePath(d, d+string(filepath.Separator)) {
		t.Error("trailing separator broke SamePath")
	}
	if SamePath(d, d+"2") {
		t.Error("prefix sibling reported as the same path")
	}
}

func quote(s string) string {
	b, _ := json.Marshal(filepath.ToSlash(s))
	return string(b)
}
