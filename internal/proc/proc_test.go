package proc_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/proc"
)

// The wrappers must be drop-in: same Path and Args os/exec would
// produce, so a call site keeps behaving identically once it switches.
func TestCommandBuildsTheSameCmd(t *testing.T) {
	cmd := proc.Command("git", "-C", "/repo", "status")
	if !strings.Contains(cmd.Path, "git") {
		t.Errorf("Path = %q, want it to resolve git", cmd.Path)
	}
	want := []string{"git", "-C", "/repo", "status"}
	if len(cmd.Args) != len(want) {
		t.Fatalf("Args = %v, want %v", cmd.Args, want)
	}
	for i := range want {
		if cmd.Args[i] != want[i] {
			t.Errorf("Args[%d] = %q, want %q", i, cmd.Args[i], want[i])
		}
	}
}

func TestCommandContextCarriesTheContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd := proc.CommandContext(ctx, "git", "status")
	if err := cmd.Start(); err == nil {
		_ = cmd.Wait()
		t.Error("Start() on a cancelled context succeeded; context not wired through")
	}
}

// HideConsole must be safe on a *exec.Cmd built elsewhere, including
// one that already carries SysProcAttr.
func TestHideConsoleIsIdempotent(t *testing.T) {
	cmd := proc.Command("git", "status")
	proc.HideConsole(cmd)
	proc.HideConsole(cmd)
}
