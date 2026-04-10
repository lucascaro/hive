//go:build windows

package muxnative

import "errors"

func clientAttach(_ *daemonClient, _ string, _ byte) error {
	return errors.New("native PTY backend is not supported on Windows")
}
