// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build darwin

package main

import (
	"errors"
	"os"
	"path/filepath"
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

// handyCtl starts and stops Handy. Toggle and Cancel may block for a
// while (they exec), so the HUD never calls them under its lock or on
// the main thread; Available must be cheap, because Open calls it on
// both.
type handyCtl interface {
	// Available reports that Handy is installed and running. Hive never
	// launches Handy: --toggle-transcription on a stopped Handy would
	// start it, which is not what opening the HUD asked for.
	Available() bool
	Toggle() error
	Cancel() error
}

// handyBundleID is Handy's macOS bundle identifier.
const handyBundleID = "com.pais.handy"

// handyTimeout bounds one Handy command. They answer at once when Handy
// is healthy; the bound is for when it is not.
const handyTimeout = 3 * time.Second

// handyPaths are where Handy's binary lives in a standard install.
func handyPaths() []string {
	paths := []string{"/Applications/Handy.app/Contents/MacOS/handy"}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, "Applications", "Handy.app", "Contents", "MacOS", "handy"))
	}
	return paths
}

// execHandy is the production handyCtl.
type execHandy struct {
	run     runner
	paths   func() []string
	running func() bool
}

func newExecHandy() *execHandy {
	return &execHandy{run: runWithTimeout, paths: handyPaths, running: nativeHandyRunning}
}

func (h *execHandy) bin() string {
	for _, p := range h.paths() {
		if isExecutable(p) {
			return p
		}
	}
	return ""
}

func (h *execHandy) Available() bool {
	return h.bin() != "" && h.running()
}

func (h *execHandy) Toggle() error { return h.do("--toggle-transcription") }
func (h *execHandy) Cancel() error { return h.do("--cancel") }

func (h *execHandy) do(flag string) error {
	bin := h.bin()
	if bin == "" {
		return os.ErrNotExist
	}
	// Handy may have quit since the HUD opened, and either flag on a
	// stopped Handy launches it.
	if !h.running() {
		return errors.New("handy is not running")
	}
	_, err := h.run(handyTimeout, bin, flag)
	return err
}
