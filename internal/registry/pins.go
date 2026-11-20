// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package registry

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/lucascaro/hive/internal/wire"
)

// Pinned responses (spec 566). A session's pins live beside its
// session.json, in sessions/<id>/pins/, two files per pin: <pid>.json
// is the header (wire.Pin) and <pid>.md the body. The body is written
// first and the header last, so a header on disk means a whole pin;
// only headers are read at boot, and a body is read when asked for.
//
// Pins live on the Entry, never in the agent-state machine: that is
// replaced on restart and revive, and a pin must outlive both. A close
// or an archive carries them to <closed|archived>/<id>.pins/ beside the
// record, a restore carries them back, and discardRecord deletes them
// with the record.
//
// Locking. pinSet.mu guards one session's pins and their directory.
// The rule is that r.mu is never held while pinSet.mu is taken: every
// path looks the entry up under r.mu, releases it, and only then locks
// the set. Holding pinSet.mu while taking r.mu is allowed (restore does
// it on a set nobody else can reach yet), so the order can never form
// a cycle. Entry.Info reads the count through an atomic for the same
// reason: it runs under r.mu.

var (
	// ErrPinInvalid is an empty title or body, or text that is not UTF-8.
	ErrPinInvalid = errors.New("registry: pin needs a non-empty UTF-8 title and body")
	// ErrPinTooLong is a title or body over its wire bound.
	ErrPinTooLong = errors.New("registry: pin is too long")
	// ErrNoSuchPin is a pin id that is not in that session.
	ErrNoSuchPin = errors.New("registry: no such pin")
)

// pinSet is one session's pins.
type pinSet struct {
	mu sync.Mutex
	// headers is every pin, newest first. Guarded by mu.
	headers []wire.Pin
	// closed is set once the session is gone (killed or archived):
	// every later add or removal is refused, so nothing can write into
	// a directory the close is moving or has removed. Guarded by mu.
	closed bool
	// lastID is the highest id issued, so ids stay strictly increasing
	// even within one clock tick. Guarded by mu.
	lastID string
	// count mirrors len(headers) for Entry.Info, which runs under r.mu
	// and so may not take mu.
	count atomic.Int32
}

func (p *pinSet) setHeadersLocked(h []wire.Pin) {
	p.headers = h
	p.count.Store(int32(len(h)))
}

// pinsOf returns e's pin set, creating it on first use.
func (e *Entry) pinsOf() *pinSet {
	if p := e.pins.Load(); p != nil {
		return p
	}
	e.pins.CompareAndSwap(nil, &pinSet{})
	return e.pins.Load()
}

// pinCount is the number of pins, for Info.
func (e *Entry) pinCount() int {
	if p := e.pins.Load(); p != nil {
		return int(p.count.Load())
	}
	return 0
}

func (r *Registry) pinsDir(id string) string {
	return filepath.Join(SessionsDir(r.stateDir), id, "pins")
}

// pinRecordDir is where a closed or archived session's pins wait,
// beside its record in dir.
func pinRecordDir(dir, id string) string {
	return filepath.Join(dir, id+".pins")
}

// validPinID reports whether id has the shape newPinID makes. Checked
// before any id from a client is used in a path.
func validPinID(id string) bool {
	if len(id) != 24 {
		return false
	}
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// newPinID is 16 hex digits of the time in nanoseconds and 8 random,
// bumped past last so ids sort in issue order.
func newPinID(last string) string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	id := fmt.Sprintf("%016x%08x", uint64(time.Now().UnixNano()), binary.BigEndian.Uint32(b[:]))
	if id <= last {
		var n uint64
		_, _ = fmt.Sscanf(last[:16], "%016x", &n)
		id = fmt.Sprintf("%016x%08x", n+1, binary.BigEndian.Uint32(b[:]))
	}
	return id
}

// checkPin validates an agent's pin at the trust boundary.
func checkPin(title, body string) error {
	if strings.TrimSpace(title) == "" || strings.TrimSpace(body) == "" ||
		!utf8.ValidString(title) || !utf8.ValidString(body) {
		return ErrPinInvalid
	}
	if len(title) > wire.MaxPinTitleLen {
		return fmt.Errorf("%w: title is %d bytes, the limit is %d", ErrPinTooLong, len(title), wire.MaxPinTitleLen)
	}
	if len(body) > wire.MaxPinBodyLen {
		return fmt.Errorf("%w: body is %d bytes, the limit is %d", ErrPinTooLong, len(body), wire.MaxPinBodyLen)
	}
	return nil
}

// entryForPins looks the entry up under r.mu and releases it.
func (r *Registry) entryForPins(id string) (*Entry, error) {
	r.mu.Lock()
	e, ok := r.entries[id]
	r.mu.Unlock()
	if !ok {
		return nil, ErrNotFound
	}
	return e, nil
}

