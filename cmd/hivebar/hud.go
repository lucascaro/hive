// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build darwin

package main

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/lucascaro/hive/internal/wire"
)

// The dictation capture HUD (spec 553).
//
// A system-wide hotkey opens a small floating panel that takes keyboard
// focus from whatever app is in front, so any dictation tool, or the
// keyboard, can fill it. The panel names the session the text will go
// to, defaulting to the one a Hive window last showed the user. Nothing
// reaches a session until the user presses Enter, and Esc discards.
//
// HUD is the whole of that behaviour as plain Go: which session is the
// target, what the panel says, when a send happens and with which
// queue flag. The AppKit panel (hud_darwin.m) only draws what HUD tells
// it and reports keys back, because a native window cannot be asserted
// on in a test and a decision made there would be untested.
//
// Delivery goes through SEND_TO_SESSION, the frame the GUI's message
// sheet already uses. The HUD adds no way of its own to put text into a
// session, which is what the spec's "no new entry point" rule asks.

// hudContract is the DaemonContract generation that added the HUD's
// frames (internal/buildinfo/contract.go, entry 36). An older daemon
// never sends GLOBAL_HOTKEY, so the hotkey would silently never
// register; the menu says so instead.
const hudContract = 36

// hudView is the panel. Every method must return promptly: HUD calls
// them with its lock held, and the panel's own callbacks arrive on the
// main thread. The AppKit view queues its work onto the main thread and
// returns.
type hudView interface {
	// Show puts the panel up, or refreshes it if it is up. clear empties
	// the text field, which happens only when the panel opens: a live
	// update must never wipe what the user is dictating. focus makes the
	// panel key with the caret at the end of the text, which only a
	// hotkey press asks for: a live update that took focus would move
	// the caret, or cut off a dictation, mid-sentence.
	Show(st hudState, clear, focus bool)
	// Hide takes the panel down. restore hands focus back to the app that
	// was in front when the panel opened.
	Hide(restore bool)
}

// hudSender delivers confirmed text. It reports false when there is no
// daemon connection to send on.
type hudSender interface {
	SendToSession(id, text string, queue bool) bool
}

// hudState is what the panel shows.
type hudState struct {
	Targets  []hudTarget
	Selected int // index into Targets, or -1 for none
	Status   string
	Hints    string
	CanSend  bool
}

// hudTarget is one live session the text can go to.
type hudTarget struct {
	ID    string
	Label string
	ACP   bool
}

type hudPhase int

const (
	hudEditing hudPhase = iota
	hudSending
	// hudBusy: the target answered session_busy, and Enter now queues.
	hudBusy
)

const (
	hintsEditing = "[↩] send · [⇧↩] new line · [↑↓] target · [esc] cancel"
	hintsBusy    = "[↩] queue it · [⇧↩] new line · [↑↓] target · [esc] cancel"
)

// HUD is the controller. Safe for concurrent use; no method blocks.
type HUD struct {
	view hudView
	send hudSender
	// goSend runs a send off the caller's goroutine, which for every key
	// is the main thread: a socket write must never hold it. Tests run
	// it inline.
	goSend func(func())

	mu        sync.Mutex
	connected bool
	projects  []wire.ProjectInfo
	sessions  []wire.SessionInfo
	// stale counts, per session, the replies still due for sends the user
	// walked away from (Esc while sending). SENT and ERROR name only the
	// session, and the daemon answers a connection's frames in order, so
	// the next that many replies for that session belong to those sends,
	// not to a newer one.
	stale map[string]int

	open   bool
	target string
	// manual records that the user picked the target, so a later focus
	// report does not move it out from under them.
	manual bool
	phase  hudPhase
	status string
}

func NewHUD(view hudView, send hudSender) *HUD {
	return &HUD{view: view, send: send, goSend: func(f func()) { go f() }, stale: map[string]int{}}
}

// SetSessions takes a fresh daemon snapshot. While the panel is open it
// refreshes the target list in place, keeping the text and the chosen
// target; if the target has gone, it says so rather than silently
// moving the text to another session.
func (h *HUD) SetSessions(projects []wire.ProjectInfo, sessions []wire.SessionInfo) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.connected = true
	h.projects = append([]wire.ProjectInfo(nil), projects...)
	h.sessions = append([]wire.SessionInfo(nil), sessions...)
	if !h.open {
		return
	}
	if h.target != "" && !h.liveLocked(h.target) {
		if h.phase == hudSending {
			h.status = "The target session ended while sending; the text may have arrived. Pick another with ↑↓."
		} else {
			h.status = "The target session ended. Pick another with ↑↓."
		}
		h.target = ""
		h.phase = hudEditing
	}
	h.renderLocked(false, false)
}

