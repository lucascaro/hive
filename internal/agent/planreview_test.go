package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/wire"
)

func writeJSONFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const exitPlanReviewerHooks = `{"hooks":{"PermissionRequest":[{"matcher":"ExitPlanMode","hooks":[{"type":"command","command":"plannotator"}]}]}}`

// installPlugin writes an installed plugin with hooks.json body into a
// fake home's plugin registry.
func installPlugin(t *testing.T, home, id, hooks string) {
	t.Helper()
	dir := filepath.Join(home, ".claude", "plugins", "cache", strings.ReplaceAll(id, "@", "-"))
	writeJSONFile(t, filepath.Join(dir, "hooks", "hooks.json"), hooks)
	reg := filepath.Join(home, ".claude", "plugins", "installed_plugins.json")
	var f struct {
		Version int                         `json:"version"`
		Plugins map[string][]map[string]any `json:"plugins"`
	}
	f.Version = 2
	if raw, err := os.ReadFile(reg); err == nil {
		_ = json.Unmarshal(raw, &f)
	}
	if f.Plugins == nil {
		f.Plugins = map[string][]map[string]any{}
	}
	f.Plugins[id] = []map[string]any{{"scope": "user", "installPath": dir}}
	blob, _ := json.Marshal(f)
	writeJSONFile(t, reg, string(blob))
}

func TestExternalPlanReviewer(t *testing.T) {
	type setup func(t *testing.T, home, proj, managed string)
	for _, tc := range []struct {
		name  string
		setup setup
		want  []ExternalReviewer // Kind + Active only; ID checked where stable
	}{
		{"none", func(*testing.T, string, string, string) {}, nil},
		{"user settings exact matcher", func(t *testing.T, home, _, _ string) {
			writeJSONFile(t, filepath.Join(home, ".claude", "settings.json"), exitPlanReviewerHooks)
		}, []ExternalReviewer{{Kind: ReviewerSettings, Active: true}}},
		{"regex matcher", func(t *testing.T, home, _, _ string) {
			writeJSONFile(t, filepath.Join(home, ".claude", "settings.json"),
				`{"hooks":{"PermissionRequest":[{"matcher":"Exit.*","hooks":[]}]}}`)
		}, []ExternalReviewer{{Kind: ReviewerSettings, Active: true}}},
		{"alternation matcher", func(t *testing.T, home, _, _ string) {
			writeJSONFile(t, filepath.Join(home, ".claude", "settings.json"),
				`{"hooks":{"PermissionRequest":[{"matcher":"Bash|ExitPlanMode","hooks":[]}]}}`)
		}, []ExternalReviewer{{Kind: ReviewerSettings, Active: true}}},
		{"project local", func(t *testing.T, _, proj, _ string) {
			writeJSONFile(t, filepath.Join(proj, ".claude", "settings.local.json"), exitPlanReviewerHooks)
		}, []ExternalReviewer{{Kind: ReviewerSettings, Active: true}}},
		{"managed", func(t *testing.T, _, _, managed string) {
			writeJSONFile(t, managed, exitPlanReviewerHooks)
		}, []ExternalReviewer{{Kind: ReviewerSettings, Active: true}}},
		{"enabled plugin", func(t *testing.T, home, _, _ string) {
			installPlugin(t, home, "plannotator@plannotator", exitPlanReviewerHooks)
			writeJSONFile(t, filepath.Join(home, ".claude", "settings.json"), `{"enabledPlugins":{"plannotator@plannotator":true}}`)
		}, []ExternalReviewer{{Kind: ReviewerPlugin, ID: "plannotator@plannotator", Active: true}}},
		{"plugin enabled only in project", func(t *testing.T, home, proj, _ string) {
			installPlugin(t, home, "plannotator@plannotator", exitPlanReviewerHooks)
			writeJSONFile(t, filepath.Join(proj, ".claude", "settings.json"), `{"enabledPlugins":{"plannotator@plannotator":true}}`)
		}, []ExternalReviewer{{Kind: ReviewerPlugin, ID: "plannotator@plannotator", Active: true}}},
		{"disabled plugin", func(t *testing.T, home, _, _ string) {
			installPlugin(t, home, "plannotator@plannotator", exitPlanReviewerHooks)
			writeJSONFile(t, filepath.Join(home, ".claude", "settings.json"), `{"enabledPlugins":{"plannotator@plannotator":false}}`)
		}, []ExternalReviewer{{Kind: ReviewerPlugin, ID: "plannotator@plannotator", Active: false}}},
		{"plugin without a reviewer hook", func(t *testing.T, home, _, _ string) {
			installPlugin(t, home, "other@x", `{"hooks":{"SessionStart":[{"matcher":"startup","hooks":[]}]}}`)
			writeJSONFile(t, filepath.Join(home, ".claude", "settings.json"), `{"enabledPlugins":{"other@x":true}}`)
		}, nil},
		{"disableAllHooks", func(t *testing.T, home, proj, _ string) {
			writeJSONFile(t, filepath.Join(home, ".claude", "settings.json"), exitPlanReviewerHooks)
			writeJSONFile(t, filepath.Join(proj, ".claude", "settings.json"), `{"disableAllHooks":true}`)
		}, nil},
		{"matcher-less and star are observers", func(t *testing.T, home, _, _ string) {
			writeJSONFile(t, filepath.Join(home, ".claude", "settings.json"),
				`{"hooks":{"PermissionRequest":[{"hooks":[]},{"matcher":"*","hooks":[]}]}}`)
		}, nil},
		{"other tool matcher", func(t *testing.T, home, _, _ string) {
			writeJSONFile(t, filepath.Join(home, ".claude", "settings.json"),
				`{"hooks":{"PermissionRequest":[{"matcher":"Bash","hooks":[]}]}}`)
		}, nil},
		{"malformed json", func(t *testing.T, home, _, _ string) {
			writeJSONFile(t, filepath.Join(home, ".claude", "settings.json"), `{ nope`)
		}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, proj := t.TempDir(), t.TempDir()
			managed := filepath.Join(t.TempDir(), "managed-settings.json")
			tc.setup(t, home, proj, managed)
			got := ExternalPlanReviewers(ReviewerPaths{Home: home, ProjectDir: proj, Managed: managed})
			if len(got) != len(tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
			for i := range got {
				if got[i].Kind != tc.want[i].Kind || got[i].Active != tc.want[i].Active {
					t.Errorf("[%d] = %+v, want %+v", i, got[i], tc.want[i])
				}
				if tc.want[i].ID != "" && got[i].ID != tc.want[i].ID {
					t.Errorf("[%d].ID = %q, want %q", i, got[i].ID, tc.want[i].ID)
				}
			}
			if HasActiveExternalReviewer(got) != (len(tc.want) > 0 && tc.want[0].Active) {
				t.Errorf("HasActiveExternalReviewer = %v", HasActiveExternalReviewer(got))
			}
		})
	}
}

