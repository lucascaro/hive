//go:build windows

package muxnative

import "errors"

func clientAttach(_ *daemonClient, _ string) error {
	return errors.New("native PTY backend is not supported on Windows")
}
