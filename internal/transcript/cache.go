package transcript

import (
	"io"
	"os"
	"sync"
)

// Cache holds the projection of exactly one session's transcript.
//
// One, not many, because cross-session search is a non-goal: the user
// searches the session in front of them. Holding a single projection
// bounds daemon memory at one transcript's worth of text without any
// eviction policy to get wrong.
//
// Re-projecting a 20 MB file on every keystroke is not viable, so a
// grown file re-parses only from the last consumed byte offset. That
// also delivers "text that arrives while the box is open becomes
// findable" as a property rather than a feature: every search re-reads
// the tail.
type Cache struct {
	mu sync.Mutex

	key   string // session id the projection belongs to
	paths []string
	sizes []int64 // bytes consumed per path, parallel to paths
	lines []Line
}

// Lines returns the projection for sessionID, refreshing it from disk.
//
// Switching sessions drops the previous projection entirely — that is
// the memory bound. Refreshing re-reads only what each file grew by,
// except when a file shrank, which forces a full re-parse: a rewritten
// or rotated file invalidates every offset, and seeking past its new
// EOF would silently return nothing.
func (c *Cache) Lines(sessionID string, paths []string) ([]Line, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if sessionID != c.key || !samePaths(paths, c.paths) {
		c.key = sessionID
		c.paths = append([]string(nil), paths...)
		c.sizes = make([]int64, len(paths))
		c.lines = nil
	}

	for i, p := range c.paths {
		st, err := os.Stat(p)
		if err != nil {
			return c.lines, err
		}
		switch {
		case st.Size() == c.sizes[i]:
			continue // unchanged; nothing to re-read
		case st.Size() < c.sizes[i]:
			// Shrank: every offset is meaningless now.
			return c.reprojectLocked()
		}
		// A file that is not the last one having grown means lines
		// would need to be inserted mid-list; indices after it would
		// all shift. Rare enough (an agent writing to an older file)
		// that a full re-parse is the honest answer.
		if i != len(c.paths)-1 {
			return c.reprojectLocked()
		}
		n, err := c.appendTailLocked(p, c.sizes[i])
		if err != nil {
			return c.lines, err
		}
		c.sizes[i] += n
	}
	return c.lines, nil
}

// appendTailLocked projects the bytes of p after off, returning how
// many bytes it consumed. The count excludes a trailing partial record
// so the next refresh re-reads it once it is complete.
func (c *Cache) appendTailLocked(p string, off int64) (int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return 0, err
	}
	lines, n, err := projectFile(io.LimitReader(f, MaxFileBytes), c.lines)
	if err != nil {
		return 0, err
	}
	c.lines = lines
	return n, nil
}

func (c *Cache) reprojectLocked() ([]Line, error) {
	c.lines = nil
	for i := range c.sizes {
		c.sizes[i] = 0
	}
	for i, p := range c.paths {
		n, err := c.appendTailLocked(p, 0)
		if err != nil {
			return c.lines, err
		}
		c.sizes[i] = n
	}
	return c.lines, nil
}

// Drop releases the cached projection. Called when the find box closes
// so an idle daemon is not holding a session's transcript in memory.
func (c *Cache) Drop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.key, c.paths, c.sizes, c.lines = "", nil, nil, nil
}

func samePaths(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