// Disconnected records that the daemon went away. A send in flight has
// no answer coming, and may or may not have been delivered, so the panel
// says exactly that rather than inviting a blind resend.
func (h *HUD) Disconnected() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.connected = false
	h.projects, h.sessions = nil, nil
	clear(h.stale)
	if !h.open {
		return
	}
	if h.phase == hudSending {
		h.status = "Lost Hive's daemon while sending; the text may have arrived. Check the session before sending again."
	}
	h.target = ""
	h.phase = hudEditing
	h.renderLocked(false, false)
}

// Open shows the panel with the default target. A second press while it
// is open only brings it forward.
func (h *HUD) Open() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.open {
		h.renderLocked(false, true)
		return
	}
	h.open = true
	h.manual = false
	h.phase = hudEditing
	h.status = ""
	h.target = h.defaultTargetLocked()
	h.renderLocked(true, true)
}

// Cancel is Esc: close and send nothing. A reply to a send already in
// flight is then ignored, so it cannot reopen the panel.
func (h *HUD) Cancel() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.open {
		return
	}
	if h.phase == hudSending {
		h.stale[h.target]++
	}
	h.closeLocked()
}

// Move steps the target through the live sessions (↑ is -1, ↓ is +1).
func (h *HUD) Move(delta int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.open || h.phase == hudSending {
		return
	}
	ts := h.targetsLocked()
	if len(ts) == 0 {
		return
	}
	i := indexOf(ts, h.target)
	switch {
	case i < 0 && delta < 0:
		i = len(ts) - 1
	case i < 0:
		i = 0
	default:
		i = (i + delta + len(ts)) % len(ts)
	}
	h.pickLocked(ts[i].ID)
}

// Select picks a target by index into the list the panel was last shown
// (a click in its popup).
func (h *HUD) Select(index int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.open || h.phase == hudSending {
		return
	}
	ts := h.targetsLocked()
	if index < 0 || index >= len(ts) {
		return
	}
	h.pickLocked(ts[index].ID)
}

func (h *HUD) pickLocked(id string) {
	h.target = id
	h.manual = true
	h.phase = hudEditing
	h.status = ""
	h.renderLocked(false, false)
}

// Submit is Enter with the field's text. It sends to the target only,
// and only from a state where a send is allowed; after a busy answer it
// sends again asking the daemon to queue.
func (h *HUD) Submit(text string) {
	send := h.prepareSend(text)
	if send != nil {
		// Outside the lock: the send's own failure path takes it.
		h.goSend(send)
	}
}

// prepareSend validates a submit and moves to hudSending, returning the
// send to run, or nil when nothing is to be sent.
func (h *HUD) prepareSend(text string) func() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.open || h.phase == hudSending {
		return nil
	}
	if strings.TrimSpace(text) == "" {
		return nil
	}
	if len(text) > wire.MaxMessageLen {
		h.status = fmt.Sprintf("Too long to send: the limit is %d KiB.", wire.MaxMessageLen>>10)
		h.renderLocked(false, false)
		return nil
	}
	if h.target == "" || !h.liveLocked(h.target) {
		h.status = "Pick a session to send to with ↑↓."
		h.renderLocked(false, false)
		return nil
	}
	queue := h.phase == hudBusy
	id := h.target
	h.phase = hudSending
	h.status = "Sending…"
	h.renderLocked(false, false)
	return func() {
		if !h.send.SendToSession(id, text, queue) {
			h.failed(id, "Hive's daemon is not running.")
		}
	}
}

func (h *HUD) failed(id, msg string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.open || h.phase != hudSending || h.target != id {
		return
	}
	h.phase = hudEditing
	h.status = msg
	h.renderLocked(false, false)
}

// OnSent is the daemon's SENT. The panel closes and hands focus back.
func (h *HUD) OnSent(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.consumeStaleLocked(id) {
		return
	}
	if !h.open || h.phase != hudSending || h.target != id {
		return
	}
	h.closeLocked()
}

