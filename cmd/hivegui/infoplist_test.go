package main

import (
	"os"
	"strings"
	"testing"
)

// Every session's processes run under the Local Network grant of whichever
// bundle spawned hived: the GUI, or hivebar's Restart Daemon. Losing the
// usage string from either (e.g. Wails regenerating the template) brings
// back an unexplained prompt whose denial breaks LAN access in all sessions.
func TestInfoPlistDeclaresLocalNetworkUsage(t *testing.T) {
	for _, p := range []string{
		"build/darwin/Info.plist",
		"../hivebar/build/darwin/Info.plist",
	} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), "<key>NSLocalNetworkUsageDescription</key>") {
			t.Errorf("%s is missing NSLocalNetworkUsageDescription", p)
		}
	}
}
