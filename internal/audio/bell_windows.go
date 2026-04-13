//go:build windows

package audio

import (
	"os/exec"
	"strings"
)

func playWAVReal(path string) error {
	// Escape single quotes by doubling them — this is PowerShell's
	// literal-string escape. Defense-in-depth: today `path` always comes
	// from os.TempDir() + a constant basename, but escaping keeps us
	// safe if TEMP ever resolves somewhere containing a quote.
	esc := strings.ReplaceAll(path, "'", "''")
	script := "(New-Object System.Media.SoundPlayer '" + esc + "').PlaySync()"
	return exec.Command("powershell", "-NoProfile", "-Command", script).Run()
}
