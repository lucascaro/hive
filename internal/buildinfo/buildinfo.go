// Package buildinfo holds build identity strings populated at link
// time via -ldflags. Both hived and hivegui import this so the
// version-handshake banner can detect a stale daemon (built from a
// different commit than the GUI talking to it).
package buildinfo

// BuildID is set at link time, e.g.
//
//	go build -ldflags "-X github.com/lucascaro/hive/internal/buildinfo.BuildID=$(git rev-parse --short HEAD)"
//
// Default "dev" identifies an unstamped local build.
var BuildID = "dev"
