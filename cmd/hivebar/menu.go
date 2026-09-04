//go:build darwin

package main

import (
	"fmt"
	"log"
	"sync"

	"fyne.io/systray"
)

// maxSessionRows bounds the session list.
//
// A fixed pool is what keeps the menu still (see Menu), so it has to
// have a size. 24 is well past what fits on screen next to a menu bar
// and past any plausible session count; anything beyond it is reported
// by the overflow row rather than dropped silently.
const maxSessionRows = 24

// Menu owns the status-bar item.
//
// Every item is created ONCE, in Ready, and afterwards only ever
// retitled, shown or hidden. Nothing is added or removed while the menu
// is alive, and that is the whole design:
//
//   - systray.AddMenuItem APPENDS. An earlier cut rebuilt the dynamic
//     half on every update, which re-appended the header and the session
//     list BELOW the static footer, so the menu visibly reordered itself.
//   - The daemon emits a `title` event every time a shell redraws its
//     prompt, so "on every update" is many times a second. Even without
//     the reordering, tearing rows down and building them back up made
//     the list jump while the user was reading it.
//
// A fixed pool costs a bounded number of hidden items and buys a menu
// that only ever changes its text. It also removes the per-rebuild
// click-watcher goroutines: each row has exactly one watcher for the
// life of the process, reading whichever session id the row currently
// stands for.
type Menu struct {
	client *Client

	mu sync.Mutex
	// ready gates every systray call. main starts the client goroutine
	// before systray.Run and the daemon's snapshot arrives on
	// handshake, so the first Model routinely lands before onReady has
	// created anything. pending holds the newest one seen until then —
	// dropping it would leave the menu blank until the next daemon
	// event, which on a quiet machine never comes.
	ready   bool
	pending *Model
	// rowIDs[i] is the session id row i currently stands for, or "" if
	// the row is hidden. Read by that row's click watcher.
	rowIDs [maxSessionRows]string

	header   *systray.MenuItem
	contract *systray.MenuItem
	summary  *systray.MenuItem
	rows     [maxSessionRows]*systray.MenuItem
	overflow *systray.MenuItem

	openGUI   *systray.MenuItem
	reloadGUI *systray.MenuItem
	restart   *systray.MenuItem
	update    *systray.MenuItem
	quit      *systray.MenuItem
}

func NewMenu() *Menu { return &Menu{} }

// Attach records the client the menu drives. Split from NewMenu because
// the client needs the menu's Update method to exist first.
func (m *Menu) Attach(c *Client) { m.client = c }

// Ready builds every item, in final order. Called from systray's
// onReady, and the only place that adds anything to the menu.
func (m *Menu) Ready() {
	systray.SetTemplateIcon(iconTemplate, iconTemplate)
	systray.SetTooltip("Hive")

	m.header = systray.AddMenuItem("", "")
	m.header.Disable()
	m.contract = systray.AddMenuItem("", "The menu bar and the daemon come from different builds")
	m.contract.Disable()
	m.contract.Hide()
	m.summary = systray.AddMenuItem("", "")
	m.summary.Disable()

	systray.AddSeparator()
	for i := range m.rows {
		m.rows[i] = systray.AddMenuItem("", "Focus this session in Hive")
		m.rows[i].Hide()
		m.watchRow(i)
	}
	m.overflow = systray.AddMenuItem("", "")
	m.overflow.Disable()
	m.overflow.Hide()

	systray.AddSeparator()
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
		return
	}
	// Say "not running" rather than showing an empty menu until the
	// daemon answers — an empty session list reads as "nothing is
	// running", which is a different claim.
	m.Update(Disconnected())
}

// Update retitles the existing items. Safe to call from any goroutine,
// and never adds or removes anything — see the type comment.
func (m *Menu) Update(model Model) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.ready {
		m.pending = &model
		return
	}

	m.header.SetTitle(model.HeaderLine())
	if line, show := model.ContractLine(); show {
		m.contract.SetTitle(line)
		m.contract.Show()
	} else {
		m.contract.Hide()
	}
	m.summary.SetTitle(model.SummaryLine())

	// Flatten the grouped model into the fixed pool. The project name
	// rides on each row instead of becoming a submenu: a submenu tree
	// cannot be a fixed pool without one pool per project, and a row
	// you can click straight from the top level beats one you have to
	// hover a parent to reach.
	i := 0
	for _, p := range model.Projects {
		for _, s := range p.Sessions {
			if i >= len(m.rows) {
				break
			}
			m.rows[i].SetTitle(s.LabelIn(p.Name))
			m.rows[i].Show()
			m.rowIDs[i] = s.ID
			i++
		}
	}
	for ; i < len(m.rows); i++ {
		m.rows[i].Hide()
		m.rowIDs[i] = ""
	}

	if hidden := model.Sessions - len(m.rows); hidden > 0 {
		m.overflow.SetTitle(fmt.Sprintf("…and %d more in Hive", hidden))
		m.overflow.Show()
	} else {
		m.overflow.Hide()
	}
}

// watchRow routes one pool row's clicks for the life of the process.
//
// The row's meaning changes as the pool is reassigned, so the id is
// read at click time rather than captured: a watcher that closed over
// the id it was created with would focus whatever session happened to
// be in that slot when the menu was built.
func (m *Menu) watchRow(i int) {
	go func() {
		for range m.rows[i].ClickedCh {
			m.mu.Lock()
			id := m.rowIDs[i]
			m.mu.Unlock()
			if id == "" {
				continue // hidden row; nothing to focus
			}
			// Focus first, then raise: the GUI may not be running, and
			// OpenGUI's launch is what makes the focus land somewhere.
			// Both are best-effort — a click that reaches neither is a
			// no-op, not an error dialog.
			m.client.FocusSession(id)
			if err := OpenGUI(); err != nil {
				log.Printf("hivebar: open GUI: %v", err)
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
