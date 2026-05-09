package agent

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func swapCopilotSessionStateDir(t *testing.T, dir string) {
	t.Helper()
	prev := copilotSessionStateDir
	copilotSessionStateDir = dir
	t.Cleanup(func() { copilotSessionStateDir = prev })
}

func swapCopilotPollInterval(t *testing.T, d time.Duration) {
	t.Helper()
	prev := copilotCapturePollInterval
	copilotCapturePollInterval = d
	t.Cleanup(func() { copilotCapturePollInterval = prev })
}

// writeCopilotSession plants a fake copilot session-state directory
// `<root>/<uuid>/workspace.yaml` containing `cwd: <cwd>` and forces
// the workspace.yaml mtime. Returns the dir path.
func writeCopilotSession(t *testing.T, root, uuid, cwd string, modTime time.Time) string {
	t.Helper()
	dir := filepath.Join(root, uuid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	wsPath := filepath.Join(dir, "workspace.yaml")
	body := "id: " + uuid + "\ncwd: " + cwd + "\nsummary: test\n"
	if err := os.WriteFile(wsPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write workspace.yaml: %v", err)
	}
	if err := os.Chtimes(wsPath, modTime, modTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	return dir
}

func TestCopilotCaptureReadsWorkspaceUUID(t *testing.T) {
	root := t.TempDir()
	swapCopilotSessionStateDir(t, root)
	swapCopilotPollInterval(t, 20*time.Millisecond)

	cwd := "/tmp/proj-a"
	wantUUID := "03cab944-0fea-4f86-b3be-989a00303876"

	spawnedAt := time.Now()
	// Write the session dir slightly in the future to simulate
	// copilot creating workspace.yaml just after spawn.
	go func() {
		time.Sleep(40 * time.Millisecond)
		writeCopilotSession(t, root, wantUUID, cwd, time.Now())
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	got, err := copilotCaptureSessionID(ctx, cwd, spawnedAt)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if got != wantUUID {
		t.Errorf("got uuid %q, want %q", got, wantUUID)
	}
}

func TestCopilotCaptureSkipsMismatchedCwd(t *testing.T) {
	root := t.TempDir()
	swapCopilotSessionStateDir(t, root)
	swapCopilotPollInterval(t, 20*time.Millisecond)

	wantCwd := "/tmp/proj-a"
	otherCwd := "/tmp/proj-b"
	writeCopilotSession(t, root, "03cab944-0fea-4f86-b3be-989a00303876", otherCwd, time.Now())

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := copilotCaptureSessionID(ctx, wantCwd, time.Now().Add(-2*time.Second))
	if err == nil {
		t.Fatalf("expected ctx-deadline error when no session matches cwd")
	}
}

func TestCopilotCaptureRespectsContextCancel(t *testing.T) {
	root := t.TempDir()
	swapCopilotSessionStateDir(t, root)
	swapCopilotPollInterval(t, 20*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := copilotCaptureSessionID(ctx, "/tmp/never", time.Now())
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

func TestCopilotCaptureIgnoresPreexistingSessionsInSameCwd(t *testing.T) {
	root := t.TempDir()
	swapCopilotSessionStateDir(t, root)
	swapCopilotPollInterval(t, 20*time.Millisecond)

	cwd := "/tmp/proj-a"
	older := "03cab944-0000-7000-8000-000000000001"
	newer := "03cab944-0000-7000-8000-000000000002"

	// Pre-existing session-state dir in the same cwd — must not match.
	// mtime well before spawnedAt-1s mirrors a previous copilot run.
	writeCopilotSession(t, root, older, cwd, time.Now().Add(-1*time.Hour))

	spawnedAt := time.Now()
	go func() {
		time.Sleep(40 * time.Millisecond)
		writeCopilotSession(t, root, newer, cwd, time.Now())
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	got, err := copilotCaptureSessionID(ctx, cwd, spawnedAt)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if got != newer {
		t.Errorf("got uuid %q, want %q (the post-spawn one)", got, newer)
	}
}

func TestCopilotCaptureSkipsNonUUIDDirNames(t *testing.T) {
	root := t.TempDir()
	swapCopilotSessionStateDir(t, root)
	swapCopilotPollInterval(t, 20*time.Millisecond)

	cwd := "/tmp/proj-a"
	// A non-UUID-named dir with a matching workspace.yaml — must be
	// ignored regardless of cwd match.
	if err := os.MkdirAll(filepath.Join(root, "not-a-uuid"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "not-a-uuid", "workspace.yaml"),
		[]byte("cwd: "+cwd+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := copilotCaptureSessionID(ctx, cwd, time.Now().Add(-2*time.Second))
	if err == nil {
		t.Fatalf("expected ctx-deadline error; non-UUID dir should be ignored")
	}
}

func TestCopilotDefHasResumeArgsAndCapture(t *testing.T) {
	d, ok := Get(IDCopilot)
	if !ok {
		t.Fatalf("copilot not registered")
	}
	if d.ResumeArgs == nil {
		t.Errorf("copilot ResumeArgs is nil")
	} else {
		got := d.ResumeArgs("xyz")
		want := []string{"copilot", "--resume=xyz"}
		if !slices.Equal(got, want) {
			t.Errorf("ResumeArgs = %v, want %v", got, want)
		}
	}
	if d.CaptureSessionIDFn == nil {
		t.Errorf("copilot CaptureSessionIDFn is nil")
	}
	// Copilot has no caller-pinning flag.
	if d.SessionIDFlag != "" {
		t.Errorf("copilot SessionIDFlag = %q, want empty (no --session-id support)", d.SessionIDFlag)
	}
}

func TestGeminiDefHasSessionIDPinning(t *testing.T) {
	d, ok := Get(IDGemini)
	if !ok {
		t.Fatalf("gemini not registered")
	}
	if d.SessionIDFlag != "--session-id" {
		t.Errorf("gemini SessionIDFlag = %q, want --session-id", d.SessionIDFlag)
	}
	if d.ResumeArgs == nil {
		t.Errorf("gemini ResumeArgs is nil")
	} else {
		got := d.ResumeArgs("xyz")
		want := []string{"gemini", "--resume", "xyz"}
		if !slices.Equal(got, want) {
			t.Errorf("ResumeArgs = %v, want %v", got, want)
		}
	}
	// Gemini pins at launch — no capture goroutine needed.
	if d.CaptureSessionIDFn != nil {
		t.Errorf("gemini CaptureSessionIDFn should be nil (uses pinning instead)")
	}
}
