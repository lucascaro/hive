//go:build darwin

package main

import (
	"log"
	"sync"

	"fyne.io/systray"
)

// Menu owns the status-bar item and rebuilds it from a Model.
//
// Rebuild-on-change rather than diffing: the whole menu is a dozen
// items, systray.MenuItem.Remove really removes (it is not a hide), and
// a diff would need its own model of what is currently on screen — a
// second source of truth for something that costs nothing to redraw.
//
// Every field is touched from two goroutines (the client's read loop
// and systray's click callbacks), so mu guards the item list and the
// click routing together.
type Menu struct {
	client *Client

	mu sync.Mutex
	// dynamic holds every item built from the last Model, in creation
	// order, so a rebuild can remove exactly what it added and leave
	// the static footer alone.
	dynamic []*systray.MenuItem
	// stop closes the goroutines watching the current dynamic items.
	// A rebuild closes it before dropping the items, so a click handler
	// cannot outlive the item it was watching.
	stop chan struct{}

	// ready gates every systray call. main starts the client goroutine
	// before systray.Run, and the daemon's snapshot arrives on
	// handshake — so the first Model can and does land before onReady
	// has created a single menu item. Adding items to a systray that
	// has not started is not merely early; it is undefined.
	//
	// pending holds the newest Model seen while not ready, so nothing
	// is lost in that window: without it the menu would sit empty
	// until the next daemon event, which on a quiet machine is never.
	ready   bool
	pending *Model

	// Static footer items, created once.
	openGUI   *systray.MenuItem
	reloadGUI *systray.MenuItem
	restart   *systray.MenuItem
	update    *systray.MenuItem
	quit      *systray.MenuItem
}

func NewMenu() *Menu { return &Menu{stop: make(chan struct{})} }

// Attach records the client the menu drives. Split from NewMenu because
// the client needs the menu's Update method to exist first.
func (m *Menu) Attach(c *Client) { m.client = c }

// Ready builds the static half. Called from systray's onReady.
func (m *Menu) Ready() {
	systray.SetTemplateIcon(iconTemplate, iconTemplate)
	systray.SetTooltip("Hive")

	// The footer is created once and never rebuilt, so its click
	// goroutines are started once too.
	m.openGUI = systray.AddMenuItem("Open Hive", "Bring the Hive window forward")
	m.reloadGUI = systray.AddMenuItem("Reload GUI", "Relaunch the windows; sessions keep running")
	m.restart = systray.AddMenuItem("Restart Daemon…", "Ends every running shell and agent")
	m.update = systray.AddMenuItem("Check for Updates…", "Opens Hive and runs the check")
	systray.AddSeparator()
	m.quit = systray.AddMenuItem("Quit Hive", "Stop the daemon and close the menu bar")

	go m.watchFooter()

	m.mu.Lock()
	m.ready = true
	held := m.pending
	m.pending = nil
	m.mu.Unlock()
	if held != nil {
		m.Update(*held)
	} else {
		// Nothing has arrived yet, so say so rather than showing a
		// header-less menu until the daemon answers.
		m.Update(Disconnected())
	}
}

// Update redraws the dynamic half. Safe to call from any goroutine.
func (m *Menu) Update(model Model) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.ready {
		m.pending = &model
		return
	}

	// Stop the click watchers before removing what they watch, then
	// hand the next generation a fresh channel.
	close(m.stop)
	m.stop = make(chan struct{})
	for _, it := range m.dynamic {
		it.Remove()
	}
	m.dynamic = m.dynamic[:0]

	header := systray.AddMenuItem(model.HeaderLine(), "")
	header.Disable()
	m.dynamic = append(m.dynamic, header)

	if line, show := model.ContractLine(); show {
		warn := systray.AddMenuItem(line, "The menu bar and the daemon come from different builds")
		warn.Disable()
		m.dynamic = append(m.dynamic, warn)
	}

	summary := systray.AddMenuItem(model.SummaryLine(), "")
	summary.Disable()
	m.dynamic = append(m.dynamic, summary)

	for _, p := range model.Projects {
		name := p.Name
		if name == "" {
			name = "Untitled project"
		}
		parent := systray.AddMenuItem(name, "")
		m.dynamic = append(m.dynamic, parent)
		for _, s := range p.Sessions {
			row := parent.AddSubMenuItem(s.Label(), "Focus this session in Hive")
			m.dynamic = append(m.dynamic, row)
			m.watchSession(row, s.ID, m.stop)
		}
	}
	sep := systray.AddMenuItem("", "")
	sep.Disable()
	m.dynamic = append(m.dynamic, sep)
}

// watchSession routes one session row's clicks until stop closes.
//
// The stop channel is what makes rebuilding safe: without it, every
// redraw would leak a goroutine parked on a removed item's channel, and
// a menu that redraws on every bell redraws a lot.
func (m *Menu) watchSession(item *systray.MenuItem, id string, stop <-chan struct{}) {
	go func() {
		for {
			select {
			case <-item.ClickedCh:
				// Focus first, then raise: the GUI may not be running,
				// and OpenGUI's launch is what makes the focus land
				// somewhere. Both are best-effort — a click that
				// reaches neither is a no-op, not an error dialog.
				m.client.FocusSession(id)
				if err := OpenGUI(); err != nil {
					log.Printf("hivebar: open GUI: %v", err)
				}
			case <-stop:
				return
			}
		}
	}()
}

func (m *Menu) watchFooter() {
	for {
		select {
		case <-m.openGUI.ClickedCh:
			if err := OpenGUI(); err != nil {
				log.Printf("hivebar: open GUI: %v", err)
			}
		case <-m.reloadGUI.ClickedCh:
			m.client.ReloadGUIs()
		case <-m.restart.ClickedCh:
			m.confirmRestart()
		case <-m.update.ClickedCh:
			// Delegated to the GUI rather than reimplemented here: the
			// GUI owns the staging, verification and bundle swap, and it
			// is the thing being replaced. See cmd/hivebar/README.md.
			if err := OpenGUI(); err != nil {
				log.Printf("hivebar: open GUI: %v", err)
				continue
			}
			m.client.CheckForUpdates()
		case <-m.quit.ClickedCh:
			m.confirmQuit()
		}
	}
}

func (m *Menu) confirmRestart() {
	if !Confirm("Restart the Hive daemon?",
		"This terminates every running shell and agent. Save your work first.") {
		return
	}
	if err := RestartDaemon(m.client.ShutdownDaemon); err != nil {
		log.Printf("hivebar: restart daemon: %v", err)
	}
}

func (m *Menu) confirmQuit() {
	if !Confirm("Quit Hive?",
		"This stops the daemon, terminating every running shell and agent, "+
			"and removes the menu bar icon.") {
		return
	}
	m.client.ShutdownDaemon()
	systray.Quit()
}
