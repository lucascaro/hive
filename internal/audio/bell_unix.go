//go:build !windows

package audio

import (
	"fmt"
	"os/exec"
)

// unixPlayers is probed in order; the first one present on $PATH wins.
// afplay is the macOS built-in; paplay/aplay cover PulseAudio/ALSA on Linux.
var unixPlayers = []string{"afplay", "paplay", "aplay"}

func playWAVReal(path string) error {
	for _, p := range unixPlayers {
		if bin, err := exec.LookPath(p); err == nil {
			return exec.Command(bin, path).Run()
		}
	}
	return fmt.Errorf("audio: no supported player found (tried %v)", unixPlayers)
}
