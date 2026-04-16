package components

import "github.com/charmbracelet/bubbles/key"

// DefaultGridKeys returns the historical hard-coded bindings GridView used
// before keymaps were plumbed in. Used by component-level tests that exercise
// Update directly without constructing a full KeyMap. Lives in a _test.go file
// so it isn't shipped in the binary API surface.
func DefaultGridKeys() GridKeys {
	return GridKeys{
		Detach:      key.NewBinding(key.WithKeys("ctrl+q")),
		InputMode:   key.NewBinding(key.WithKeys("i")),
		Attach:      key.NewBinding(key.WithKeys("enter")),
		CursorUp:    key.NewBinding(key.WithKeys("up")),
		CursorDown:  key.NewBinding(key.WithKeys("down")),
		CursorLeft:  key.NewBinding(key.WithKeys("left")),
		CursorRight: key.NewBinding(key.WithKeys("right")),
	}
}
