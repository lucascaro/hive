// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package agent

import (
	"os"
	"regexp"
	"strconv"
	"testing"

	"github.com/lucascaro/hive/internal/wire"
)

// The Pi extension hand-writes the frame numbers it speaks
// (`const FRAME_ADD_PIN = 0x5a;`). Every one must be the wire's frame of
// the same name: a renumbered frame would otherwise break Pi silently,
// because the daemon would read the bytes as some other frame.
func TestPiExtensionFrameConstantsMatchWire(t *testing.T) {
	src, err := os.ReadFile("pi/hive.ts")
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]wire.FrameType{}
	for i := range 256 {
		ft := wire.FrameType(i)
		byName[ft.String()] = ft
	}
	re := regexp.MustCompile(`const FRAME_([A-Z_]+) = (0x[0-9a-fA-F]+);`)
	matches := re.FindAllStringSubmatch(string(src), -1)
	if len(matches) == 0 {
		t.Fatal("no FRAME_ constants found in pi/hive.ts")
	}
	for _, m := range matches {
		v, err := strconv.ParseUint(m[2], 0, 8)
		if err != nil {
			t.Fatalf("FRAME_%s = %s: %v", m[1], m[2], err)
		}
		want, ok := byName[m[1]]
		if !ok {
			t.Errorf("FRAME_%s: no wire frame of that name", m[1])
			continue
		}
		if wire.FrameType(v) != want {
			t.Errorf("FRAME_%s = %#x, wire has %#x", m[1], v, byte(want))
		}
	}
}
