package hooks

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"github.com/lucascaro/hive/internal/state"
)

const hookTimeout = 5 * time.Second

// Run executes all hooks registered for the given event.
// Errors are returned but are non-fatal — callers should log them.
func Run(hooksDir string, event state.HookEvent) []error {
	scripts := findScripts(hooksDir, event.Name)
	if len(scripts) == 0 {
		return nil
	}
	env := BuildEnv(event)
	var errs []error
	for _, script := range scripts {
		if err := runScript(script, env); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// findScripts returns all executable hook scripts for the given event name.
// Checks both a flat file (on-{event}) and a .d/ directory (on-{event}.d/).
func findScripts(hooksDir, eventName string) []string {
	var scripts []string
	flat := filepath.Join(hooksDir, "on-"+eventName)
	if isExecutable(flat) {
		scripts = append(scripts, flat)
	}
	dotD := flat + ".d"
	entries, err := os.ReadDir(dotD)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			p := filepath.Join(dotD, e.Name())
			if isExecutable(p) {
				scripts = append(scripts, p)
			}
		}
		sort.Strings(scripts[len(scripts)-len(entries):]) // sort .d/ entries
	}
	return scripts
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir() && info.Mode()&0o111 != 0
}

func runScript(path string, extraEnv []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), hookTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path)
	cmd.Env = append(os.Environ(), extraEnv...)
	return cmd.Run()
}
