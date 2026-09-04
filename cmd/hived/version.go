package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/lucascaro/hive/internal/buildinfo"
)

// printIdentity writes this daemon's build identity to w.
//
// The JSON form is not a convenience: it is how a GUI reads the
// daemon contract of a build it is NOT talking to. The updater stages
// a new bundle and has to decide, before applying it, whether the
// update needs a cheap GUI reload or a full restart that ends every
// session. The only way to ask that question of a binary on disk is
// to run it — so this path must stay cheap, side-effect free, and
// must never touch the socket or the state dir.
func printIdentity(w io.Writer, asJSON bool) {
	id := buildinfo.CurrentIdentity()
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(id)
		return
	}
	fmt.Fprintf(w, "hived %s (build %s, daemon contract %d)\n",
		id.Release, id.BuildID, id.DaemonContract)
}