// The deny message is how the user's words reach the agent; every
// quote and every comment must survive, attributed to the user (an
// unattributed version was ignored by Claude as an injection).
func TestFormatPlanFeedbackCarriesEveryQuote(t *testing.T) {
	comments := []wire.PlanComment{
		{Quote: "Create bye.txt at repo root", Text: "Call it farewell.txt."},
		{Quote: "Run   the\ntests", Text: "Use scripts/test.sh\nnot go test"},
		{Quote: "", Text: ""}, // empty: skipped
	}
	msg := FormatPlanFeedback(wire.PlanReviewSourceClaude, comments, "Keep it short.")
	for _, want := range []string{
		"The user reviewed your plan in Hive",
		"call ExitPlanMode again",
		`1. On "Create bye.txt at repo root":`,
		"> Call it farewell.txt.",
		`2. On "Run the tests":`,
		"> Use scripts/test.sh",
		"> not go test",
		"Overall feedback from the user:\nKeep it short.",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message lacks %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "3. On") {
		t.Errorf("empty comment rendered:\n%s", msg)
	}
	if pi := FormatPlanFeedback(wire.PlanReviewSourcePi, nil, "x"); !strings.Contains(pi, "call hive_submit_plan again") {
		t.Errorf("pi wording: %s", pi)
	}
}

// Only PermissionRequest carries the long timeout; every other event
// keeps Claude's default so a wedged hook cannot stall it for days.
func TestClaudeSpawnArgsPlanReviewTimeout(t *testing.T) {
	withClaudeVersion(t, "2.1.273")
	settingsDir(t, "")
	args := claudeSpawnArgs(hooked)
	if len(args) != 2 {
		t.Fatalf("args = %v", args)
	}
	var s claudeSettings
	if err := json.Unmarshal([]byte(args[1]), &s); err != nil {
		t.Fatal(err)
	}
	for ev, groups := range s.Hooks {
		if len(groups) != 1 || len(groups[0].Hooks) != 1 {
			t.Fatalf("%s: want one group with one entry, got %+v", ev, groups)
		}
		want := 0
		if ev == "PermissionRequest" {
			want = PlanReviewHookTimeout
		}
		if got := groups[0].Hooks[0].Timeout; got != want {
			t.Errorf("%s timeout = %d, want %d", ev, got, want)
		}
	}
	if !strings.Contains(args[1], `"timeout":345600`) || strings.Count(args[1], `"timeout"`) != 1 {
		t.Errorf("settings JSON should carry exactly one timeout: %s", args[1])
	}
}

// With Hive chosen as reviewer, every installed ExitPlanMode plugin is
// switched off for the session; otherwise nothing is touched.
func TestClaudeSpawnArgsDisablesExternalPluginReviewer(t *testing.T) {
	withClaudeVersion(t, "2.1.273")
	home := t.TempDir()
	installPlugin(t, home, "plannotator@plannotator", exitPlanReviewerHooks)
	installPlugin(t, home, "other@x", `{"hooks":{"SessionStart":[{"matcher":"startup","hooks":[]}]}}`)
	prev := userHomeDir
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = prev })

	for _, tc := range []struct {
		file string
		want map[string]bool
	}{
		{``, nil},
		{`{"plan_review": true}`, nil},
		{`{"plan_review": false, "plan_reviewer": "hive"}`, nil},
		{`{"plan_review": true, "plan_reviewer": "hive"}`, map[string]bool{"plannotator@plannotator": false}},
	} {
		settingsDir(t, tc.file)
		var s claudeSettings
		if err := json.Unmarshal([]byte(claudeSpawnArgs(hooked)[1]), &s); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(s.EnabledPlugins, tc.want) {
			t.Errorf("settings %q: enabledPlugins = %v, want %v", tc.file, s.EnabledPlugins, tc.want)
		}
	}
}
