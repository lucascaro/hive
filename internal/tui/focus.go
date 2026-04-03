package tui

import tea "github.com/charmbracelet/bubbletea"

// KeyHandler is implemented by any component that can exclusively capture
// keyboard input. When a KeyHandler reports itself as focused, all KeyMsg
// events are routed to it — nothing leaks to lower-priority handlers or
// global bindings.
type KeyHandler interface {
	// Focused returns true when this handler wants exclusive key input.
	Focused() bool
	// HandleKey processes a key event and returns a command.
	HandleKey(msg tea.KeyMsg) tea.Cmd
}

// componentHandler is a lightweight adapter that turns a pair of closures into
// a KeyHandler. This lets existing components participate in the focus dispatch
// without changing their signatures.
type componentHandler struct {
	focused func() bool
	handle  func(tea.KeyMsg) tea.Cmd
}

func (c componentHandler) Focused() bool                  { return c.focused() }
func (c componentHandler) HandleKey(msg tea.KeyMsg) tea.Cmd { return c.handle(msg) }

// dispatchKey walks handlers in priority order and routes msg to the first
// focused handler. Returns the handler's command and true if a handler was
// found, or (nil, false) if no handler is focused.
func dispatchKey(handlers []KeyHandler, msg tea.KeyMsg) (tea.Cmd, bool) {
	for _, h := range handlers {
		if h.Focused() {
			return h.HandleKey(msg), true
		}
	}
	return nil, false
}