// OnError is a SEND_TO_SESSION refusal for session id.
func (h *HUD) OnError(id, code, msg string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.consumeStaleLocked(id) {
		return
	}
	if !h.open || h.phase != hudSending || h.target != id {
		return
	}
	h.phase = hudEditing
	switch code {
	case wire.ErrCodeSessionBusy:
		if h.isACPLocked(id) {
			// The daemon cannot queue for an ACP session.
			h.status = "The agent is busy. Try again when it is idle."
		} else {
			h.phase = hudBusy
			h.status = "The agent is busy. Press ↩ to queue it for when it is idle."
		}
	case wire.ErrCodeSessionDead, "no_such_session":
		h.status = "The target session is not running. Pick another with ↑↓."
	default:
		if msg == "" {
			msg = code
		}
		h.status = "Not sent: " + msg
	}
	h.renderLocked(false, false)
}

// consumeStaleLocked reports whether a reply for id answers a send the
// user already walked away from, and so must not settle the current one.
func (h *HUD) consumeStaleLocked(id string) bool {
	if h.stale[id] == 0 {
		return false
	}
	if h.stale[id]--; h.stale[id] == 0 {
		delete(h.stale, id)
	}
	return true
}

func (h *HUD) closeLocked() {
	h.open = false
	h.phase = hudEditing
	h.status = ""
	h.target = ""
	h.view.Hide(true)
}

// defaultTargetLocked is the live session a GUI focused most recently,
// or none: guessing would put dictated text into a session the user did
// not choose, which is the risk this whole panel exists to remove.
func (h *HUD) defaultTargetLocked() string {
	best, at := "", int64(0)
	for _, s := range h.sessions {
		if s.Alive && s.FocusedAt > at {
			best, at = s.ID, s.FocusedAt
		}
	}
	return best
}

func (h *HUD) liveLocked(id string) bool {
	for _, s := range h.sessions {
		if s.ID == id {
			return s.Alive
		}
	}
	return false
}

func (h *HUD) isACPLocked(id string) bool {
	for _, s := range h.sessions {
		if s.ID == id {
			return s.Kind == wire.KindACP
		}
	}
	return false
}

// targetsLocked lists every live session, in the menu's order: by
// project, then by the session's own order. Not capped: the menu's row
// pool has a size, the popup does not need one.
func (h *HUD) targetsLocked() []hudTarget {
	projName := make(map[string]string, len(h.projects))
	projOrder := make(map[string]int, len(h.projects))
	for _, p := range h.projects {
		projName[p.ID] = p.Name
		projOrder[p.ID] = p.Order
	}
	live := make([]wire.SessionInfo, 0, len(h.sessions))
	for _, s := range h.sessions {
		if s.Alive {
			live = append(live, s)
		}
	}
	sort.SliceStable(live, func(i, j int) bool {
		pi, iok := projOrder[live[i].ProjectID]
		pj, jok := projOrder[live[j].ProjectID]
		if iok != jok {
			return iok // sessions of a known project first
		}
		if pi != pj {
			return pi < pj
		}
		return live[i].Order < live[j].Order
	})
	out := make([]hudTarget, 0, len(live))
	for _, s := range live {
		row := SessionRow{ID: s.ID, Name: s.Name, Title: s.Title, Alive: true}
		out = append(out, hudTarget{
			ID:    s.ID,
			Label: strings.TrimSpace(row.LabelIn(projName[s.ProjectID])),
			ACP:   s.Kind == wire.KindACP,
		})
	}
	return out
}

func indexOf(ts []hudTarget, id string) int {
	for i, t := range ts {
		if t.ID == id {
			return i
		}
	}
	return -1
}

func (h *HUD) renderLocked(clear, focus bool) {
	ts := h.targetsLocked()
	st := hudState{Targets: ts, Selected: indexOf(ts, h.target), Status: h.status, Hints: hintsEditing}
	switch {
	case !h.connected:
		if st.Status == "" {
			st.Status = "Hive's daemon is not running."
		}
		st.Hints = "[esc] close"
	case len(ts) == 0:
		st.Status = "No live sessions. Start one in Hive first."
		st.Hints = "[esc] close"
	case h.phase == hudBusy:
		st.Hints = hintsBusy
	case st.Selected < 0 && st.Status == "":
		st.Status = "Pick a session to send to with ↑↓."
	}
	st.CanSend = st.Selected >= 0 && h.phase != hudSending
	h.view.Show(st, clear, focus)
}
