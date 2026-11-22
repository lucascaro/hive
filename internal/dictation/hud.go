// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package dictation

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/lucascaro/hive/internal/agent"
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
//
// With Handy auto-start on (spec 573), opening the panel also starts
// Handy recording (handy.go). Enter while that recording is still going
// stops Handy, waits for its transcription to land in the field, and
// sends it; closing the panel by any route cancels Handy, so a recording
// the HUD started can never outlive it and paste into another app.

// Contract is the DaemonContract generation that added the HUD's
// frames (internal/buildinfo/contract.go, entry 40: DICTATION). An older
// daemon never sends DICTATION, so the hotkey would silently never
// register; the menu says so instead.
const Contract = 40

// View is the panel. Every method must return promptly: HUD calls
// them with its lock held, and the panel's own callbacks arrive on the
// main thread. The AppKit view queues its work onto the main thread and
// returns.
type View interface {
	// Show puts the panel up, or refreshes it if it is up. clear empties
	// the text field, which happens only when the panel opens: a live
	// update must never wipe what the user is dictating. focus makes the
	// panel key with the caret at the end of the text, which only a
	// hotkey press asks for: a live update that took focus would move
	// the caret, or cut off a dictation, mid-sentence.
	Show(st State, clear, focus bool)
	// Hide takes the panel down. restore hands focus back to the app that
	// was in front when the panel opened.
	Hide(restore bool)
}

// Sender delivers confirmed text. It reports false when there is no
// daemon connection to send on.
type Sender interface {
	SendToSession(id, text string, queue bool) bool
}

// State is what the panel shows.
type State struct {
	Targets  []Target
	Selected int // index into Targets, or -1 for none
	Status   string
	Hints    string
	CanSend  bool
}

// Target is one live session the text can go to.
type Target struct {
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
	// hudAwaitingText: Enter stopped a Handy recording, and the send waits
	// for the transcription to land in the field.
	hudAwaitingText
)

// How long Enter-while-recording waits for Handy's transcription, how
// long the field must be quiet before a delivery counts as complete (a
// delivery that types rather than pastes arrives in pieces), and the
// most it waits in all while text keeps arriving.
const (
	handyWait  = 10 * time.Second
	handyQuiet = 300 * time.Millisecond
	handyCap   = 60 * time.Second
)

const (
	statusRecording   = "Recording with Handy…"
	statusAwaiting    = "Waiting for Handy's transcription…"
	statusNoText      = "No transcription arrived from Handy. Nothing was sent."
	statusStartFailed = "Couldn't start Handy. Dictate with its own key."
	statusStopFailed  = "Couldn't stop Handy. Stop it with its own key, then press ↩."
	statusOrphaned    = "Handy may still be recording from last time. Stop it with its own key."
	statusHandyGone   = "Handy quit before it could be stopped. Press ↩ to send what's here."
)

// statusUnanswered is shown while a send the user abandoned still holds
// the session's one reply slot, and cleared when that reply arrives.
const statusUnanswered = "Hive has not answered the last send to this session yet. Try again in a moment."

const (
	hintsEditing = "[↩] send · [⇧↩] new line · [↑↓] target · [esc] cancel"
	hintsBusy    = "[↩] queue it · [⇧↩] new line · [↑↓] target · [esc] cancel"
	hintsRecord  = "[↩] stop & send · [⇧↩] new line · [↑↓] target · [esc] cancel"
	hintsAwait   = "[esc] cancel"
)

// KeyNames are the box's Enter and Shift+Enter as the platform writes
// them. Hints and statuses are written with macOS glyphs; a box on
// another platform renders them with its own names.
type KeyNames struct{ Enter, NewLine string }

var (
	MacKeys = KeyNames{Enter: "↩", NewLine: "⇧↩"}
	PCKeys  = KeyNames{Enter: "Enter", NewLine: "Shift+Enter"}
)

func (k KeyNames) apply(s string) string {
	if k == MacKeys || k == (KeyNames{}) {
		return s
	}
	s = strings.ReplaceAll(s, MacKeys.NewLine, k.NewLine)
	return strings.ReplaceAll(s, MacKeys.Enter, k.Enter)
}

