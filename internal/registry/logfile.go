package registry

import (
	"os"
	"path/filepath"
)

// maxLogBytes caps a log file before it's rotated. The GUI/daemon logs
// are diagnostic breadcrumbs, not an audit trail — an 8 MiB window is
// tens of thousands of lines, plenty to catch a freeze, while bounding
// disk use. (A prior unbounded log reached 54 MiB.)
const maxLogBytes = 8 << 20

// OpenLogFile opens name under the state dir for appending, rotating it
// first if it already exceeds maxLogBytes. Rotation is a single-slot
// rename to name+".1" (overwriting any previous .1), so at most ~2x the
// cap is ever on disk. Best-effort: on any error the caller gets a nil
// file and should fall back to stderr-only logging.
//
// ponytail: single-slot size rotation, no compression or dated archives.
// Add generations only if one prior window ever proves too little.
func OpenLogFile(name string) (*os.File, error) {
	dir := StateDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, name)
	if fi, err := os.Stat(path); err == nil && fi.Size() >= maxLogBytes {
		// Rotate. Ignore rename errors — worst case we keep appending to
		// the oversized file, which is still better than losing logging.
		_ = os.Rename(path, path+".1")
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}
