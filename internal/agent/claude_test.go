package agent

import "testing"

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