// HUD is the controller. Safe for concurrent use; no method blocks.
type HUD struct {
	view View
	send Sender
	keys KeyNames
	// goSend runs a send off the caller's goroutine, which for every key
	// is the main thread: a socket write must never hold it. Tests run
	// it inline.
	goSend func(func())

	mu        sync.Mutex
	connected bool
	// conn counts connections, so a local send failure that lands after a
	// reconnect cannot free a slot belonging to a send on the new one.
	conn     int
	projects []wire.ProjectInfo
	sessions []wire.SessionInfo
	// pending counts, per session, sends with a reply still due. SENT and
	// ERROR name only the session, and the daemon runs each send on its
	// own goroutine, so replies can come back out of order. Allowing one
	// unanswered send per session makes every reply unambiguous: a send
	// abandoned with Esc blocks a new one to that session until it is
	// answered, rather than its reply settling the new one.
	pending map[string]int

	open   bool
	target string
	// manual records that the user picked the target, so a later focus
	// report does not move it out from under them.
	manual bool
	phase  hudPhase
	status string

	// Handy auto-start (spec 573). handy is nil when this build cannot
	// drive Handy. goHandy runs a Handy call: in production on one serial
	// worker, so calls run in the order they were made and a cancel can
	// never overtake the start it follows. afterFunc is time.AfterFunc,
	// swapped in tests.
	handy     HandyCtl
	goHandy   func(func())
	afterFunc func(time.Duration, func()) (stop func() bool)
	autoStart bool
	// gen changes on every open and close, so a Handy result or timer
	// from an earlier opening cannot touch this one.
	gen int
	// startedHandy: this opening started Handy, so closing it cancels
	// Handy. recording: that recording has not yet been seen to end.
	startedHandy, recording bool
	// orphaned: a close-time cancel failed twice, so a recording may still
	// be going with no HUD tracking it. The next opening does not toggle
	// (that would stop it, not start one) and says so instead.
	orphaned bool
	// shutdownWait bounds how long Shutdown waits for its cancel.
	shutdownWait time.Duration
	// text is the field as last reported. awaitBase is the field when
	// Enter stopped Handy, and awaitStart when; timer is the one pending
	// wait, and timerSeq names it so a stale firing is dropped.
	text, awaitBase string
	awaitStart      time.Time
	timer           func() bool
	timerSeq        int
}

func NewHUD(view View, send Sender, handy HandyCtl, keys KeyNames) *HUD {
	h := &HUD{
		view: view, send: send, handy: handy, keys: keys,
		goSend:       func(f func()) { go f() },
		afterFunc:    func(d time.Duration, f func()) func() bool { return time.AfterFunc(d, f).Stop },
		pending:      map[string]int{},
		shutdownWait: 2*handyTimeout + time.Second,
	}
	h.goHandy = serialWorker()
	return h
}

// serialWorker returns a function that runs what it is given on one
// goroutine, in order. Queuing never blocks: the HUD queues under its
// lock, and the queued calls take that lock to report, so a bounded
// queue behind a hung Handy would deadlock the main thread.
func serialWorker() func(func()) {
	var mu sync.Mutex
	var queue []func()
	wake := make(chan struct{}, 1)
	go func() {
		for range wake {
			for {
				mu.Lock()
				if len(queue) == 0 {
					mu.Unlock()
					break
				}
				f := queue[0]
				queue[0] = nil
				queue = queue[1:]
				mu.Unlock()
				f()
			}
		}
	}()
	return func(f func()) {
		mu.Lock()
		queue = append(queue, f)
		mu.Unlock()
		select {
		case wake <- struct{}{}:
		default: // the worker is already awake and will see it
		}
	}
}

