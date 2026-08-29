//go:build !darwin

package main

import "fmt"

// Self-update applies on macOS only. Windows would need a detached
// helper process to replace a running .exe, and Linux ships no release
// artifact (README documents a native build). Both platforms keep the
// banner's Download button, which opens the release page — so this is a
// missing convenience, not a missing update path.
//
// errUnsupported is returned rather than silently doing nothing: the
// frontend shows it, which is how the user learns to use the link.
var errUnsupported = fmt.Errorf("in-app update is macOS-only — use the Download button to update manually")

func stageUpdate(UpdateInfo, func(string)) (string, error) { return "", errUnsupported }

func applyStagedBundle(string) error { return errUnsupported }
