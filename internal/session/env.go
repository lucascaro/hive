package session

import "strings"

// sessionEnv builds the environment a spawned session runs with, from
// base — the daemon's own environment.
//
// TERM is forced because a tile is an xterm.js terminal whatever the
// daemon itself was started under. NO_COLOR is dropped for the same
// reason: it reaches hived only by inheritance from whichever shell
// happened to launch the GUI, it says nothing about the tile, and while
// it is set every agent and shell Hive spawns renders monochrome.
func sessionEnv(base []string) []string {
	env := make([]string, 0, len(base)+1)
	for _, kv := range base {
		if strings.HasPrefix(kv, "NO_COLOR=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "TERM=xterm-256color")
}
