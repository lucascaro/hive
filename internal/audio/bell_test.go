package audio

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// withHooks installs deterministic test doubles for writeBell and playWAV
// and returns counters + a recorder of the last path passed to playWAV.
// Callers register cleanup via t.Cleanup.
func withHooks(t *testing.T) (writeCalls *atomic.Int32, playCalls *atomic.Int32, lastPath *string) {
	t.Helper()
	var wc, pc atomic.Int32
	var lp string
	var mu sync.Mutex

	origWrite := writeBell
	origPlay := playWAV
	writeBell = func() { wc.Add(1) }
	playWAV = func(path string) error {
		pc.Add(1)
		mu.Lock()
		lp = path
		mu.Unlock()
		return nil
	}
	t.Cleanup(func() {
		writeBell = origWrite
		playWAV = origPlay
	})

	return &wc, &pc, &lp
}

// withTempDir redirects the lazy extractOnce target to t.TempDir and resets
// the cache so each test observes a fresh filesystem.
func withTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	orig := tempDir
	tempDir = func() string { return dir }
	extractCache = sync.Map{}
	t.Cleanup(func() {
		tempDir = orig
		extractCache = sync.Map{}
	})
	return dir
}

func TestPlay_SilentIsNoop(t *testing.T) {
	wc, pc, _ := withHooks(t)
	withTempDir(t)

	playSync(BellSilent)

	if got := wc.Load(); got != 0 {
		t.Errorf("writeBell called %d times, want 0", got)
	}
	if got := pc.Load(); got != 0 {
		t.Errorf("playWAV called %d times, want 0", got)
	}
}

func TestPlay_NormalWritesBellChar(t *testing.T) {
	wc, pc, _ := withHooks(t)
	withTempDir(t)

	playSync(BellNormal)

	if got := wc.Load(); got != 1 {
		t.Errorf("writeBell called %d times, want 1", got)
	}
	if got := pc.Load(); got != 0 {
		t.Errorf("playWAV unexpectedly called %d times", got)
	}
}

func TestPlay_CustomSoundCallsPlayWAV(t *testing.T) {
	wc, pc, lastPath := withHooks(t)
	withTempDir(t)

	playSync(BellBee)

	if got := pc.Load(); got != 1 {
		t.Fatalf("playWAV called %d times, want 1", got)
	}
	if got := wc.Load(); got != 0 {
		t.Errorf("writeBell unexpectedly called %d times on a successful custom play", got)
	}
	if base := filepath.Base(*lastPath); base != "hive-bell-bee.wav" {
		t.Errorf("playWAV path basename = %q, want %q", base, "hive-bell-bee.wav")
	}
	if _, err := os.Stat(*lastPath); err != nil {
		t.Errorf("extracted file missing: %v", err)
	}
}

func TestPlay_UnknownSoundFallsBackToBell(t *testing.T) {
	wc, pc, _ := withHooks(t)
	withTempDir(t)

	playSync("bogus-not-a-sound")

	if got := pc.Load(); got != 0 {
		t.Errorf("playWAV called %d times, want 0 on unknown sound", got)
	}
	if got := wc.Load(); got != 1 {
		t.Errorf("writeBell called %d times, want 1 (fallback)", got)
	}
}

func TestExtractOnce_CachesPath(t *testing.T) {
	dir := withTempDir(t)

	p1, err := extractOnce(BellBee)
	if err != nil {
		t.Fatalf("first extractOnce: %v", err)
	}
	info1, err := os.Stat(p1)
	if err != nil {
		t.Fatalf("stat after first extract: %v", err)
	}
	// Overwrite the extracted file with different content to prove the
	// second call does NOT re-extract (it would restore the original bytes).
	sentinel := []byte("TEST-SENTINEL")
	if err := os.WriteFile(p1, sentinel, 0o644); err != nil {
		t.Fatalf("overwrite extracted file: %v", err)
	}

	p2, err := extractOnce(BellBee)
	if err != nil {
		t.Fatalf("second extractOnce: %v", err)
	}
	if p1 != p2 {
		t.Errorf("extractOnce returned different paths: %q vs %q", p1, p2)
	}
	got, err := os.ReadFile(p2)
	if err != nil {
		t.Fatalf("read after second extract: %v", err)
	}
	if string(got) != string(sentinel) {
		t.Error("extractOnce re-wrote the file on a cached call; cache is not working")
	}
	if !strings.HasPrefix(p1, dir) {
		t.Errorf("extracted path %q is not under test temp dir %q", p1, dir)
	}
	_ = info1 // reference to quiet linters
}

func TestBellsConstantCoversAllOptions(t *testing.T) {
	want := map[string]bool{
		BellNormal: true, BellBee: true, BellChime: true,
		BellPing: true, BellKnock: true, BellSilent: true,
	}
	if len(Bells) != len(want) {
		t.Fatalf("Bells has %d entries, want %d", len(Bells), len(want))
	}
	for _, b := range Bells {
		if !want[b] {
			t.Errorf("unexpected entry in Bells: %q", b)
		}
	}
	if Bells[0] != BellNormal {
		t.Errorf("Bells[0] = %q, want %q (default must be first)", Bells[0], BellNormal)
	}
}

func TestExtractOnce_UnknownSoundReturnsError(t *testing.T) {
	withTempDir(t)
	if _, err := extractOnce("does-not-exist"); err == nil {
		t.Error("extractOnce returned nil error for unknown sound")
	}
}
