package proc_test

import (
	"context"
	"os/exec"
	"reflect"
	"runtime"
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

// HideConsole must be safe on a *exec.Cmd built elsewhere - including
// one that already carries SysProcAttr - and a second call must leave
// exactly what the first one left. Which flags it sets is the
// Windows-only TestHideConsolePreservesExistingFlags; this is that
// applying it twice is applying it once, on every platform.
//
// Off Windows it must be wholly inert: SysProcAttr stays nil, which is
// what keeps every converted call site building the same exec.Cmd it
// built before the switch.
func TestHideConsoleIsIdempotent(t *testing.T) {
	cmd := exec.Command("git", "status") // built elsewhere, not by proc.Command

	// Snapshot the value, not the pointer: HideConsole mutates in place,
	// so keeping the pointer would compare a struct against itself.
	snapshot := func() any {
		if cmd.SysProcAttr == nil {
			return nil
		}
		v := *cmd.SysProcAttr
		return v
	}

	proc.HideConsole(cmd)
	once := snapshot()
	proc.HideConsole(cmd)
	twice := snapshot()

	if !reflect.DeepEqual(once, twice) {
		t.Errorf("a second HideConsole changed SysProcAttr:\n once:  %+v\n twice: %+v", once, twice)
	}
	if runtime.GOOS != "windows" && cmd.SysProcAttr != nil {
		t.Errorf("SysProcAttr = %+v off Windows, want nil: HideConsole must stay inert there", cmd.SysProcAttr)
	}
}