// SetHandyAutoStart is the daemon's handy_auto_start. It takes effect at
// the next opening; a recording already started is left alone.
func (h *HUD) SetHandyAutoStart(on bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.autoStart = on
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
		if h.phase == hudAwaitingText {
			// Enter already stopped Handy; the next ↩ must not toggle it
			// back on.
			h.recording = false
		}
		h.target = ""
		h.stopTimerLocked()
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
	h.conn++
	h.projects, h.sessions = nil, nil
	clear(h.pending) // no reply is coming on a dead connection
	if !h.open {
		return
	}
	if h.startedHandy {
		// Nothing can be sent now, so a transcription would be stranded.
		h.cancelHandyLocked()
		h.status = "Lost Hive's daemon; Handy was stopped."
	}
	if h.phase == hudSending {
		// Said last: whether the text arrived matters more than Handy.
		h.status = "Lost Hive's daemon while sending; the text may have arrived. Check the session before sending again."
	}
	h.target = ""
	h.stopTimerLocked()
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
	h.gen++
	h.manual = false
	h.phase = hudEditing
	h.status = ""
	h.text = ""
	h.target = h.defaultTargetLocked()
	// Only when something could be sent: with no daemon or no live
	// session the microphone would be on for nothing.
	if h.autoStart && h.connected && len(h.targetsLocked()) > 0 &&
		h.handy != nil && h.handy.Available() {
		if h.orphaned {
			h.orphaned = false
			h.status = statusOrphaned
		} else {
			h.startedHandy, h.recording = true, true
			h.status = statusRecording
			h.handyLocked("start", h.handy.Toggle)
		}
	}
	h.renderLocked(true, true)
}

// Shutdown cancels a Handy recording this opening started, and waits for
// the cancel to finish, so one cannot outlive the menu bar when it quits.
// It runs Handy on the caller's goroutine: call it only on the way out.
//
// The cancel goes through the same in-order worker as every other Handy
// call, so it cannot overtake a start still queued, and Shutdown waits at
// most shutdownWait for it. With nothing to cancel it still waits for
// the worker to drain: an Esc just before Quit queued its own cancel,
// possibly behind a start that has not answered yet.
func (h *HUD) Shutdown() {
	h.mu.Lock()
	started, handy, wait := h.startedHandy, h.handy, h.shutdownWait
	h.startedHandy, h.recording = false, false
	h.gen++
	h.mu.Unlock()
	if handy == nil {
		return
	}
	done := make(chan struct{})
	h.goHandy(func() {
		if started {
			_ = handy.Cancel()
		}
		close(done)
	})
	select {
	case <-done:
	case <-time.After(wait):
		log.Printf("hivebar: Handy did not answer --cancel before quitting")
	}
}

// Cancel is Esc: close and send nothing. A reply to a send already in
// flight is then ignored, so it cannot reopen the panel.
func (h *HUD) Cancel() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.open {
		return
	}
	h.closeLocked()
}

// Move steps the target through the live sessions (↑ is -1, ↓ is +1).
func (h *HUD) Move(delta int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.open || h.phase == hudSending || h.phase == hudAwaitingText {
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
	if !h.open || h.phase == hudSending || h.phase == hudAwaitingText {
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
	if h.recording {
		h.status = statusRecording
	}
	h.renderLocked(false, false)
}

// Submit is Enter with the field's text. It sends to the target only,
// and only from a state where a send is allowed; after a busy answer it
// sends again asking the daemon to queue.
//
// While a recording the HUD started is still going, Enter stops Handy
// instead and the send waits for the transcription (TextChanged).
func (h *HUD) Submit(text string) {
	send := h.submit(text)
	if send != nil {
		// Outside the lock: the send's own failure path takes it.
		h.goSend(send)
	}
}

func (h *HUD) submit(text string) func() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.open || h.phase == hudSending || h.phase == hudAwaitingText {
		return nil
	}
	h.text = text
	if !h.recording {
		return h.prepareSendLocked(text)
	}
	if !h.targetReadyLocked() {
		return nil
	}
	h.phase = hudAwaitingText
	h.awaitBase, h.awaitStart = text, time.Now()
	h.status = statusAwaiting
	h.armLocked(handyWait, h.noTextLocked)
	h.handyLocked("stop", h.handy.Toggle)
	h.renderLocked(false, false)
	return nil
}

// TextChanged reports the field after every edit. pasted is true when the
// edit was a paste, which is how Handy delivers by default.
//
// A delivery is a paste or an edit that adds two or more characters at
// once; typing adds one at a time, so a user typing while Handy listens
// is not mistaken for Handy finishing. While a recording is going, a
// delivery means Handy was stopped with its own key. While Enter waits,
// any change is the transcription arriving, sent once the field has been
// quiet for handyQuiet.
func (h *HUD) TextChanged(text string, pasted bool) {
	send := h.textChanged(text, pasted)
	if send != nil {
		h.goSend(send)
	}
}

func (h *HUD) textChanged(text string, pasted bool) func() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.open {
		return nil
	}
	prev := h.text
	h.text = text
	if h.phase == hudAwaitingText {
		// Handy's text is pasted, or typed two or more characters at a
		// time; a stray key is neither, and must not send a half message.
		if text == h.awaitBase ||
			!pasted && utf8.RuneCountInString(text)-utf8.RuneCountInString(h.awaitBase) < 2 {
			return nil
		}
		h.recording = false
		if time.Since(h.awaitStart) >= handyCap {
			return h.deliverLocked()
		}
		h.armLocked(handyQuiet, h.deliverLocked)
		return nil
	}
	if h.recording && (pasted || utf8.RuneCountInString(text)-utf8.RuneCountInString(prev) >= 2) {
		h.recording = false
		if h.status == statusRecording {
			h.status = ""
		}
		h.renderLocked(false, false)
	}
	return nil
}

