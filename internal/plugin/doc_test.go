package plugin

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/wire"
)

// docs/plugins.md promises plugins every control request and event a
// GUI has. The promise is only as good as the table, so the table is
// checked against the wire package's own enumeration: a frame added to
// the protocol fails here until the plugin docs list it.
func TestPluginDocListsEveryControlFrame(t *testing.T) {
	b, err := os.ReadFile("../../docs/plugins.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	reqStart := strings.Index(doc, "| Request | Payload | Answered by |")
	evStart := strings.Index(doc, "| Frame | When |")
	if reqStart < 0 || evStart < 0 || evStart < reqStart {
		t.Fatal("docs/plugins.md request/event tables not found")
	}
	rows := regexp.MustCompile("(?m)^\\| `([A-Z_]+)` \\|")
	listed := func(section string) map[string]bool {
		out := map[string]bool{}
		for _, m := range rows.FindAllStringSubmatch(section, -1) {
			out[m[1]] = true
		}
		return out
	}
	requests := listed(doc[reqStart:evStart])
	events := listed(doc[evStart:])

	check := func(kind string, want []wire.FrameType, got map[string]bool) {
		wantNames := map[string]bool{}
		for _, ft := range want {
			wantNames[ft.String()] = true
			if !got[ft.String()] {
				t.Errorf("docs/plugins.md %s table is missing %s", kind, ft)
			}
		}
		for name := range got {
			if !wantNames[name] {
				t.Errorf("docs/plugins.md %s table lists %s, which is not a control %s", kind, name, kind)
			}
		}
	}
	check("request", wire.ControlRequestFrames, requests)
	check("event", wire.ControlEventFrames, events)
}
