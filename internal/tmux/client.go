package tmux

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Exec runs a tmux command with the given arguments and returns stdout.
func Exec(args ...string) (string, error) {
	cmd := exec.Command("tmux", args...)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("tmux %s: %w — %s", strings.Join(args, " "), err, errBuf.String())
	}
	return strings.TrimRight(out.String(), "\n"), nil
}

// ExecSilent runs tmux and discards output, returning only errors.
func ExecSilent(args ...string) error {
	_, err := Exec(args...)
	return err
}

// IsAvailable checks whether tmux is installed and accessible.
func IsAvailable() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}

func IsServerRunning() bool {
	err := ExecSilent("info")
	return err == nil
}
