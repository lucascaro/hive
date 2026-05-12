package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func swapCodexSessionsDir(t *testing.T, dir string) {
	t.Helper()
	prev := codexSessionsDir
	codexSessionsDir = dir
	t.Cleanup(func() { codexSessionsDir = prev })
}

func swapCodexPollInterval(t *testing.T, d time.Duration) {
	t.Helper()
	prev := codexCapturePollInterval
	codexCapturePollInterval = d
	t.Cleanup(func() { codexCapturePollInterval = prev })
}

// writeRollout writes a fake codex rollout file with the given cwd and
// returns the full path. The filename UUID is taken from the wantUUID
// argument so tests assert against a known id.
//
// Writes to a staging path inside the same directory, applies Chtimes,
// then renames into place so the file only becomes visible to a
// concurrent watcher after mtime is set. Without this, the capture
// goroutine could observe the file between WriteFile and Chtimes,
// return early, end the test, and trigger t.TempDir cleanup — leaving
// the still-running writer's Chtimes to fail with ENOENT and Fatalf
// after the test had already completed.
func writeRollout(t *testing.T, root, day, cwd, uuid string, modTime time.Time) string {
	t.Helper()
	dir := filepath.Join(root, day)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "rollout-2026-05-08T12-00-00-"+uuid+".jsonl")
	staging := path + ".tmp"
	body := `{"timestamp":"2026-05-08T12:00:00.000Z","type":"session_meta","payload":{"id":"` + uuid + `","cwd":"` + cwd + `","timestamp":"2026-05-08T12:00:00.000Z"}}` + "\n"
	if err := os.WriteFile(staging, []byte(body), 0o644); err != nil {
		t.Fatalf("write rollout: %v", err)
	}
	if err := os.Chtimes(staging, modTime, modTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	if err := os.Rename(staging, path); err != nil {
		t.Fatalf("rename rollout: %v", err)
	}
	return path
}

func TestCodexCaptureReadsRolloutUUID(t *testing.T) {
	root := t.TempDir()
	swapCodexSessionsDir(t, root)
	swapCodexPollInterval(t, 20*time.Millisecond)

	cwd := "/tmp/proj-a"
	wantUUID := "019d4d18-0b7d-7751-89ca-8a4386362f54"

	spawnedAt := time.Now()
	// Write the rollout slightly in the future to simulate codex
	// creating the file just after spawn.
	go func() {
		time.Sleep(40 * time.Millisecond)
		writeRollout(t, root, "2026/05/08", cwd, wantUUID, time.Now())
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	got, err := codexCaptureSessionID(ctx, cwd, spawnedAt)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if got != wantUUID {
		t.Errorf("got uuid %q, want %q", got, wantUUID)
	}
}

func TestCodexCaptureSkipsMismatchedCwd(t *testing.T) {
	root := t.TempDir()
	swapCodexSessionsDir(t, root)
	swapCodexPollInterval(t, 20*time.Millisecond)

	wantCwd := "/tmp/proj-a"
	otherCwd := "/tmp/proj-b"
	writeRollout(t, root, "2026/05/08", otherCwd, "019d4d18-0b7d-7751-89ca-8a4386362f54", time.Now())

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := codexCaptureSessionID(ctx, wantCwd, time.Now().Add(-2*time.Second))
	if err == nil {
		t.Fatalf("expected ctx-deadline error when no rollout matches cwd")
	}
}

func TestCodexCaptureRespectsContextCancel(t *testing.T) {
	root := t.TempDir()
	swapCodexSessionsDir(t, root)
	swapCodexPollInterval(t, 20*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := codexCaptureSessionID(ctx, "/tmp/never", time.Now())
		done <- err
	}()
	time.Sleep(40 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("capture did not return after context cancel")
	}
}

func TestCodexCaptureIgnoresPreexistingRolloutsInSameCwd(t *testing.T) {
	root := t.TempDir()
	swapCodexSessionsDir(t, root)
	swapCodexPollInterval(t, 20*time.Millisecond)

	cwd := "/tmp/proj-a"
	older := "019d4d18-0000-7000-8000-000000000001"
	newer := "019d4d18-0000-7000-8000-000000000002"

	// Pre-existing rollout in the same cwd — must not match. Give
	// it an mtime well before spawnedAt-1s so the cutoff filter
	// excludes it (mirrors reality: a previous codex run hours/days
	// ago, not a millisecond-old "old" file).
	writeRollout(t, root, "2026/05/08", cwd, older, time.Now().Add(-1*time.Hour))

	spawnedAt := time.Now()
	go func() {
		time.Sleep(40 * time.Millisecond)
		writeRollout(t, root, "2026/05/08", cwd, newer, time.Now())
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	got, err := codexCaptureSessionID(ctx, cwd, spawnedAt)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if got != newer {
		t.Errorf("got uuid %q, want %q (the post-spawn one)", got, newer)
	}
}

func TestCodexDefHasResumeArgsAndCapture(t *testing.T) {
	d, ok := Get(IDCodex)
	if !ok {
		t.Fatalf("codex not registered")
	}
	if d.ResumeArgs == nil {
		t.Errorf("codex ResumeArgs is nil")
	} else {
		got := d.ResumeArgs("xyz", "")
		want := []string{"codex", "resume", "xyz"}
		if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
			t.Errorf("ResumeArgs = %v, want %v", got, want)
		}
	}
	if d.CaptureSessionIDFn == nil {
		t.Errorf("codex CaptureSessionIDFn is nil")
	}
}
