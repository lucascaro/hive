package agent

import "testing"

func TestBuiltinsPresent(t *testing.T) {
	want := []ID{IDShell, IDClaude, IDCodex, IDGemini, IDCopilot, IDAider}
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
