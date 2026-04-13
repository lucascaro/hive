//go:build windows

package audio

import "os/exec"

func playWAVReal(path string) error {
	script := "(New-Object System.Media.SoundPlayer '" + path + "').PlaySync()"
	return exec.Command("powershell", "-NoProfile", "-Command", script).Run()
}
