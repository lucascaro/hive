package muxnative

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// muxSession groups the panes (windows) that belong to a logical session.
type muxSession struct {
	mu      sync.Mutex
	name    string
	panes   map[int]*pane
	nextIdx int
}

// manager is the global registry of all live sessions and their panes.
type manager struct {
	mu       sync.RWMutex
	sessions map[string]*muxSession
}

// defaultMgr is the package-level singleton.
var defaultMgr = &manager{
	sessions: make(map[string]*muxSession),
}

func (m *manager) createSession(sessionName, windowName, workDir string, args []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.sessions[sessionName]; exists {
		return nil // already exists; createWindow will add panes
	}

	p, err := startPane(windowName, workDir, args)
	if err != nil {
		return fmt.Errorf("start pane: %w", err)
	}

	sess := &muxSession{
		name:    sessionName,
		panes:   map[int]*pane{0: p},
		nextIdx: 1,
	}
	m.sessions[sessionName] = sess
	return nil
}

func (m *manager) sessionExists(sessionName string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sess, ok := m.sessions[sessionName]
	if !ok {
		return false
	}
	// A session is live as long as at least one pane is not dead.
	sess.mu.Lock()
	defer sess.mu.Unlock()
	for _, p := range sess.panes {
		if !p.isDead() {
			return true
		}
	}
	return false
}

func (m *manager) killSession(sessionName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	sess, ok := m.sessions[sessionName]
	if !ok {
		return fmt.Errorf("session not found: %s", sessionName)
	}
	sess.mu.Lock()
	for _, p := range sess.panes {
		p.kill()
	}
	sess.mu.Unlock()
	delete(m.sessions, sessionName)
	return nil
}

func (m *manager) listSessionNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.sessions))
	for name := range m.sessions {
		names = append(names, name)
	}
	return names
}

func (m *manager) createWindow(sessionName, windowName, workDir string, args []string) (int, error) {
	m.mu.RLock()
	sess, ok := m.sessions[sessionName]
	m.mu.RUnlock()
	if !ok {
		return 0, fmt.Errorf("session not found: %s", sessionName)
	}

	p, err := startPane(windowName, workDir, args)
	if err != nil {
		return 0, fmt.Errorf("start pane: %w", err)
	}

	// Re-validate the session still exists. killSession() may have run while
	// startPane() was blocked (forking a process). If it's gone, clean up the
	// orphaned pane and return an error rather than storing it on a dead session.
	m.mu.RLock()
	_, stillExists := m.sessions[sessionName]
	m.mu.RUnlock()
	if !stillExists {
		p.kill()
		return 0, fmt.Errorf("session was deleted while starting pane: %s", sessionName)
	}

	sess.mu.Lock()
	idx := sess.nextIdx
	sess.nextIdx++
	sess.panes[idx] = p
	sess.mu.Unlock()

	return idx, nil
}

func (m *manager) windowExists(target string) bool {
	p := m.paneByTarget(target)
	return p != nil && !p.isDead()
}

func (m *manager) killWindow(target string) error {
	sessionName, idx, err := parseTarget(target)
	if err != nil {
		return err
	}
	m.mu.Lock()
	sess, ok := m.sessions[sessionName]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("session not found: %s", sessionName)
	}
	sess.mu.Lock()
	p, ok := sess.panes[idx]
	if ok {
		delete(sess.panes, idx)
	}
	sess.mu.Unlock()
	if !ok {
		return fmt.Errorf("window %d not found in session %s", idx, sessionName)
	}
	p.kill()
	return nil
}

func (m *manager) renameWindow(target, newName string) error {
	p := m.paneByTarget(target)
	if p == nil {
		return fmt.Errorf("window not found: %s", target)
	}
	p.mu.Lock()
	p.name = newName
	p.mu.Unlock()
	return nil
}

func (m *manager) listWindows(sessionName string) ([]windowEntry, error) {
	m.mu.RLock()
	sess, ok := m.sessions[sessionName]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionName)
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()

	result := make([]windowEntry, 0, len(sess.panes))
	for idx, p := range sess.panes {
		p.mu.Lock()
		name := p.name
		dead := p.dead
		p.mu.Unlock()
		if !dead {
			result = append(result, windowEntry{idx: idx, name: name})
		}
	}
	return result, nil
}

// windowEntry is a small internal struct for listWindows.
type windowEntry struct {
	idx  int
	name string
}

// paneByTarget returns the pane for "session:index", or nil if not found.
func (m *manager) paneByTarget(target string) *pane {
	sessionName, idx, err := parseTarget(target)
	if err != nil {
		return nil
	}
	m.mu.RLock()
	sess, ok := m.sessions[sessionName]
	m.mu.RUnlock()
	if !ok {
		return nil
	}
	sess.mu.Lock()
	p := sess.panes[idx]
	sess.mu.Unlock()
	return p
}

// parseTarget splits "session:index" into its components.
func parseTarget(target string) (string, int, error) {
	i := strings.LastIndex(target, ":")
	if i < 0 {
		return "", 0, fmt.Errorf("invalid target %q: expected session:index", target)
	}
	idx, err := strconv.Atoi(target[i+1:])
	if err != nil {
		return "", 0, fmt.Errorf("invalid window index in %q: %w", target, err)
	}
	return target[:i], idx, nil
}

// lastLines returns the last n lines of s (all lines if n == 0).
func lastLines(s string, n int) string {
	if n <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}
