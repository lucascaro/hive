package registry

import (
	"os"
	"slices"
	"testing"

	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/wire"
)

// unsetEnv removes key for the test and restores it afterwards. The
// suite may itself run under a Claude session that set the variable,
// and "set to anything" is precisely what the opt-in must defer to.
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "") // registers the restore
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
}

// TestAgentEnvFollowsTheHookGate: the task-tool opt-in costs context in
// every session and pays off only through the hooks, so it must ride
// along exactly where the adapter's argv does — never on a session that
// runs an explicit command, and never on a different agent.
func TestAgentEnvFollowsTheHookGate(t *testing.T) {
	r := freshRegistry(t)
	r.SetHivedPath("/usr/local/bin/hived")
	// Pin a supported Claude so the hooks — and therefore the opt-in —
	// apply regardless of what is installed on the machine running this.
	t.Cleanup(agent.SetClaudeVersionProbeForTest(func() ([]byte, error) {
		return []byte("2.1.273 (Claude Code)"), nil
	}))
	agent.SetCustomDir(t.TempDir()) // no settings file: the defaults (on)
	t.Cleanup(func() { agent.SetCustomDir("") })
	unsetEnv(t, agent.ClaudeTaskToolsEnv)

	optIn := agent.ClaudeTaskToolsEnv + "=1"
	for _, tc := range []struct {
		name string
		spec wire.CreateSpec
		want bool
	}{
		{"claude gets it", wire.CreateSpec{Agent: "claude"}, true},
		{"explicit cmd gets no hooks, so no opt-in", wire.CreateSpec{Agent: "claude", Cmd: []string{"bash"}}, false},
		{"another agent never", wire.CreateSpec{Agent: "codex"}, false},
		{"a bare shell never", wire.CreateSpec{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := r.resolveAgentEnv(tc.spec)
			if got := slices.Contains(env, optIn); got != tc.want {
				t.Errorf("env = %v; contains %s = %v, want %v", env, optIn, got, tc.want)
			}
			// And the gate is shared: the opt-in is present exactly when
			// the hooks are.
			cmd := r.resolveAgentCmd(tc.spec, "sid-1")
			if hooked := slices.Contains(cmd, "--settings"); hooked != slices.Contains(env, optIn) {
				t.Errorf("hooks=%v but opt-in=%v; argv = %v env = %v",
					hooked, !hooked, cmd, env)
			}
		})
	}
}
