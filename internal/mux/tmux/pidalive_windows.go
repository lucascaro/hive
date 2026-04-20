//go:build windows

package muxtmux

// pidAlive on Windows always reports "alive" — the tmux backend is reachable
// from Windows only through WSL (where tmux itself runs on the Linux side),
// and Go's os.Process.Signal on Windows does not support the signal-0 probe
// that pidAlive uses on Unix. Conservatively returning true means the orphan
// sweep never kills a grouped session we cannot confirm is dead; the cost
// is at most one leaked empty tmux session per crashed hive on Windows.
func pidAlive(pid int) bool { return true }