// AddPin pins a response to session id and returns its header.
func (r *Registry) AddPin(id, title, body string) (wire.Pin, error) {
	if err := checkPin(title, body); err != nil {
		return wire.Pin{}, err
	}
	e, err := r.entryForPins(id)
	if err != nil {
		return wire.Pin{}, err
	}
	return r.addPin(e, title, body)
}

// addPin is AddPin on an entry already looked up — possibly one a close
// has taken out of the map since, which is the race pinSet.closed exists
// for.
func (r *Registry) addPin(e *Entry, title, body string) (wire.Pin, error) {
	id := e.ID
	p := e.pinsOf()
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return wire.Pin{}, ErrNotFound
	}
	pin := wire.Pin{
		ID:        newPinID(p.lastID),
		Title:     title,
		CreatedAt: time.Now().UnixMilli(),
		BodyLen:   len(body),
	}
	dir := r.pinsDir(id)
	if err := writeAtomic(filepath.Join(dir, pin.ID+".md"), []byte(body)); err != nil {
		p.mu.Unlock()
		return wire.Pin{}, fmt.Errorf("registry: write pin: %w", err)
	}
	if err := writeJSON(filepath.Join(dir, pin.ID+".json"), pin); err != nil {
		_ = os.Remove(filepath.Join(dir, pin.ID+".md"))
		p.mu.Unlock()
		return wire.Pin{}, fmt.Errorf("registry: write pin: %w", err)
	}
	p.lastID = pin.ID
	p.setHeadersLocked(append([]wire.Pin{pin}, p.headers...))
	r.announcePins(e, wire.PinsChangedMsg{SessionID: id, Added: &pin})
	p.mu.Unlock()
	return pin, nil
}

// Pins returns a copy of session id's pin headers, newest first.
func (r *Registry) Pins(id string) ([]wire.Pin, error) {
	e, err := r.entryForPins(id)
	if err != nil {
		return nil, err
	}
	p := e.pinsOf()
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.headers), nil
}

// PinBody returns one pin's body. A pin of another session is
// ErrNoSuchPin, exactly like one that does not exist.
func (r *Registry) PinBody(id, pinID string) (string, error) {
	if !validPinID(pinID) {
		return "", ErrNoSuchPin
	}
	e, err := r.entryForPins(id)
	if err != nil {
		return "", err
	}
	p := e.pinsOf()
	p.mu.Lock()
	defer p.mu.Unlock()
	if !slices.ContainsFunc(p.headers, func(h wire.Pin) bool { return h.ID == pinID }) {
		return "", ErrNoSuchPin
	}
	b, err := os.ReadFile(filepath.Join(r.pinsDir(id), pinID+".md"))
	if err != nil {
		return "", fmt.Errorf("registry: read pin: %w", err)
	}
	return string(b), nil
}

// Unpin removes one pin from session id.
func (r *Registry) Unpin(id, pinID string) error {
	if !validPinID(pinID) {
		return ErrNoSuchPin
	}
	e, err := r.entryForPins(id)
	if err != nil {
		return err
	}
	p := e.pinsOf()
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return ErrNotFound
	}
	i := slices.IndexFunc(p.headers, func(h wire.Pin) bool { return h.ID == pinID })
	if i < 0 {
		p.mu.Unlock()
		return ErrNoSuchPin
	}
	r.removePinFiles(id, pinID)
	p.setHeadersLocked(slices.Delete(slices.Clone(p.headers), i, i+1))
	r.announcePins(e, wire.PinsChangedMsg{SessionID: id, RemovedIDs: []string{pinID}})
	p.mu.Unlock()
	return nil
}

// UnpinAll removes every pin from session id.
func (r *Registry) UnpinAll(id string) error {
	e, err := r.entryForPins(id)
	if err != nil {
		return err
	}
	p := e.pinsOf()
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return ErrNotFound
	}
	for _, h := range p.headers {
		r.removePinFiles(id, h.ID)
	}
	p.setHeadersLocked(nil)
	r.announcePins(e, wire.PinsChangedMsg{SessionID: id, Cleared: true})
	p.mu.Unlock()
	return nil
}

// removePinFiles deletes one pin, header first, so a crash part way
// leaves at worst a body with no header, which loadPinHeaders sweeps.
func (r *Registry) removePinFiles(id, pinID string) {
	dir := r.pinsDir(id)
	for _, name := range []string{pinID + ".json", pinID + ".md"} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
			log.Printf("registry: session %s: remove pin %s: %v", id, name, err)
		}
	}
}

// announcePins tells clients about a change: the delta, and the
// session's new pin count — but only while e is still the live entry,
// so a change racing a close never resurrects a removed row. Callers
// hold p.mu (pinSet.mu before r.mu, the allowed order), so concurrent
// changes publish in the order they were made.
func (r *Registry) announcePins(e *Entry, msg wire.PinsChangedMsg) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.entries[e.ID] != e {
		return
	}
	r.broadcastLocked(wire.SessionEventUpdated, e.Info())
	for ch := range r.pinsListeners {
		select {
		case ch <- msg:
		default:
			log.Printf("registry: dropping slow pins listener (buffer %d full); closing the channel, so the daemon hangs up on that client and it reconnects", cap(ch))
			delete(r.pinsListeners, ch)
			close(ch)
		}
	}
}

