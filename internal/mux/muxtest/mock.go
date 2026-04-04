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
	calls         map[string]int               // method name → call count
	errors        map[string]error             // method name → error to return
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
		calls:         make(map[string]int),
		errors:        make(map[string]error),
	}
}

// SetError configures the mock to return err on the next call to method.
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

// CallCount returns how many times method was called.
func (m *MockBackend) CallCount(method string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls[method]
}

func (m *MockBackend) record(method string) error {
	m.calls[method]++
	return m.errors[method]
}

// --- mux.Backend implementation ---

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
	return false
}

func (m *MockBackend) GetCurrentCommand(target string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.record("GetCurrentCommand"); err != nil {
		return "", err
	}
	return "sh", nil
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

func (m *MockBackend) UseExecAttach() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record("UseExecAttach")
	return false
}

func (m *MockBackend) DetachKey() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record("DetachKey")
	return "Ctrl+D"
}
