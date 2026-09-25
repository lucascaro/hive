package registry

import (
	"context"
	"os"
	"slices"
	"testing"

	"github.com/lucascaro/hive/internal/session"

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
			env := r.resolveAgentEnv(tc.spec, r.spawnInfo())
			if got := slices.Contains(env, optIn); got != tc.want {
				t.Errorf("env = %v; contains %s = %v, want %v", env, optIn, got, tc.want)
			}
			// And the gate is shared: the opt-in is present exactly when
			// the hooks are.
			cmd := r.resolveAgentCmd(tc.spec, "sid-1", r.spawnInfo())
			if hooked := slices.Contains(cmd, "--settings"); hooked != slices.Contains(env, optIn) {
				t.Errorf("hooks=%v but opt-in=%v; argv = %v env = %v",
					hooked, !hooked, cmd, env)
			}
		})
	}
}

// TestEverySpawnPathCarriesHooksAndOptInTogether is the regression test
// for the Restart bug: create, restart and boot revive each spawn a
// Claude session, and each must carry the hook wiring AND the task-tool
// opt-in, or neither. Restart used to add the hooks and drop the opt-in,
// so a restarted session silently lost its plan.
func TestEverySpawnPathCarriesHooksAndOptInTogether(t *testing.T) {
	skipOnWindows(t)
	rec := captureStartSession(t)
	r := freshRegistry(t)
	r.SetHivedPath("/usr/local/bin/hived")
	agent.SetCustomDir(t.TempDir())
	t.Cleanup(func() { agent.SetCustomDir("") })
	unsetEnv(t, agent.ClaudeTaskToolsEnv)
	t.Cleanup(agent.SetClaudeVersionProbeForTest(func() ([]byte, error) {
		return []byte("2.1.273 (Claude Code)"), nil
	}))
	optIn := agent.ClaudeTaskToolsEnv + "=1"

	last := func() session.Options {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		if len(rec.opts) == 0 {
			t.Fatal("nothing was spawned")
		}
		return rec.opts[len(rec.opts)-1]
	}
	check := func(path string) {
		t.Helper()
		o := last()
		hooked := slices.Contains(o.Cmd, "--settings")
		opted := slices.Contains(o.Env, optIn)
		if !hooked || !opted {
			t.Errorf("%s: hooks=%v opt-in=%v, want both; cmd=%v env=%v", path, hooked, opted, o.Cmd, o.Env)
		}
	}

	e, err := r.Create(context.Background(), wire.CreateSpec{Name: "c", Agent: "claude", Shell: "/bin/bash"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	check("create")

	if err := r.Restart(e.ID); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	check("restart")
}

// TestEveryPiSpawnPathCarriesExtensionAndTodoSettingTogether: the Pi
// half of the test above. Create and restart each carry the extension
// (`-e`) AND the todo-tool setting, or neither, and the setting is read
// at spawn — a restart after switching it off must carry "=0".
func TestEveryPiSpawnPathCarriesExtensionAndTodoSettingTogether(t *testing.T) {
	skipOnWindows(t)
	rec := captureStartSession(t)
	r := freshRegistry(t)
	agent.SetCustomDir(t.TempDir())
	t.Cleanup(func() { agent.SetCustomDir("") })
	if err := agent.EnsurePiExtension(r.stateDir); err != nil {
		t.Fatal(err)
	}

	check := func(path, want string) {
		t.Helper()
		rec.mu.Lock()
		defer rec.mu.Unlock()
		if len(rec.opts) == 0 {
			t.Fatal("nothing was spawned")
		}
		o := rec.opts[len(rec.opts)-1]
		loaded := slices.Contains(o.Cmd, "-e")
		set := slices.Contains(o.Env, want)
		if !loaded || !set {
			t.Errorf("%s: extension=%v %s=%v, want both; cmd=%v env=%v", path, loaded, want, set, o.Cmd, o.Env)
		}
	}

	e, err := r.Create(context.Background(), wire.CreateSpec{Name: "p", Agent: "pi", Shell: "/bin/bash"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	check("create", agent.PiTodoToolEnv+"=1")

	if err := agent.SaveSettings(agent.Settings{ClaudeTaskTools: true, PiTodoTool: false}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := r.Restart(e.ID); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	check("restart", agent.PiTodoToolEnv+"=0")
}
