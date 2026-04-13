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

func playWAVReal(path string, volume int) error {
	for _, p := range unixPlayers {
		if bin, err := lookPath(p); err == nil {
			args := volumeArgs(p, path, volume)
			return runCmd(bin, args...)
		}
	}
	return fmt.Errorf("audio: no supported player found (tried %v)", unixPlayers)
}

// volumeArgs builds the argument list for the given player binary, path, and
// volume percentage (1–100). afplay supports -v <0.0–1.0>; paplay supports
// --volume=<0–65536> where 65536 is 100%; aplay has no volume flag.
func volumeArgs(player, path string, volume int) []string {
	switch player {
	case "afplay":
		v := fmt.Sprintf("%.4f", float64(volume)/100.0)
		return []string{"-v", v, path}
	case "paplay":
		v := fmt.Sprintf("%d", 65536*volume/100)
		return []string{"--volume=" + v, path}
	default:
		return []string{path}
	}
}
