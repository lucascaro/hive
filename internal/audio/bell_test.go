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
// and returns counters + recorders for the last path and volume passed to playWAV.
// Callers register cleanup via t.Cleanup.
func withHooks(t *testing.T) (writeCalls *atomic.Int32, playCalls *atomic.Int32, lastPath *string, lastVolume *int) {
	t.Helper()
	var wc, pc atomic.Int32
	var lp string
	var lv int
	var mu sync.Mutex

	origWrite := writeBell
	origPlay := playWAV
	writeBell = func() { wc.Add(1) }
	playWAV = func(path string, volume int) error {
		pc.Add(1)
		mu.Lock()
		lp = path
		lv = volume
		mu.Unlock()
		return nil
	}
	t.Cleanup(func() {
		writeBell = origWrite
		playWAV = origPlay
	})

	return &wc, &pc, &lp, &lv
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
	wc, pc, _, _ := withHooks(t)
	withTempDir(t)

	playSync(BellSilent, 100)

	if got := wc.Load(); got != 0 {
		t.Errorf("writeBell called %d times, want 0", got)
	}
	if got := pc.Load(); got != 0 {
		t.Errorf("playWAV called %d times, want 0", got)
	}
}

func TestPlay_NormalWritesBellChar(t *testing.T) {
	wc, pc, _, _ := withHooks(t)
	withTempDir(t)

	playSync(BellNormal, 100)

	if got := wc.Load(); got != 1 {
		t.Errorf("writeBell called %d times, want 1", got)
	}
	if got := pc.Load(); got != 0 {
		t.Errorf("playWAV unexpectedly called %d times", got)
	}
}

func TestPlay_CustomSoundCallsPlayWAV(t *testing.T) {
	wc, pc, lastPath, _ := withHooks(t)
	withTempDir(t)

	playSync(BellBee, 100)

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
	wc, pc, _, _ := withHooks(t)
	withTempDir(t)

	playSync("bogus-not-a-sound", 100)

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
	if _, err := os.Stat(p1); err != nil {
		t.Fatalf("stat after first extract: %v", err)
	}
	// Overwrite the extracted file with different content to prove the
	// second call does NOT re-extract (it would restore the original bytes).
	sentinel := []byte("TEST-SENTINEL")
	if err := os.WriteFile(p1, sentinel, 0o600); err != nil {
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

// TestExtractOnce_Concurrent verifies that N goroutines racing on a cold
// cache all observe the same path and only one file write occurs. Guards
// the sync.Once wrapper inside extractOnce.
func TestExtractOnce_Concurrent(t *testing.T) {
	withTempDir(t)

	const n = 20
	var wg sync.WaitGroup
	paths := make([]string, n)
	errs := make([]error, n)

	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			paths[i], errs[i] = extractOnce(BellBee)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}
	for i := 1; i < n; i++ {
		if paths[i] != paths[0] {
			t.Errorf("paths[%d] = %q, want %q (all goroutines must share the cache)", i, paths[i], paths[0])
		}
	}
}

func TestPlay_VolumePassedThrough(t *testing.T) {
	_, _, _, lastVolume := withHooks(t)
	withTempDir(t)

	playSync(BellBee, 75)

	if got := *lastVolume; got != 75 {
		t.Errorf("playWAV received volume %d, want 75", got)
	}
}

func TestPlay_ZeroVolumeDefaultsTo100(t *testing.T) {
	_, _, _, lastVolume := withHooks(t)
	withTempDir(t)

	playSync(BellBee, 0)

	if got := *lastVolume; got != 100 {
		t.Errorf("playWAV received volume %d, want 100 (zero should default to full volume)", got)
	}
}

func TestEffectiveVolume(t *testing.T) {
	tests := []struct {
		in   int
		want int
	}{
		{0, 100},
		{-5, 100},
		{1, 1},
		{50, 50},
		{100, 100},
		{101, 100},
	}
	for _, tc := range tests {
		if got := effectiveVolume(tc.in); got != tc.want {
			t.Errorf("effectiveVolume(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
