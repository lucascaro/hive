package agent

import "testing"

func TestBuiltinsPresent(t *testing.T) {
	want := []ID{IDShell, IDClaude, IDCodex, IDGemini, IDCopilot, IDAider, IDPi}
	for _, id := range want {
		if _, ok := Get(id); !ok {
			t.Errorf("missing built-in agent %s", id)
		}
	}
	all := All()
	if len(all) != len(want) {
		t.Errorf("All() returned %d defs, want %d", len(all), len(want))
	}
	if all[0].ID != IDShell {
		t.Errorf("first agent should be shell; got %s", all[0].ID)
	}
}

func TestShellAlwaysAvailable(t *testing.T) {
	d, _ := Get(IDShell)
	if !d.Available() {
		t.Errorf("shell agent should always be Available()")
	}
}

func TestUnknownAvailableFalse(t *testing.T) {
	d := Def{ID: "definitely-not-real-cli", Cmd: []string{"definitely-not-real-cli-xyz-12345"}}
	if d.Available() {
		t.Errorf("unknown binary should not be Available()")
	}
}

func TestClaudeDefSupportsSessionID(t *testing.T) {
	d, ok := Get(IDClaude)
	if !ok {
		t.Fatalf("claude agent not registered")
	}
	if d.SessionIDFlag != "--session-id" {
		t.Errorf("claude SessionIDFlag = %q, want --session-id", d.SessionIDFlag)
	}
	if d.ResumeArgs == nil {
		t.Fatalf("claude ResumeArgs is nil; expected per-id resume support")
	}
	prev := claudeSessionExists
	claudeSessionExists = func(id, cwd string) bool { return true }
	t.Cleanup(func() { claudeSessionExists = prev })
	got := d.ResumeArgs("abc123", "/tmp/some/cwd")
	want := []string{"claude", "--resume", "abc123"}
	if len(got) != len(want) {
		t.Fatalf("ResumeArgs len = %d, want %d (got %v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ResumeArgs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestPiDefSupportsSessionID(t *testing.T) {
	d, ok := Get(IDPi)
	if !ok {
		t.Fatalf("pi agent not registered")
	}
	if d.Name != "Pi" {
		t.Errorf("pi Name = %q, want Pi", d.Name)
	}
	if len(d.Cmd) != 1 || d.Cmd[0] != "pi" {
		t.Errorf("pi Cmd = %v, want [pi]", d.Cmd)
	}
	// `pi --session-id <id>` pins a caller-chosen id at first launch
	// (verified against pi 0.79.3), so pi resumes unambiguously by id
	// even when sibling sessions share a cwd.
	if d.SessionIDFlag != "--session-id" {
		t.Errorf("pi SessionIDFlag = %q, want --session-id", d.SessionIDFlag)
	}
	if d.ResumeArgs == nil {
		t.Fatalf("pi ResumeArgs is nil; expected per-id resume support")
	}
	got := d.ResumeArgs("abc123", "/tmp/some/cwd")
	want := []string{"pi", "--session-id", "abc123"}
	if len(got) != len(want) {
		t.Fatalf("ResumeArgs len = %d, want %d (got %v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ResumeArgs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	// Generic cwd-scoped continue stays as the fallback for sessions
	// launched with a user-supplied Cmd (no SessionIDFlag injection).
	wantResume := []string{"pi", "-c"}
	if len(d.ResumeCmd) != len(wantResume) || d.ResumeCmd[0] != "pi" || d.ResumeCmd[1] != "-c" {
		t.Errorf("pi ResumeCmd = %v, want %v", d.ResumeCmd, wantResume)
	}
}

func TestAvailableSubsetOfAll(t *testing.T) {
	all := All()
	avail := Available()
	if len(avail) > len(all) {
		t.Errorf("Available() bigger than All()")
	}
	// Shell must always show up.
	found := false
	for _, d := range avail {
		if d.ID == IDShell {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Available() did not include shell")
	}
}
