// Package plugins holds the plugins that ship inside Hive. Only the
// directories embedded below are bundled; the rest of this tree
// (examples, the SDK) is installed by hand or vendored.
package plugins

import "embed"

// Builtin is every bundled plugin, one top-level directory per id.
// hived materializes it into its state dir on every start
// (internal/plugin.Config.Builtin).
//
//go:embed all:plan-review
var Builtin embed.FS
