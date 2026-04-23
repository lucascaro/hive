// Package muxtest provides a mock implementation of mux.Backend for testing.
package muxtest

import (
	"fmt"
	"sync"

	"github.com/lucascaro/hive/internal/mux"
)

// MockBackend is an in-memory implementation of mux.Backend for use in tests.
// It tracks sessions/windows and records method calls for assertions.
type MockBackend struct {
	mu            sync.Mutex
	sessions      map[string]bool              // session name → exists
	windows       map[string]string            // "session:idx" → window name
	nextWindowIdx map[string]int               // session → next window index
	paneContents  map[string]string            // "session:idx" → capture content
	paneTitles    map[string]string            // "session:idx" → pane title
	paneBells     map[string]bool              // "session:idx" → bell flag
	paneDead      map[string]bool              // "session:idx" → pane dead flag
	calls         map[string]int               // method name → call count
	errors        map[string]error             // method name → error to return

	// LastSentTarget and LastSentKeys record the most-recent SendKeys call.
	// Exported for use in test assertions.
	LastSentTarget string
	LastSentKeys   string

	// useExecAttach controls the value returned by UseExecAttach.
	// Defaults to false (native backend behaviour). Set via SetUseExecAttach.
	useExecAttach bool
}

// Compile-time check that MockBackend satisfies mux.Backend.
var _ mux.Backend = (*MockBackend)(nil)

// New returns a ready-to-use MockBackend.
func New() *MockBackend {
	return &MockBackend{
		sessions:      make(map[string]bool),
		windows:       make(map[string]string),
		nextWindowIdx: make(map[string]int),
		paneContents:  make(map[string]string),
		paneTitles:    make(map[string]string),
		paneBells:     make(map[string]bool),
		paneDead:      make(map[string]bool),
		calls:         make(map[string]int),
		errors:        make(map[string]error),
	}
}

// SetError configures the mock to return err on all subsequent calls to method.
// Call SetError(method, nil) to clear.
func (m *MockBackend) SetError(method string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errors[method] = err
}

// SetPaneContent sets the content returned by CapturePane/CapturePaneRaw for target.
func (m *MockBackend) SetPaneContent(target, content string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.paneContents[target] = content
}

// SetPaneTitle sets the title returned by GetPaneTitles for target.
func (m *MockBackend) SetPaneTitle(target, title string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.paneTitles[target] = title
}

// SetPaneBell sets the bell flag returned by GetPaneTitles for target.
func (m *MockBackend) SetPaneBell(target string, bell bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.paneBells[target] = bell
}

// CallCount returns how many times method was called.
func (m *MockBackend) CallCount(method string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls[method]
}

// ResetCounts clears all recorded call counts and the LastSentKeys/Target fields.
func (m *MockBackend) ResetCounts() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = make(map[string]int)
	m.LastSentTarget = ""
	m.LastSentKeys = ""
}

// record must be called with m.mu held.
func (m *MockBackend) record(method string) error {
	m.calls[method]++
	return m.errors[method]
}

func (m *MockBackend) IsAvailable() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record("IsAvailable")
	return true
}

func (m *MockBackend) IsServerRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record("IsServerRunning")
	return true
}

func (m *MockBackend) CreateSession(session, windowName, workDir string, cmd []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.record("CreateSession"); err != nil {
		return err
	}
	m.sessions[session] = true
	target := fmt.Sprintf("%s:%d", session, 0)
	m.windows[target] = windowName
	m.nextWindowIdx[session] = 1
	return nil
}

func (m *MockBackend) SessionExists(session string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record("SessionExists")
	return m.sessions[session]
}

func (m *MockBackend) KillSession(session string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.record("KillSession"); err != nil {
		return err
	}
	delete(m.sessions, session)
	// Remove all windows for this session.
	for k := range m.windows {
		if len(k) > len(session) && k[:len(session)+1] == session+":" {
			delete(m.windows, k)
		}
	}
	return nil
}

func (m *MockBackend) ListSessionNames() ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.record("ListSessionNames"); err != nil {
		return nil, err
	}
	var names []string
	for name := range m.sessions {
		names = append(names, name)
	}
	return names, nil
}

