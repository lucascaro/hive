// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package dictation

import (
	"errors"
	"os"
	"path"
	"strings"
	"time"
)

// Handy (https://handy.computer) is a local speech-to-text app. With the
// "Start Handy recording" setting on (spec 573), opening the dictation
// HUD starts it recording, so one hotkey press is enough to dictate.
//
// Handy is driven through its own command line, which forwards to the
// running instance: --toggle-transcription starts or stops a recording,
// --cancel drops one, and on an idle Handy --cancel does nothing. There
// is no way to ask whether it is recording, which is why the HUD tracks
// only the recordings it started itself. Verified on Handy 0.10.1.

// HandyCtl starts and stops Handy. Toggle and Cancel may block for a
// while (they exec), so the HUD never calls them under its lock or on
// the main thread; Available must be cheap, because Open calls it on
// both.
type HandyCtl interface {
	// Available reports that Handy is installed and running. Hive never
	// launches Handy: --toggle-transcription on a stopped Handy would
	// start it, which is not what opening the HUD asked for.
	Available() bool
	Toggle() error
	Cancel() error
}

// errHandyNotRunning: Handy quit after the HUD checked. Neither flag is
// run then, because either would launch it, and there is no recording to
// cancel.
var errHandyNotRunning = errors.New("handy is not running")

// HandyBundleID is Handy's macOS bundle identifier, for a host's
// "is it running" check.
const HandyBundleID = "com.pais.handy"

// handyTimeout bounds one Handy command. They answer at once when Handy
// is healthy (about a second on Windows); the bound is for when it is
// not.
const handyTimeout = 3 * time.Second

// darwinHandyPaths are where Handy's binary lives in a standard macOS
// install. path, not filepath: these are macOS paths on every GOOS the
// tests run on.
func darwinHandyPaths(home string) []string {
	paths := []string{"/Applications/Handy.app/Contents/MacOS/handy"}
	if home != "" {
		paths = append(paths, path.Join(home, "Applications", "Handy.app", "Contents", "MacOS", "handy"))
	}
	return paths
}

// windowsHandyPaths are where Handy's installers put handy.exe on
// Windows (checked with 0.10.1): the setup .exe installs per user under
// %LOCALAPPDATA%\Handy, the .msi under %ProgramFiles%\Handy. Joined with
// a backslash rather than filepath.Join so the list is the same, and
// testable, on every OS.
func windowsHandyPaths(getenv func(string) string) []string {
	var paths []string
	for _, root := range []string{getenv("LOCALAPPDATA"), getenv("ProgramFiles")} {
		if root != "" {
			paths = append(paths, strings.TrimRight(root, `\`)+`\Handy\handy.exe`)
		}
	}
	return paths
}

// HandyBin is the installed Handy binary, or "" when there is none.
func HandyBin() string {
	for _, p := range handyPaths() {
		if isExecutable(p) {
			return p
		}
	}
	return ""
}

// ExecHandy is the production HandyCtl.
type ExecHandy struct {
	run     Runner
	bin     func() string
	running func() bool
}

// NewExecHandy drives the installed Handy. running reports that Handy
// is up; a host passes its own check (hivebar asks AppKit), or nil for
// this platform's default.
func NewExecHandy(running func() bool) *ExecHandy {
	if running == nil {
		running = handyRunning
	}
	return &ExecHandy{run: RunWithTimeout, bin: HandyBin, running: running}
}

func (h *ExecHandy) Available() bool {
	return h.bin() != "" && h.running()
}

func (h *ExecHandy) Toggle() error { return h.do("--toggle-transcription") }
func (h *ExecHandy) Cancel() error { return h.do("--cancel") }

func (h *ExecHandy) do(flag string) error {
	bin := h.bin()
	if bin == "" {
		return os.ErrNotExist
	}
	// Handy may have quit since the HUD opened, and either flag on a
	// stopped Handy launches it.
	if !h.running() {
		return errHandyNotRunning
	}
	_, err := h.run(handyTimeout, bin, flag)
	return err
}
