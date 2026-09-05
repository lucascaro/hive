package agent

import (
	"encoding/json"
	"errors"
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
			// Pre-normalized Windows-style input (already
			// ToSlash'd) — covers the drive-colon branch in a
			// platform-independent way. filepath.Clean's
			// backslash handling differs between GOOS=windows
			// and POSIX, so we feed the encoder slash form
			// directly to keep the assertion deterministic.
			name: "windows drive with forward slashes",
			cwd:  "C:/Users/u/repo",
			want: "C--Users-u-repo",
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
