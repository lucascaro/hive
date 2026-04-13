// Package audio provides the terminal-bell dispatcher used by the TUI when a
// background session rings its bell. Playback is asynchronous and never
// blocks the render loop; unknown or unplayable sounds fall back to writing
// \a so the user always gets some indication.
package audio

import (
	"embed"
	"os"
	"path/filepath"
	"sync"
)

// Sound identifiers persisted in Config.BellSound.
const (
	BellNormal = "normal"
	BellBee    = "bee"
	BellChime  = "chime"
	BellPing   = "ping"
	BellKnock  = "knock"
	BellSilent = "silent"
)

// Bells is the ordered list surfaced in the settings UI.
var Bells = []string{BellNormal, BellBee, BellChime, BellPing, BellKnock, BellSilent}

//go:embed sounds/*.wav
var soundsFS embed.FS

// Package-level hooks let tests intercept playback without touching real
// audio devices. Tests may swap these and restore them with t.Cleanup, or
// use SetTestHooks from outside this package.
//
// These are plain globals, not guarded by a mutex. Tests that touch them
// must NOT use t.Parallel() — the current test suite relies on sequential
// execution within the audio package and within cross-package consumers.
var (
	playWAV   = playWAVReal
	writeBell = writeBellReal
)

// SyncForTest, when true, causes Play to run synchronously instead of
// spawning a goroutine. Tests set this to make playback assertions
// deterministic. Production code must leave it false. Like the hooks
// above, this global is not guarded — tests that flip it must not use
// t.Parallel().
var SyncForTest bool

// Play dispatches the configured bell sound in a goroutine so callers
// (the TUI render loop) never block on exec or file IO. Unknown sound
// names and playback errors fall back to writing \a.
func Play(sound string) {
	if SyncForTest {
		playSync(sound)
		return
	}
	go playSync(sound)
}

// SetTestHooks replaces the internal playback and bell-write callbacks with
// the provided functions and returns a restore closure. Either argument may
// be nil to leave that hook untouched. Intended for cross-package tests;
// production code must not call this.
func SetTestHooks(onBell func(), onPlayWAV func(string) error) (restore func()) {
	ow, op := writeBell, playWAV
	if onBell != nil {
		writeBell = onBell
	}
	if onPlayWAV != nil {
		playWAV = onPlayWAV
	}
	return func() {
		writeBell = ow
		playWAV = op
	}
}

// playSync runs the dispatch synchronously. Exposed for tests that need
// deterministic ordering; Play wraps this in a goroutine.
func playSync(sound string) {
	switch sound {
	case BellSilent:
		return
	case BellNormal:
		writeBell()
		return
	}
	path, err := extractOnce(sound)
	if err != nil {
		writeBell()
		return
	}
	if err := playWAV(path); err != nil {
		writeBell()
	}
}

func writeBellReal() {
	_, _ = os.Stdout.Write([]byte("\a"))
}

// cachedExtract memoizes a single sound's extract result so concurrent
// callers share one read+write and see the same error/path.
type cachedExtract struct {
	once sync.Once
	path string
	err  error
}

// extractCache maps sound name → *cachedExtract. Populated lazily by
// extractOnce; survives for the lifetime of the process.
var extractCache sync.Map

// extractOnce copies the embedded WAV for the given sound to the OS temp
// directory and returns the path. Subsequent calls return the cached path
// without touching the filesystem. Concurrent first-callers share the same
// extract (via sync.Once) so the file is read+written at most once per
// sound per process. Returns an error for unknown sounds or write failures.
func extractOnce(sound string) (string, error) {
	v, _ := extractCache.LoadOrStore(sound, &cachedExtract{})
	ce := v.(*cachedExtract)
	ce.once.Do(func() {
		data, err := soundsFS.ReadFile("sounds/" + sound + ".wav")
		if err != nil {
			ce.err = err
			return
		}
		path := filepath.Join(tempDir(), "hive-bell-"+sound+".wav")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			ce.err = err
			return
		}
		ce.path = path
	})
	return ce.path, ce.err
}

// tempDir is indirected so tests can redirect the extract target.
var tempDir = os.TempDir
