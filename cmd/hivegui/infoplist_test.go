package main

import (
	"os"
	"strings"
	"testing"
)

// Every session's processes run under this bundle's Local Network grant.
// Losing the usage string (e.g. Wails regenerating the template) brings
// back an unexplained prompt whose denial breaks LAN access in all sessions.
func TestInfoPlistDeclaresLocalNetworkUsage(t *testing.T) {
	b, err := os.ReadFile("build/darwin/Info.plist")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "<key>NSLocalNetworkUsageDescription</key>") {
		t.Fatal("build/darwin/Info.plist is missing NSLocalNetworkUsageDescription")
	}
}
