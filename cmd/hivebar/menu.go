// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

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

	// The menu-bar controls. Unlike Quit Hive these never touch the
	// daemon: they act on this process only.
	loginItem  *systray.MenuItem
	restartBar *systray.MenuItem
	quitBar    *systray.MenuItem

	// Guarded by mu. loginStatus is what the Start at Login item is
	// showing; toggling drops a second click while one is in flight;
	// quitting suppresses alerts once the process is going away, so an
	// osascript dialog is not orphaned by systray.Quit.
	loginStatus string
	toggling    bool
	quitting    bool

	// probeReq has capacity 1 and is the whole "needs a refresh" state: a
	// request while a probe runs leaves exactly one more queued. The
	// systray open signal is an unbuffered, non-blocking send, so the
	// receiver must be parked to catch it — see watchTrayOpen.
	probeReq chan struct{}

	// Seams, swapped in tests. Production values come from NewMenu.
	run    runner
	gui    string
	app    string
	notify func(title, body string)
	quitFn func()
}

func NewMenu() *Menu {
	return &Menu{
		probeReq: make(chan struct{}, 1),
		run:      runWithTimeout,
		gui:      guiBinary(),
		app:      ownBundle(),
		notify:   Notify,
		quitFn:   systray.Quit,
	}
}

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
	// Hidden until the first probe answers: a checkmark that guesses is
	// worse than no item (see loginItemView).
	m.loginItem = systray.AddMenuItem("Start at Login", "")
	m.loginItem.Hide()
	m.restartBar = systray.AddMenuItem("Restart Menu Bar", "Relaunch the menu bar; the daemon and sessions keep running")
	m.quitBar = systray.AddMenuItem("Quit Menu Bar", "Close the menu bar; the daemon and sessions keep running")
	systray.AddSeparator()
	m.quit = systray.AddMenuItem("Quit Hive", "Stop the daemon and close the menu bar")
	go m.watchFooter()
	go m.probeWorker()
	go m.watchTrayOpen()

	m.mu.Lock()
	m.ready = true
	held := m.pending
	m.pending = nil
	m.mu.Unlock()

	m.requestProbe()
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

// footerClick names one footer item. watchFooter turns channel receives into
// these so the dispatch below is testable without systray.
type footerClick int

const (
	clickOpenGUI footerClick = iota
	clickReloadGUI
	clickRestartDaemon
	clickCheckUpdates
	clickLoginItem
	clickRestartBar
	clickQuitBar
	clickQuitHive
)

// watchFooter is a pure forwarder. Every handler runs on its own goroutine
// (dispatch), because several block for a long time — a confirm dialog, an
// exec with a 5 s deadline — and none of them may stop another click from
// being heard, least of all Quit Menu Bar.
func (m *Menu) watchFooter() {
	for {
		select {
		case <-m.openGUI.ClickedCh:
			m.dispatch(clickOpenGUI)
		case <-m.reloadGUI.ClickedCh:
			m.dispatch(clickReloadGUI)
		case <-m.restart.ClickedCh:
			m.dispatch(clickRestartDaemon)
		case <-m.update.ClickedCh:
			m.dispatch(clickCheckUpdates)
		case <-m.loginItem.ClickedCh:
			m.dispatch(clickLoginItem)
		case <-m.restartBar.ClickedCh:
			m.dispatch(clickRestartBar)
		case <-m.quitBar.ClickedCh:
			m.dispatch(clickQuitBar)
		case <-m.quit.ClickedCh:
			m.dispatch(clickQuitHive)
		}
	}
}

func (m *Menu) dispatch(c footerClick) { go m.handle(c) }

func (m *Menu) handle(c footerClick) {
	switch c {
	case clickOpenGUI:
		if err := OpenGUI(); err != nil {
			log.Printf("hivebar: open GUI: %v", err)
		}
	case clickReloadGUI:
		m.client.ReloadGUIs()
	case clickRestartDaemon:
		m.confirmRestart()
	case clickCheckUpdates:
		// Delegated to the GUI rather than reimplemented here: the
		// GUI owns the staging, verification and bundle swap, and it
		// is the thing being replaced. See cmd/hivebar/README.md.
		if err := OpenGUI(); err != nil {
			log.Printf("hivebar: open GUI: %v", err)
			return
		}
		m.client.CheckForUpdates()
	case clickLoginItem:
		m.toggleLogin()
	case clickRestartBar:
		_ = restartMenuBar(m.run, m.app, m.quitApp, m.alert)
	case clickQuitBar:
		// No confirm: nothing is lost. The daemon and every session keep
		// running, and hived or hivegui bring the icon back next launch.
		m.quitApp()
	case clickQuitHive:
		m.confirmQuit()
	}
}

// quitApp ends this process's menu bar only. It marks the menu as quitting
// first so a late failure alert is not shown over a dying process.
func (m *Menu) quitApp() {
	m.mu.Lock()
	m.quitting = true
	m.mu.Unlock()
	m.quitFn()
}

// alert shows a native dialog unless the process is already quitting.
func (m *Menu) alert(title, body string) {
	m.mu.Lock()
	quitting := m.quitting
	m.mu.Unlock()
	if quitting {
		return
	}
	m.notify(title, body)
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
	m.quitApp()
}

// watchTrayOpen asks for a refresh each time the menu opens, so the
// checkmark is current when the user looks at it — it may have been changed
// in Settings or System Settings since the last time.
//
// systray sends on an unbuffered channel without blocking, so this loop does
// nothing but flag a request and go straight back to receiving; the probe
// itself runs on probeWorker.
func (m *Menu) watchTrayOpen() {
	for range systray.TrayOpenedCh {
		m.requestProbe()
	}
}

// requestProbe asks for one more refresh. Never blocks: a request already
// queued covers this one.
func (m *Menu) requestProbe() {
	select {
	case m.probeReq <- struct{}{}:
	default:
	}
}

func (m *Menu) probeWorker() {
	for range m.probeReq {
		status, err := m.probe()
		m.applyLogin(status, err)
	}
}

func (m *Menu) probe() (string, error) {
	if m.gui == "" {
		return "", fmt.Errorf("not running from a Hive.app bundle")
	}
	return probeLoginItem(m.run, m.gui)
}

// toggleLogin handles one click on Start at Login.
func (m *Menu) toggleLogin() {
	m.mu.Lock()
	if m.toggling {
		m.mu.Unlock()
		return
	}
	m.toggling = true
	status := m.loginStatus
	m.mu.Unlock()

	next, err := toggleLoginItem(m.run, m.gui, status)

	// Release the flag and show the truth BEFORE any alert: display dialog
	// blocks until OK, and the item must not stay locked behind it.
	m.mu.Lock()
	m.toggling = false
	m.mu.Unlock()
	m.applyLogin(next, nil)
	if err != nil {
		log.Printf("hivebar: login item: %v", err)
		m.alert("Could not change the login item", err.Error())
	}
}

// applyLogin records the status and, once the menu exists, shows it.
func (m *Menu) applyLogin(status string, err error) {
	view := loginItemView(status, err)
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		status = ""
	}
	m.loginStatus = status
	if !m.ready {
		return
	}
	if !view.Visible {
		m.loginItem.Hide()
		return
	}
	m.loginItem.SetTitle(view.Title)
	m.loginItem.SetTooltip(view.Tooltip)
	if view.Checked {
		m.loginItem.Check()
	} else {
		m.loginItem.Uncheck()
	}
	m.loginItem.Show()
}
