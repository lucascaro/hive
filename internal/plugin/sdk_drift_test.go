package plugin

import (
	"bytes"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/wire"
)

// The SDK is hand-written JavaScript mirroring the Go wire types. These
// tests are what keep it honest: a new frame or a new field on a struct
// the SDK documents fails here until the SDK learns it.

const sdkPath = "../../plugins/sdk/hive-plugin.mjs"

func readSDK(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(sdkPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSDKFrameTableMatchesWire(t *testing.T) {
	sdk := readSDK(t)
	start := strings.Index(sdk, "export const Frame = Object.freeze({")
	end := strings.Index(sdk[start:], "});")
	if start < 0 || end < 0 {
		t.Fatal("Frame table not found in the SDK")
	}
	table := sdk[start : start+end]
	got := map[string]byte{}
	for _, m := range regexp.MustCompile(`(?m)^\s+([A-Z_]+): 0x([0-9a-f]{2}),`).FindAllStringSubmatch(table, -1) {
		v, _ := strconv.ParseUint(m[2], 16, 8)
		got[m[1]] = byte(v)
	}
	want := map[string]byte{}
	for b := 0; b < 256; b++ {
		name := wire.FrameType(b).String()
		if !strings.HasPrefix(name, "UNKNOWN") {
			want[name] = byte(b)
		}
	}
	for name, b := range want {
		if g, ok := got[name]; !ok || g != b {
			t.Errorf("SDK Frame.%s = 0x%02x (present %v), wire has 0x%02x", name, g, ok, b)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("SDK Frame.%s does not exist in the wire package", name)
		}
	}
}

func TestSDKTypedefsMatchWireStructs(t *testing.T) {
	sdk := readSDK(t)
	for typedef, v := range map[string]any{
		"Hello":         wire.Hello{},
		"Welcome":       wire.Welcome{},
		"HiveError":     wire.Error{},
		"SessionInfo":   wire.SessionInfo{},
		"SessionEvent":  wire.SessionEvent{},
		"ProjectInfo":   wire.ProjectInfo{},
		"ProjectEvent":  wire.ProjectEvent{},
		"PluginInfo":    wire.PluginInfo{},
		"PluginEvent":   wire.PluginEvent{},
		"ClientCommand": wire.ClientCommand{},
	} {
		block := typedefBlock(sdk, typedef)
		if block == "" {
			t.Errorf("SDK has no @typedef %s", typedef)
			continue
		}
		rt := reflect.TypeOf(v)
		for i := 0; i < rt.NumField(); i++ {
			tag := strings.Split(rt.Field(i).Tag.Get("json"), ",")[0]
			if tag == "" || tag == "-" {
				continue
			}
			if !regexp.MustCompile(`@property \{[^}]*\}+ \[?` + regexp.QuoteMeta(tag) + `\]?\s`).MatchString(block) {
				t.Errorf("SDK @typedef %s is missing %s (wire.%s.%s)", typedef, tag, rt.Name(), rt.Field(i).Name)
			}
		}
	}
}

// typedefBlock returns the JSDoc comment that declares @typedef name.
func typedefBlock(sdk, name string) string {
	i := strings.Index(sdk, "@typedef {object} "+name+"\n")
	if i < 0 {
		return ""
	}
	end := strings.Index(sdk[i:], "*/")
	return sdk[i : i+end]
}

// The reference plugin vendors the SDK, as every plugin is told to. The
// copy must be the SDK, byte for byte.
func TestWebhookVendorsCurrentSDK(t *testing.T) {
	sdk, err := os.ReadFile(sdkPath)
	if err != nil {
		t.Fatal(err)
	}
	vendored, err := os.ReadFile("../../plugins/webhook/hive-plugin.mjs")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sdk, vendored) {
		t.Fatal("plugins/webhook/hive-plugin.mjs differs from plugins/sdk/hive-plugin.mjs; copy the SDK over it")
	}
}

// The reference plugin's manifest must load in this build.
func TestWebhookManifestLoads(t *testing.T) {
	m, err := LoadManifest("../../plugins/webhook")
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "webhook" || m.Main.Command[0] != "node" {
		t.Fatalf("webhook manifest = %+v", m)
	}
}
