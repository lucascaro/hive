// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package main

import (
	"net"
	"time"

	"github.com/lucascaro/hive/internal/buildinfo"
	hdaemon "github.com/lucascaro/hive/internal/daemon"
	"github.com/lucascaro/hive/internal/wire"
)

// dictationDial connects the dictation box's own control connection
// (spec 575). It only dials: unlike dialHandshake it never spawns hived,
// because the box redials for the life of the window and would otherwise
// resurrect a daemon the user just quit, or use up this process's one
// spawn (spawnedHived) before the boot connect could.
func (a *App) dictationDial() (*wire.Client, error) {
	sock := a.socketPath()
	if err := hdaemon.CheckSocketDir(sock); err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout("unix", sock, 2*time.Second)
	if err != nil {
		return nil, err
	}
	return wire.Handshake(conn, wire.Hello{
		Client:  wire.DictationClientPrefix + "0.1",
		BuildID: buildinfo.BuildID(),
		Mode:    wire.ModeControl,
	})
}
