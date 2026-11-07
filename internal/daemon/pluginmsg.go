// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package daemon

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/lucascaro/hive/internal/wire"
)

// PLUGIN_MESSAGE (spec 528): a two-way side channel between a plugin and
// the GUI.
//
//	plugin → every UI-capable user control client (the GUI, the ws-bridge)
//	UI     → the named plugin's live control connection(s)
//
// A plugin's message is stamped with its own id, whatever the frame
// claims, so a plugin cannot speak as another. It never reaches other
// plugins, hivebar or a session's events socket. It adds no privilege:
// anything the GUI can ask of a plugin the user can already do, and the
// plugin already runs with the user's full access (docs/plugins.md).

// pluginMsgBuffer is each subscriber's queue. Like every fan-out here a
// subscriber that fills it is dropped and its connection hung up, so a
// stuck GUI costs it a reconnect and a resync (the plugin replays state
// on request), never a stall in the sender.
const pluginMsgBuffer = 32

type pluginMsgHub struct {
	mu      sync.Mutex
	ui      map[chan wire.PluginMessage]struct{}
	plugins map[string]map[chan wire.PluginMessage]struct{}
	closed  bool
}

func newPluginMsgHub() *pluginMsgHub {
	return &pluginMsgHub{
		ui:      make(map[chan wire.PluginMessage]struct{}),
		plugins: make(map[string]map[chan wire.PluginMessage]struct{}),
	}
}

// subscribe adds a queue to set (h.mu held by the caller via the two
// wrappers below) and returns it with its cleanup. After Close it hands
// back a closed channel, like commandHub.Subscribe.
func (h *pluginMsgHub) subscribe(set func() map[chan wire.PluginMessage]struct{}) (chan wire.PluginMessage, func()) {
	ch := make(chan wire.PluginMessage, pluginMsgBuffer)
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		close(ch)
		return ch, func() {}
	}
	m := set()
	m[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := m[ch]; ok {
			delete(m, ch)
			close(ch)
		}
	}
}

// subscribeUI registers a UI-capable user control connection.
func (h *pluginMsgHub) subscribeUI() (chan wire.PluginMessage, func()) {
	return h.subscribe(func() map[chan wire.PluginMessage]struct{} { return h.ui })
}

// subscribePlugin registers a control connection of plugin id.
func (h *pluginMsgHub) subscribePlugin(id string) (chan wire.PluginMessage, func()) {
	return h.subscribe(func() map[chan wire.PluginMessage]struct{} {
		if h.plugins[id] == nil {
			h.plugins[id] = make(map[chan wire.PluginMessage]struct{})
		}
		return h.plugins[id]
	})
}

// send offers m to every queue in set, dropping any that is full.
// Callers hold h.mu.
func (h *pluginMsgHub) send(set map[chan wire.PluginMessage]struct{}, m wire.PluginMessage) {
	for ch := range set {
		select {
		case ch <- m:
		default:
			log.Printf("daemon: dropping slow PLUGIN_MESSAGE listener (buffer %d full); closing the channel, so the daemon hangs up on that client (it missed topic %q from %q) and it reconnects",
				cap(ch), m.Topic, m.PluginID)
			delete(set, ch)
			close(ch)
		}
	}
}

// publishUI relays a plugin's message to every UI-capable client.
func (h *pluginMsgHub) publishUI(m wire.PluginMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.send(h.ui, m)
}

// publishPlugin relays a UI's message to plugin id's connections. It
// reports false when the plugin has none.
func (h *pluginMsgHub) publishPlugin(id string, m wire.PluginMessage) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	set := h.plugins[id]
	if len(set) == 0 {
		return false
	}
	h.send(set, m)
	return true
}

// pressure is how full the fullest queue is, 0 to 1; see
// registry.Pressure.
func (h *pluginMsgHub) pressure() float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	p := 0.0
	scan := func(set map[chan wire.PluginMessage]struct{}) {
		for ch := range set {
			p = max(p, float64(len(ch))/float64(cap(ch)))
		}
	}
	scan(h.ui)
	for _, set := range h.plugins {
		scan(set)
	}
	return p
}

// Close drops every subscriber. Idempotent.
func (h *pluginMsgHub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for ch := range h.ui {
		delete(h.ui, ch)
		close(ch)
	}
	for id, set := range h.plugins {
		for ch := range set {
			delete(set, ch)
			close(ch)
		}
		delete(h.plugins, id)
	}
}

// pluginMessageFits enforces the cap on the frame as it will be relayed:
// stamped, and re-encoded (JSON escaping can grow it), not as received.
func pluginMessageFits(ops controlOps, m wire.PluginMessage) bool {
	b, err := json.Marshal(m)
	if err != nil || len(b) > wire.MaxPluginMessage {
		ops.sendError(wire.ErrCodePluginMessageTooLarge,
			fmt.Sprintf("PLUGIN_MESSAGE is %d bytes as relayed; the cap is %d", len(b), wire.MaxPluginMessage))
		return false
	}
	return true
}

// relayPluginMessage handles one PLUGIN_MESSAGE frame from ops's
// connection. A session connection never gets here (it is not in
// sessionModeFrames).
func (d *Daemon) relayPluginMessage(ops controlOps, payload []byte) {
	// Cheap bound before decoding anything.
	if len(payload) > wire.MaxPluginMessage+1024 {
		ops.sendError(wire.ErrCodePluginMessageTooLarge,
			fmt.Sprintf("PLUGIN_MESSAGE is %d bytes; the cap is %d", len(payload), wire.MaxPluginMessage))
		return
	}
	m, ok := decodeReq[wire.PluginMessage](payload, ops.sendError)
	if !ok {
		return
	}
	if !wire.ValidPluginTopic(m.Topic) {
		ops.sendError(wire.ErrCodeInvalidPluginMessage, fmt.Sprintf("topic %q must match [a-z][a-z0-9._-]{0,63}", m.Topic))
		return
	}
	switch {
	case ops.pluginID != "":
		// Whatever it claimed, it is who it is.
		m.PluginID = ops.pluginID
		if !pluginMessageFits(ops, m) {
			return
		}
		d.pluginMsgs.publishUI(m)
	case ops.uiCapable:
		if !pluginMessageFits(ops, m) {
			return
		}
		if !d.pluginMsgs.publishPlugin(m.PluginID, m) {
			ops.sendError(wire.ErrCodePluginNotRunning, fmt.Sprintf("plugin %q has no live connection", m.PluginID))
		}
	default:
		ops.sendError(wire.ErrCodeModeNotAllowed, "PLUGIN_MESSAGE is sent by a plugin or by a client that shows UI")
	}
}
