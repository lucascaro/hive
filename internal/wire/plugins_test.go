package wire

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// The frame bytes are the protocol's identity — reassigning one
// silently reinterprets an old client's frames as a different command.
func TestPluginFrameTypeValues(t *testing.T) {
	cases := map[FrameType]struct {
		b    byte
		name string
	}{
		FrameListPlugins:      {0x37, "LIST_PLUGINS"},
		FramePlugins:          {0x38, "PLUGINS"},
		FrameInstallPlugin:    {0x39, "INSTALL_PLUGIN"},
		FrameSetPluginEnabled: {0x3a, "SET_PLUGIN_ENABLED"},
		FrameRemovePlugin:     {0x3b, "REMOVE_PLUGIN"},
		FramePluginEvent:      {0x3c, "PLUGIN_EVENT"},
	}
	for ft, want := range cases {
		if byte(ft) != want.b {
			t.Errorf("%s = 0x%02x, want 0x%02x", want.name, byte(ft), want.b)
		}
		if ft.String() != want.name {
			t.Errorf("String() = %q, want %q", ft.String(), want.name)
		}
	}
}

func TestPluginControlEventNames(t *testing.T) {
	for ft, want := range map[FrameType]string{
		FramePlugins:     "plugin:list",
		FramePluginEvent: "plugin:event",
	} {
		if got, ok := ControlEventName(ft); !ok || got != want {
			t.Errorf("ControlEventName(%s) = %q, %v; want %q", ft, got, ok, want)
		}
	}
}

// Every FrameType constant must be classified, or a new frame could
// silently skip the plugin parity table in docs/plugins.md. The
// constants are enumerated from the source rather than via String(),
// which would miss a frame that also forgot its String case.
func TestEveryFrameClassified(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "frame.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)
			if id, ok := vs.Type.(*ast.Ident); !ok || id.Name != "FrameType" {
				continue
			}
			for _, n := range vs.Names {
				names = append(names, n.Name)
			}
		}
	}
	if len(names) < 50 {
		t.Fatalf("found only %d FrameType constants; parser walk is broken", len(names))
	}

	count := map[FrameType]int{}
	for _, list := range [][]FrameType{ControlRequestFrames, ControlEventFrames, NonControlFrames} {
		for _, ft := range list {
			count[ft]++
		}
	}
	byValue := map[string]FrameType{}
	for ft := FrameType(0); ; ft++ {
		byValue[ft.String()] = ft
		if ft == 0xff {
			break
		}
	}
	for _, n := range names {
		// FrameListPlugins → LIST_PLUGINS is not derivable from the Go
		// name, so resolve the constant through the classified lists by
		// matching every frame's String() against a normalised name.
		ft, ok := resolveFrame(n, byValue)
		if !ok {
			t.Errorf("%s has no String() case", n)
			continue
		}
		if c := count[ft]; c != 1 {
			t.Errorf("%s (0x%02x) appears in %d classification lists, want exactly 1", n, byte(ft), c)
		}
	}
}

// resolveFrame maps a Go constant name (FrameSetPluginEnabled) to its
// FrameType by comparing against each String() with underscores and
// case removed (SETPLUGINENABLED).
func resolveFrame(goName string, byValue map[string]FrameType) (FrameType, bool) {
	want := strings.ToUpper(strings.TrimPrefix(goName, "Frame"))
	for s, ft := range byValue {
		if strings.ReplaceAll(s, "_", "") == want {
			return ft, true
		}
	}
	return 0, false
}

func TestPluginPayloadsSnakeCase(t *testing.T) {
	b, err := json.Marshal(PluginEvent{Kind: PluginEventAdded, Nonce: "n1", Plugin: PluginInfo{
		ID: "webhook", APIVersion: "0.1", StatusDetail: "x", Command: []string{"node"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"api_version"`, `"status_detail"`, `"nonce":"n1"`, `"command":["node"]`} {
		if !strings.Contains(string(b), key) {
			t.Errorf("PluginEvent JSON %s missing %s", b, key)
		}
	}
	e, _ := json.Marshal(Error{Code: ErrCodePluginInstallFailed, Nonce: "n2"})
	if !strings.Contains(string(e), `"nonce":"n2"`) {
		t.Errorf("Error JSON %s missing nonce", e)
	}
}
