//go:build !windows

package proc

import (
	"bufio"
	"errors"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// KillTree must reach a grandchild in the leader's group, not only the
// leader — the adapter and plugin trees it guards are npx → node → CLI.
func TestKillTreeKillsTheWholeGroup(t *testing.T) {
	cmd := Command("/bin/sh", "-c", "sleep 60 & echo $!; wait")
	OwnGroup(cmd)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	child, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatalf("grandchild pid %q: %v", line, err)
	}
	if err := KillTree(cmd.Process); err != nil {
		t.Fatalf("KillTree: %v", err)
	}
	_ = cmd.Wait()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if errors.Is(syscall.Kill(child, 0), syscall.ESRCH) {
			break
		}
		if time.Now().After(deadline) {
			_ = syscall.Kill(child, syscall.SIGKILL)
			t.Fatalf("grandchild %d survived KillTree", child)
		}
	}
	if err := KillTree(cmd.Process); !errors.Is(err, os.ErrProcessDone) {
		t.Errorf("KillTree on a gone group = %v, want os.ErrProcessDone", err)
	}
}