// PinsListener receives PINS_CHANGED deltas.
type PinsListener chan wire.PinsChangedMsg

// SubscribePins returns a channel of every pins delta. The cleanup
// function unsubscribes and closes it.
func (r *Registry) SubscribePins() (PinsListener, func()) {
	ch := make(PinsListener, 64)
	r.mu.Lock()
	if r.pinsListeners == nil {
		r.mu.Unlock()
		close(ch)
		return ch, func() {}
	}
	r.pinsListeners[ch] = struct{}{}
	r.mu.Unlock()
	return ch, func() {
		r.mu.Lock()
		if _, ok := r.pinsListeners[ch]; ok {
			delete(r.pinsListeners, ch)
			close(ch)
		}
		r.mu.Unlock()
	}
}

// loadPinHeaders reads every header in dir, newest first. A body whose
// header is missing is a pin that never finished being written, or one
// half-removed; it is deleted.
func loadPinHeaders(dir string) []wire.Pin {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	// have is every header file present, readable or not: an
	// unreadable header keeps its body, so restoring access recovers it.
	have := map[string]bool{}
	var out []wire.Pin
	for _, d := range ents {
		name := d.Name()
		id, ok := strings.CutSuffix(name, ".json")
		if !ok || !validPinID(id) {
			continue
		}
		have[id] = true
		var h wire.Pin
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || json.Unmarshal(b, &h) != nil || h.ID != id {
			log.Printf("registry: skipping unreadable pin %s", filepath.Join(dir, name))
			continue
		}
		out = append(out, h)
	}
	for _, d := range ents {
		if id, ok := strings.CutSuffix(d.Name(), ".md"); ok && validPinID(id) && !have[id] {
			_ = os.Remove(filepath.Join(dir, d.Name()))
		}
	}
	slices.SortFunc(out, func(a, b wire.Pin) int { return strings.Compare(b.ID, a.ID) })
	return out
}

// loadPins fills e's pins from disk at boot.
func (r *Registry) loadPins(e *Entry) {
	h := loadPinHeaders(r.pinsDir(e.ID))
	if len(h) == 0 {
		return
	}
	p := e.pinsOf()
	p.mu.Lock()
	p.setHeadersLocked(h)
	p.lastID = h[0].ID
	p.mu.Unlock()
}

// closePins runs once kill is past its point of no return (the entry
// is out of r.entries): it refuses every later change and moves the
// pins beside the record in recordDir, or leaves them to be removed
// with the session directory when recordDir is "".
func (r *Registry) closePins(e *Entry, recordDir string) {
	p := e.pinsOf()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	if recordDir == "" {
		return
	}
	src, dst := r.pinsDir(e.ID), pinRecordDir(recordDir, e.ID)
	if _, err := os.Stat(src); err != nil {
		return
	}
	_ = os.RemoveAll(dst)
	if err := os.Rename(src, dst); err != nil {
		log.Printf("registry: close %s: could not keep its pins: %v", e.ID, err)
	}
}

// pinsForRestore reads a record's pins and returns a set that is
// already locked, for an entry not yet in the map: finishRestorePins
// moves the files and unlocks it once the entry is visible. Nothing
// else can reach the set before then, so holding it across r.mu is
// safe (see the locking note at the top of this file).
func pinsForRestore(recordDir, id string) *pinSet {
	p := &pinSet{}
	p.mu.Lock()
	h := loadPinHeaders(pinRecordDir(recordDir, id))
	p.setHeadersLocked(h)
	if len(h) > 0 {
		p.lastID = h[0].ID
	}
	return p
}

// finishRestorePins moves a restored entry's pins back under its
// session directory and unlocks the set pinsForRestore returned.
func (r *Registry) finishRestorePins(p *pinSet, recordDir, id string) {
	defer p.mu.Unlock()
	src := pinRecordDir(recordDir, id)
	if _, err := os.Stat(src); err != nil {
		return
	}
	dst := r.pinsDir(id)
	_ = os.RemoveAll(dst)
	if err := os.Rename(src, dst); err != nil {
		log.Printf("registry: restore %s: could not bring back its pins: %v", id, err)
		p.setHeadersLocked(nil)
	}
}

// sweepOrphanPinRecords removes <id>.pins directories in dir whose
// record is gone — left by a crash between a discard's two removals.
func sweepOrphanPinRecords(dir string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, d := range ents {
		id, ok := strings.CutSuffix(d.Name(), ".pins")
		if !ok || !d.IsDir() || !validSessionID(id) {
			continue
		}
		if _, err := os.Stat(recordPath(dir, id)); os.IsNotExist(err) {
			_ = os.RemoveAll(filepath.Join(dir, d.Name()))
		}
	}
}
