//go:build !windows

package main

import "path/filepath"

// resolveDir returns the path behind dir with every symlink followed.
// Unix has no junctions; EvalSymlinks is the whole story here.
func resolveDir(dir string) (string, error) {
	return filepath.EvalSymlinks(dir)
}
