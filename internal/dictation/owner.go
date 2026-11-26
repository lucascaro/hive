// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package dictation

import (
	"log"

	"github.com/lucascaro/hive/internal/wire"
)

// Owner decides which of several processes holds the dictation chord.
//
// On Windows every Hive window is its own process, and each would
// register the chord. The OS lets only one: RegisterHotKey fails for the
// rest, which is the whole election. A loser keeps retrying on a timer,
// so when the owning window closes (and the OS frees its registration)
// another takes over. One press therefore opens exactly one box.
//
// Owner is the bookkeeping, with the OS calls injected so it can be
// tested; it is not safe for concurrent use. The Windows host calls it
// only from the thread that owns its window, because RegisterHotKey is
// thread-affine.
type Owner struct {
	register   func(wire.GlobalHotkey) bool // true: the OS took it
	unregister func()
	// onResult reports each registration attempt's outcome. May be nil.
	onResult func(h wire.GlobalHotkey, ok bool)

	want     wire.GlobalHotkey
	held     bool
	failures int
}

// ownerQuietAfter is how many failed tries in a row are logged once: a
// chord another app holds looks the same as one another Hive window
// holds, and only the second is normal.
const ownerQuietAfter = 30

func NewOwner(register func(wire.GlobalHotkey) bool, unregister func(), onResult func(wire.GlobalHotkey, bool)) *Owner {
	return &Owner{register: register, unregister: unregister, onResult: onResult}
}

// Want sets the chord to hold. A disabled chord releases it.
func (o *Owner) Want(h wire.GlobalHotkey) {
	if h == o.want && (o.held || !h.Enabled) {
		return
	}
	if o.held {
		o.unregister()
		o.held = false
	}
	o.want, o.failures = h, 0
	o.try()
}

// Tick retries a chord this process does not hold yet.
func (o *Owner) Tick() {
	if o.Retrying() {
		o.try()
	}
}

// Retrying reports that a chord is wanted and not held, so the host
// should keep its retry timer running.
func (o *Owner) Retrying() bool { return o.want.Enabled && !o.held }

// Held reports that this process holds the chord.
func (o *Owner) Held() bool { return o.held }

// Release lets the chord go, for shutdown.
func (o *Owner) Release() {
	if o.held {
		o.unregister()
		o.held = false
	}
	o.want = wire.GlobalHotkey{}
}

func (o *Owner) try() {
	if !o.want.Enabled {
		return
	}
	o.held = o.register(o.want)
	if o.held {
		o.failures = 0
	} else {
		o.failures++
		if o.failures == ownerQuietAfter {
			log.Printf("dictation: hotkey still not registered after %d tries; another Hive window or another app holds it", o.failures)
		}
	}
	if o.onResult != nil && (o.held || o.failures == 1) {
		o.onResult(o.want, o.held)
	}
}