// deliverLocked sends the transcription Enter was waiting for.
func (h *HUD) deliverLocked() func() {
	h.stopTimerLocked()
	h.phase = hudEditing
	h.status = ""
	send := h.prepareSendLocked(h.text)
	if send == nil {
		h.renderLocked(false, false) // e.g. the field was emptied while waiting
	}
	return send
}

// noTextLocked is the wait running out with nothing delivered. Handy was
// stopped, so there is no recording left to track.
func (h *HUD) noTextLocked() func() {
	h.phase = hudEditing
	h.recording = false
	h.status = statusNoText
	h.renderLocked(false, false)
	return nil
}

// armLocked replaces the pending wait with f after d. f runs under the
// lock and may return a send to run outside it.
func (h *HUD) armLocked(d time.Duration, f func() func()) {
	h.stopTimerLocked()
	h.timerSeq++
	seq := h.timerSeq
	h.timer = h.afterFunc(d, func() {
		h.mu.Lock()
		var send func()
		if h.open && seq == h.timerSeq {
			h.timer = nil
			send = f()
		}
		h.mu.Unlock()
		if send != nil {
			h.goSend(send)
		}
	})
}

func (h *HUD) stopTimerLocked() {
	h.timerSeq++
	if h.timer != nil {
		h.timer()
		h.timer = nil
	}
}

// handyLocked runs a Handy call off the lock and the main thread, in
// order with every other, and reports its outcome to handyResult.
func (h *HUD) handyLocked(op string, call func() error) {
	gen := h.gen
	h.goHandy(func() { h.handyResult(gen, op, call()) })
}

// cancelHandyLocked cancels whatever Handy is doing for this opening.
// --cancel on an idle Handy does nothing, so this need not know whether
// Handy is still listening.
//
// A cancel that fails is tried once more. If that fails too, a recording
// may be left with nothing tracking it, which the next opening says.
func (h *HUD) cancelHandyLocked() {
	if h.startedHandy {
		handy := h.handy
		h.goHandy(func() {
			err := handy.Cancel()
			if err != nil && !errors.Is(err, errHandyNotRunning) {
				err = handy.Cancel()
			}
			if err != nil && !errors.Is(err, errHandyNotRunning) {
				log.Printf("hivebar: cancelling Handy: %v", err)
				h.mu.Lock()
				h.orphaned = true
				h.mu.Unlock()
			}
		})
	}
	h.startedHandy, h.recording = false, false
}

func (h *HUD) handyResult(gen int, op string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err == nil || gen != h.gen || !h.open {
		return
	}
	switch op {
	case "start":
		// startedHandy stays set: a stop-toggle queued behind this failed
		// start may have started a recording, and the close-time cancel is
		// harmless if it did not.
		h.recording = false
		h.status = statusStartFailed
		if h.phase == hudAwaitingText {
			h.stopTimerLocked()
			h.phase = hudEditing
		}
	case "stop":
		if errors.Is(err, errHandyNotRunning) {
			// Handy quit, so there is no recording left to stop or wait
			// for, and retrying would only fail again.
			h.recording = false
			if h.phase == hudAwaitingText {
				h.stopTimerLocked()
				h.phase = hudEditing
			}
			h.status = statusHandyGone
			break
		}
		// Still recording, as far as anyone knows, whatever happened
		// since: a target that ended while waiting cleared the flag on
		// the assumption the stop worked.
		h.recording = true
		if h.phase == hudAwaitingText {
			h.stopTimerLocked()
			h.phase = hudEditing
		}
		h.status = statusStopFailed
	default:
		return
	}
	h.renderLocked(false, false)
}

