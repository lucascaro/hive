package agent

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEncodeClaudeProjectDir(t *testing.T) {
	cases := []struct {
		name string
		cwd  string
		want string
	}{
		{
			name: "plain posix path",
			cwd:  "/Users/u/checkout/repo",
			want: "-Users-u-checkout-repo",
		},
		{
			name: "worktree dotted segment",
			cwd:  "/Users/u/checkout/hive/.worktrees/green-anchor",
			want: "-Users-u-checkout-hive--worktrees-green-anchor",
		},
		{
			name: "trailing slash is cleaned",
			cwd:  "/Users/u/checkout/repo/",
			want: "-Users-u-checkout-repo",
		},
		{
			name: "dotfile component",
			cwd:  "/home/u/.config/x",
			want: "-home-u--config-x",
		},
		{
			// Windows drive colon. filepath.Clean turns "/" into
			// "\" on Windows; both fold to "-",
			// so this row holds on every platform.
			name: "windows drive with forward slashes",
			cwd:  "C:/Users/u/repo",
			want: "C--Users-u-repo",
		},
		// Expected values below were produced by Claude's own encoder
		// (copied from the claude 2.1.288 bundle and run under node),
		// not by this package.
		{
			name: "underscore and space",
			cwd:  "/var/folders/x_y/T/my repo",
			want: "-var-folders-x-y-T-my-repo",
		},
		{
			name: "non-ASCII folds one dash per UTF-16 unit",
			cwd:  "/Users/u/café/日本",
			want: "-Users-u-caf----",
		},
		{
			name: "non-BMP rune is two UTF-16 units",
			cwd:  "/Users/u/😀x",
			want: "-Users-u---x",
		},
		{
			name: "exactly 200 is not truncated",
			cwd:  "/Users/u/" + strings.Repeat("a", 191),
			want: "-Users-u-" + strings.Repeat("a", 191),
		},
		// The truncation rows carry no path separator so filepath.Clean
		// leaves them byte-identical on every GOOS: the hash covers the
		// raw cwd, and Windows would otherwise hash a "\" form.
		{
			name: "over 200 truncates and appends positive hash",
			cwd:  strings.Repeat("very_long_segment", 13),
			want: strings.Repeat("very-long-segment", 11) + "very-long-seg-qqxzx5",
		},
		{
			name: "over 200 truncates and appends abs of negative hash",
			cwd:  strings.Repeat("x_", 101),
			want: strings.Repeat("x-", 100) + "-cuqrml",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := encodeClaudeProjectDir(tc.cwd)
			if got != tc.want {
				t.Errorf("encodeClaudeProjectDir(%q) = %q, want %q", tc.cwd, got, tc.want)
			}
		})
	}
}

