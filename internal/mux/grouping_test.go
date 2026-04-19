package mux

import "testing"

// fakeBackend is the minimal mux.Backend used by grouping tests. It is
// deliberately stubby: the grouping test surface only exercises the package-
// level functions that type-assert for GroupedBackend.
type fakeBackend struct{}

func (fakeBackend) IsAvailable() bool                                                   { return true }
func (fakeBackend) IsServerRunning() bool                                               { return true }
func (fakeBackend) CreateSession(string, string, string, []string) error                { return nil }
func (fakeBackend) SessionExists(string) bool                                           { return true }
func (fakeBackend) KillSession(string) error                                            { return nil }
func (fakeBackend) ListSessionNames() ([]string, error)                                 { return nil, nil }
func (fakeBackend) CreateWindow(string, string, string, []string) (int, error)          { return 0, nil }
func (fakeBackend) WindowExists(string) bool                                            { return true }
func (fakeBackend) KillWindow(string) error                                             { return nil }
func (fakeBackend) RenameWindow(string, string) error                                   { return nil }
func (fakeBackend) ListWindows(string) ([]WindowInfo, error)                            { return nil, nil }
func (fakeBackend) GetPaneTitles(string) (map[string]string, map[string]bool, error)    { return nil, nil, nil }
func (fakeBackend) CapturePane(string, int) (string, error)                             { return "", nil }
func (fakeBackend) CapturePaneRaw(string, int) (string, error)                          { return "", nil }
func (fakeBackend) BatchCapturePane(map[string]int, bool) (map[string]string, error)    { return nil, nil }
func (fakeBackend) GetCurrentCommand(string) (string, error)                            { return "", nil }
func (fakeBackend) IsPaneDead(string) bool                                              { return false }
func (fakeBackend) SendKeys(string, string) error                                       { return nil }
func (fakeBackend) Attach(string) error                                                 { return nil }
func (fakeBackend) SupportsPopup() bool                                                 { return false }
func (fakeBackend) PopupAttach(string, string) error                                    { return nil }
func (fakeBackend) UseExecAttach() bool                                                 { return false }
func (fakeBackend) DetachKey() string                                                   { return "" }

type fakeGroupedBackend struct {
	fakeBackend
	instance    string
	canonical   bool
	initCalled  bool
	downCalled  bool
	sweepCalled bool
}

func (f *fakeGroupedBackend) InitInstance() error {
	f.initCalled = true
	f.instance = "hive-sessions-1-abcd"
	return nil
}
func (f *fakeGroupedBackend) ShutdownInstance() error {
	f.downCalled = true
	f.instance = ""
	return nil
}
func (f *fakeGroupedBackend) SweepOrphanInstances() error { f.sweepCalled = true; return nil }
func (f *fakeGroupedBackend) InstanceSession() string     { return f.instance }
func (f *fakeGroupedBackend) CanonicalExists() bool       { return f.canonical }

func TestInstanceSession_FallsBackToCanonicalWhenNoGroup(t *testing.T) {
	SetBackend(fakeBackend{})
	defer SetBackend(nil)
	if got := InstanceSession(); got != HiveSession {
		t.Errorf("InstanceSession() with non-grouped backend = %q; want %q", got, HiveSession)
	}
}

func TestInstanceSession_ReturnsGroupedName(t *testing.T) {
	gb := &fakeGroupedBackend{canonical: true}
	SetBackend(gb)
	defer SetBackend(nil)

	// Before InitInstance: backend reports empty → fall back to canonical.
	if got := InstanceSession(); got != HiveSession {
		t.Errorf("InstanceSession() before Init = %q; want canonical %q", got, HiveSession)
	}
	if err := InitInstance(); err != nil {
		t.Fatalf("InitInstance: %v", err)
	}
	if !gb.initCalled {
		t.Error("InitInstance did not forward to GroupedBackend.InitInstance")
	}
	if got := InstanceSession(); got != "hive-sessions-1-abcd" {
		t.Errorf("InstanceSession() after Init = %q; want grouped name", got)
	}
	if err := ShutdownInstance(); err != nil {
		t.Fatalf("ShutdownInstance: %v", err)
	}
	if !gb.downCalled {
		t.Error("ShutdownInstance did not forward to GroupedBackend.ShutdownInstance")
	}
}

func TestSweepOrphanInstances_ForwardsToGroupedBackend(t *testing.T) {
	gb := &fakeGroupedBackend{}
	SetBackend(gb)
	defer SetBackend(nil)
	if err := SweepOrphanInstances(); err != nil {
		t.Fatalf("SweepOrphanInstances: %v", err)
	}
	if !gb.sweepCalled {
		t.Error("SweepOrphanInstances did not forward to GroupedBackend.SweepOrphanInstances")
	}
}

func TestCanonicalExists_TrueForNonGroupedBackend(t *testing.T) {
	SetBackend(fakeBackend{})
	defer SetBackend(nil)
	// Non-grouped backends don't track a canonical session, so the helper
	// returns true (no false negatives that would trigger the fatal dialog).
	if !CanonicalExists() {
		t.Error("CanonicalExists() on non-grouped backend = false; want true")
	}
}
