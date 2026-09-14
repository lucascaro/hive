package main

import (
	"context"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/lucascaro/hive/internal/proc"
)

// logLaunch records who launched this process and how.
// Called right after setupLogFile so it lands even if everything after it
// fails to start — the whole point of this diagnostic is to have SOMETHING
// on disk for the launch that led to the next mystery quit/relaunch.
//
// The launch line is synchronous; the parent's command name follows on its
// own line from a goroutine, because resolving it shells out to `ps` and a
// stalled ps must not hold up the window (it has a 1s budget).
func logLaunch() {
	ppid := os.Getppid()
	log.Printf("hivegui: launch pid=%d ppid=%d exe=%q args=%q env={%s}",
		os.Getpid(), ppid, exePath(), os.Args, launchEnvSummary(os.Getenv))
	go func() {
		log.Printf("hivegui: launch parent ppid=%d comm=%q", ppid, parentCommand(ppid))
	}()
}

// exePath is best-effort; a failure logs as "" rather than aborting the
// whole launch line over a cosmetic field.
func exePath() string {
	p, err := os.Executable()
	if err != nil {
		return ""
	}
	return p
}

// parentCommand resolves the parent process's command name, best effort.
// Note: a LaunchServices-started app (Finder, Dock, `open`) normally has
// ppid 1 (launchd), not the app that asked for it to open — that's
// expected and still worth logging, since a NON-1 ppid is the signal that
// something spawned hivegui directly (a shell, a script, another Hive
// process).
func parentCommand(ppid int) string {
	if runtime.GOOS == "windows" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := proc.CommandContext(ctx, "ps", "-o", "comm=", "-p", strconv.Itoa(ppid)).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// launchEnvVars is the fixed allowlist logged by launchEnvSummary.
//
// SECURITY: the full environment can hold tokens (shell auth, CI
// secrets) that a user pastes a log line to us in a bug report — so
// this stays a hand-picked list of variables that explain a launch
// rather than a dump. HIVE_SESSION_ID matters most: macOS `open` hands
// the caller's environment to the launched app, so a GUI opened from a
// command run *inside* a Hive session (an agent shelling out to `open
// -a Hive`, say) inherits that session's HIVE_SESSION_ID even though it
// is a brand new, unrelated GUI window — see registry.go sessionEnv.
var launchEnvVars = []string{
	"HIVE_SESSION_ID",
	"HIVE_SOCKET",
	"HIVE_LAUNCH_DIR",
	"HIVE_STATE_DIR",
	"TERM_PROGRAM",
	"__CFBundleIdentifier",
	"XPC_SERVICE_NAME",
	"SHLVL",
	"_",
}

// launchEnvSummary renders the allowlisted launch env as "KEY=value"
// pairs, space separated, skipping any that are unset — never the full
// environment. See launchEnvVars for why the list is fixed.
func launchEnvSummary(getenv func(string) string) string {
	var parts []string
	for _, k := range launchEnvVars {
		if v := getenv(k); v != "" {
			parts = append(parts, k+"="+v)
		}
	}
	return strings.Join(parts, " ")
}
