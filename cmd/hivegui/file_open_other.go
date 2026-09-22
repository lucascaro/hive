//go:build !darwin && !linux && !windows

package main

import (
	"errors"
	"os"
)

// Hive ships macOS, Linux and Windows builds. The remaining unix
// targets still have to compile (GOOS=freebsd go build is a cheap
// smoke test), so opening a file there fails with a clear error
// rather than silently doing nothing.

var errUnsupportedPlatform = errors.New("opening files is not supported on this platform")

func openDefault(string) error { return errUnsupportedPlatform }

func reveal(string) error { return errUnsupportedPlatform }

func guardPath(path string) string { return path }

func statMeta(path string) (fileMeta, error) {
	info, err := os.Stat(path)
	if err != nil {
		return fileMeta{}, err
	}
	return fileMeta{
		isDir:   info.IsDir(),
		execBit: !info.IsDir() && info.Mode().Perm()&0o111 != 0,
	}, nil
}