// targetReadyLocked reports that a send to the target could go now,
// saying why not when it could not.
func (h *HUD) targetReadyLocked() bool {
	if h.target == "" || !h.liveLocked(h.target) {
		h.status = "Pick a session to send to with ↑↓."
		h.renderLocked(false, false)
		return false
	}
	if h.pending[h.target] > 0 {
		h.status = statusUnanswered
		h.renderLocked(false, false)
		return false
	}
	return true
}

// prepareSendLocked validates a send of text and moves to hudSending,
// returning the send to run, or nil when nothing is to be sent.
func (h *HUD) prepareSendLocked(text string) func() {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	if len(text) > wire.MaxMessageLen {
		h.status = fmt.Sprintf("Too long to send: the limit is %d KiB.", wire.MaxMessageLen>>10)
		h.renderLocked(false, false)
		return nil
	}
	if !h.targetReadyLocked() {
		return nil
	}
	queue := h.phase == hudBusy
	id, conn := h.target, h.conn
	h.pending[id]++
	h.phase = hudSending
	h.status = "Sending…"
	h.renderLocked(false, false)
	return func() {
		if !h.send.SendToSession(id, text, queue) {
			h.failed(id, conn, "Hive's daemon is not running.")
		}
	}
}

func (h *HUD) failed(id string, conn int, msg string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if conn != h.conn {
		return // a disconnect since then already freed the slot and said so
	}
	h.answeredLocked(id) // never written, so no reply is coming
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
	if !h.answeredLocked(id) {
		return
	}
	if !h.open || h.phase != hudSending || h.target != id {
		h.clearUnansweredLocked(id)
		return
	}
	h.closeLocked()
}

// OnError is a SEND_TO_SESSION refusal for session id.
func (h *HUD) OnError(id, code, msg string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.answeredLocked(id) {
		return
	}
	if !h.open || h.phase != hudSending || h.target != id {
		h.clearUnansweredLocked(id)
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

// answeredLocked records a reply (or a local failure) for id's one
// outstanding send, reporting false when none was outstanding: a reply
// to nothing of ours.
func (h *HUD) answeredLocked(id string) bool {
	if h.pending[id] == 0 {
		return false
	}
	if h.pending[id]--; h.pending[id] == 0 {
		delete(h.pending, id)
	}
	return true
}

// clearUnansweredLocked drops the "not answered yet" status once the
// reply it was waiting on for the target has come.
func (h *HUD) clearUnansweredLocked(id string) {
	if h.open && h.target == id && h.status == statusUnanswered {
		h.status = ""
		h.renderLocked(false, false)
	}
}

func (h *HUD) closeLocked() {
	h.cancelHandyLocked()
	h.stopTimerLocked()
	h.gen++
	h.text = ""
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
func (h *HUD) targetsLocked() []Target {
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
	out := make([]Target, 0, len(live))
	for _, s := range live {
		label := Label(projName[s.ProjectID], s.ID, s.Name, s.Title)
		if s.Kind != wire.KindACP && (s.Agent == "" || s.Agent == string(agent.IDShell)) {
			// Dictated text lands in a shell as a command, not a prompt.
			label = "[shell] " + label
		}
		out = append(out, Target{
			ID:    s.ID,
			Label: label,
			ACP:   s.Kind == wire.KindACP,
		})
	}
	return out
}

func indexOf(ts []Target, id string) int {
	for i, t := range ts {
		if t.ID == id {
			return i
		}
	}
	return -1
}

func (h *HUD) renderLocked(clear, focus bool) {
	ts := h.targetsLocked()
	st := State{Targets: ts, Selected: indexOf(ts, h.target), Status: h.status, Hints: hintsEditing}
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
	case h.phase == hudAwaitingText:
		st.Hints = hintsAwait
	case h.recording:
		st.Hints = hintsRecord
	case st.Selected < 0 && st.Status == "":
		st.Status = "Pick a session to send to with ↑↓."
	}
	st.CanSend = st.Selected >= 0 && h.phase != hudSending && h.phase != hudAwaitingText
	st.Status, st.Hints = h.keys.apply(st.Status), h.keys.apply(st.Hints)
	h.view.Show(st, clear, focus)
}