func (m *MockBackend) CreateWindow(session, windowName, workDir string, cmd []string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.record("CreateWindow"); err != nil {
		return 0, err
	}
	idx := m.nextWindowIdx[session]
	m.nextWindowIdx[session] = idx + 1
	target := fmt.Sprintf("%s:%d", session, idx)
	m.windows[target] = windowName
	return idx, nil
}

func (m *MockBackend) WindowExists(target string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record("WindowExists")
	_, ok := m.windows[target]
	return ok
}

func (m *MockBackend) KillWindow(target string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.record("KillWindow"); err != nil {
		return err
	}
	delete(m.windows, target)
	return nil
}

func (m *MockBackend) RenameWindow(target, newName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.record("RenameWindow"); err != nil {
		return err
	}
	if _, ok := m.windows[target]; ok {
		m.windows[target] = newName
	}
	return nil
}

func (m *MockBackend) ListWindows(session string) ([]mux.WindowInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.record("ListWindows"); err != nil {
		return nil, err
	}
	var windows []mux.WindowInfo
	prefix := session + ":"
	for target, name := range m.windows {
		if len(target) > len(prefix) && target[:len(prefix)] == prefix {
			var idx int
			fmt.Sscanf(target[len(prefix):], "%d", &idx)
			windows = append(windows, mux.WindowInfo{Index: idx, Name: name})
		}
	}
	return windows, nil
}

func (m *MockBackend) GetPaneTitles(session string) (map[string]string, map[string]bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.record("GetPaneTitles"); err != nil {
		return nil, nil, err
	}
	titles := make(map[string]string)
	bells := make(map[string]bool)
	prefix := session + ":"
	for target, title := range m.paneTitles {
		if len(target) > len(prefix) && target[:len(prefix)] == prefix {
			titles[target] = title
		}
	}
	for target, bell := range m.paneBells {
		if len(target) > len(prefix) && target[:len(prefix)] == prefix && bell {
			bells[target] = true
		}
	}
	return titles, bells, nil
}

func (m *MockBackend) BatchCapturePane(targets map[string]int, _ bool) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.record("BatchCapturePane"); err != nil {
		return nil, err
	}
	results := make(map[string]string, len(targets))
	for target := range targets {
		if content, ok := m.paneContents[target]; ok {
			results[target] = content
		}
	}
	return results, nil
}

func (m *MockBackend) CapturePane(target string, lines int) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.record("CapturePane"); err != nil {
		return "", err
	}
	return m.paneContents[target], nil
}

func (m *MockBackend) CapturePaneRaw(target string, lines int) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.record("CapturePaneRaw"); err != nil {
		return "", err
	}
	return m.paneContents[target], nil
}

func (m *MockBackend) IsPaneDead(target string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record("IsPaneDead")
	return m.paneDead[target]
}

// SetPaneDead marks a target pane as dead (process exited) or alive.
func (m *MockBackend) SetPaneDead(target string, dead bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if dead {
		m.paneDead[target] = true
	} else {
		delete(m.paneDead, target)
	}
}

// AddWindow adds a window entry to the mock so that WindowExists returns true.
func (m *MockBackend) AddWindow(target, name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.windows[target] = name
}

// RemoveWindow removes a window from the mock, simulating a tmux window
// that has disappeared (e.g. agent process crashed).
func (m *MockBackend) RemoveWindow(target string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.windows, target)
}

func (m *MockBackend) GetCurrentCommand(target string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.record("GetCurrentCommand"); err != nil {
		return "", err
	}
	return "sh", nil
}

func (m *MockBackend) SendKeys(target, keys string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.record("SendKeys"); err != nil {
		return err
	}
	m.LastSentTarget = target
	m.LastSentKeys = keys
	return nil
}

func (m *MockBackend) Attach(target string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.record("Attach")
}

func (m *MockBackend) SupportsPopup() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record("SupportsPopup")
	return false
}

func (m *MockBackend) PopupAttach(target, title string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.record("PopupAttach")
}

// SetUseExecAttach configures whether UseExecAttach reports true.
// Call this in tests that exercise the tea.ExecProcess attach path.
func (m *MockBackend) SetUseExecAttach(v bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.useExecAttach = v
}

func (m *MockBackend) UseExecAttach() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record("UseExecAttach")
	return m.useExecAttach
}

func (m *MockBackend) DetachKey() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record("DetachKey")
	return "Ctrl+Q"
}