func TestClaudeResumeArgsFallsBackWhenTranscriptMissing(t *testing.T) {
	t.Cleanup(SetClaudeSessionExistsForTest(func(_, _ string) bool { return false }))
	got := claudeResumeArgs("abc", "/some/cwd")
	want := []string{"claude", "--session-id", "abc"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestClaudeResumeArgsResumesWhenTranscriptExists(t *testing.T) {
	t.Cleanup(SetClaudeSessionExistsForTest(func(_, _ string) bool { return true }))
	got := claudeResumeArgs("abc", "/some/cwd")
	want := []string{"claude", "--resume", "abc"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestClaudeResumeArgsFindsTranscriptInUnderscoreCwd drives the real
// on-disk probe (no stub): a transcript Claude wrote for a cwd containing
// "_" must be found, or Restart falls back to --session-id and Claude
// refuses it as "already in use" (#494).
func TestClaudeResumeArgsFindsTranscriptInUnderscoreCwd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cwd := filepath.FromSlash("/work/my_repo")
	// The directory name Claude writes, spelled out rather than derived
	// from encodeClaudeProjectDir so the test cannot agree with a broken
	// encoder. On Windows the cleaned cwd is "\work\my_repo", which
	// folds to the same name.
	dir := filepath.Join(home, ".claude", "projects", "-work-my-repo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "abc.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := claudeResumeArgs("abc", cwd)
	want := []string{"claude", "--resume", "abc"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("claudeResumeArgs = %v, want %v", got, want)
	}
}

func TestSetClaudeSessionExistsForTestRejectsNil(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic when passing nil fn")
		}
	}()
	SetClaudeSessionExistsForTest(nil)
}

func TestClaudeSettingsJSONQuotesPath(t *testing.T) {
	t.Cleanup(SetClaudeVersionProbeForTest(func() ([]byte, error) {
		return []byte("2.1.260 (Claude Code)"), nil
	}))
	args := claudeSpawnArgs(SpawnInfo{HivedPath: "/Applications/Hive.app/Contents/Application Support/hived"})
	if len(args) != 2 || args[0] != "--settings" {
		t.Fatalf("args = %v, want [--settings <json>]", args)
	}
	var settings claudeSettings
	if err := json.Unmarshal([]byte(args[1]), &settings); err != nil {
		t.Fatalf("unmarshal settings: %v", err)
	}
	for _, ev := range claudeHookEvents {
		groups, ok := settings.Hooks[ev]
		if !ok || len(groups) != 1 || len(groups[0].Hooks) != 1 {
			t.Fatalf("hooks[%s] = %+v", ev, groups)
		}
		cmd := groups[0].Hooks[0].Command
		want := `'/Applications/Hive.app/Contents/Application Support/hived' hook`
		if cmd != want {
			t.Errorf("hooks[%s].command = %q, want %q", ev, cmd, want)
		}
		if groups[0].Hooks[0].Type != "command" {
			t.Errorf("hooks[%s].type = %q, want command", ev, groups[0].Hooks[0].Type)
		}
	}
}

func TestClaudeSpawnArgsNilWithoutHivedPath(t *testing.T) {
	t.Cleanup(SetClaudeVersionProbeForTest(func() ([]byte, error) {
		return []byte("2.1.260"), nil
	}))
	if args := claudeSpawnArgs(SpawnInfo{}); args != nil {
		t.Errorf("args = %v, want nil", args)
	}
}

func TestClaudeVersionGateSkipsBelowMin(t *testing.T) {
	t.Cleanup(SetClaudeVersionProbeForTest(func() ([]byte, error) {
		return []byte("1.9.0"), nil
	}))
	if args := claudeSpawnArgs(SpawnInfo{HivedPath: "/usr/local/bin/hived"}); args != nil {
		t.Errorf("args = %v, want nil (below minHooksVersion)", args)
	}
}

func TestClaudeVersionUnknownSkips(t *testing.T) {
	t.Cleanup(SetClaudeVersionProbeForTest(func() ([]byte, error) {
		return nil, errors.New("claude: command not found")
	}))
	if args := claudeSpawnArgs(SpawnInfo{HivedPath: "/usr/local/bin/hived"}); args != nil {
		t.Errorf("args = %v, want nil (unknown version)", args)
	}
}

func TestClaudeVersionAtOrAboveMinPasses(t *testing.T) {
	t.Cleanup(SetClaudeVersionProbeForTest(func() ([]byte, error) {
		return []byte("2.1.0"), nil
	}))
	if args := claudeSpawnArgs(SpawnInfo{HivedPath: "/usr/local/bin/hived"}); args == nil {
		t.Errorf("args = nil, want non-nil at minHooksVersion")
	}
}

func TestSemverLess(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"2.1.0", "2.1.0", false},
		{"2.1.0", "2.10.0", true},
		{"2.10.0", "2.1.0", false},
		{"1.9.0", "2.1.0", true},
		{"2.1.260", "2.1.0", false},
	}
	for _, tc := range cases {
		if got := semverLess(tc.a, tc.b); got != tc.want {
			t.Errorf("semverLess(%q,%q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

// TestClaudeSettingsRegistersSubagentHooks names the subagent hooks
// explicitly: the loop in TestClaudeSettingsJSONQuotesPath walks
// claudeHookEvents itself, so it would pass whatever the list holds.
func TestClaudeSettingsRegistersSubagentHooks(t *testing.T) {
	t.Cleanup(SetClaudeVersionProbeForTest(func() ([]byte, error) {
		return []byte("2.1.273 (Claude Code)"), nil
	}))
	args := claudeSpawnArgs(SpawnInfo{HivedPath: "/usr/local/bin/hived"})
	if len(args) != 2 {
		t.Fatalf("args = %v, want [--settings <json>]", args)
	}
	var settings claudeSettings
	if err := json.Unmarshal([]byte(args[1]), &settings); err != nil {
		t.Fatalf("unmarshal settings: %v", err)
	}
	for _, ev := range []string{"SubagentStart", "SubagentStop"} {
		if len(settings.Hooks[ev]) != 1 {
			t.Errorf("hooks[%s] not registered: %+v", ev, settings.Hooks[ev])
		}
	}
}
