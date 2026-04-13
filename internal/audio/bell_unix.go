//go:build !windows

package audio

import (
	"fmt"
	"os/exec"
)

// unixPlayers is probed in order; the first one present on $PATH wins.
// afplay is the macOS built-in; paplay/aplay cover PulseAudio/ALSA on Linux.
var unixPlayers = []string{"afplay", "paplay", "aplay"}

// lookPath and runCmd are indirected so tests can verify probe order
// without requiring any real audio tool on the host.
var (
	lookPath = exec.LookPath
	runCmd   = func(bin string, args ...string) error { return exec.Command(bin, args...).Run() }
)

func playWAVReal(path string) error {
	for _, p := range unixPlayers {
		if bin, err := lookPath(p); err == nil {
			return runCmd(bin, path)
		}
	}
	return fmt.Errorf("audio: no supported player found (tried %v)", unixPlayers)
}
