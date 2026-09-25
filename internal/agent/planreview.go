package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/lucascaro/hive/internal/wire"
)

// Claude plan review (#457): who else reviews ExitPlanMode, and the
// words a denied plan goes back to the agent with.

// ReviewerKind says where an external plan reviewer is configured,
// which decides whether Hive can switch it off for its own sessions.
const (
	// ReviewerPlugin is a Claude Code plugin's hook. Hive can disable
	// the plugin for the sessions it starts (--settings enabledPlugins).
	ReviewerPlugin = "plugin"
	// ReviewerSettings is a hook written into a settings.json. Claude
	// concatenates hooks across settings sources, so nothing Hive passes
	// can remove it; the Settings screen warns instead.
	ReviewerSettings = "settings"
)

// ExternalReviewer is one ExitPlanMode reviewer other than Hive.
type ExternalReviewer struct {
	Kind string `json:"kind"`
	// ID is the plugin id ("name@marketplace") for a plugin, or the
	// settings file path for a settings hook.
	ID string `json:"id"`
	// Active is false for an installed plugin that no settings source
	// enables. Settings hooks are always active.
	Active bool `json:"active"`
}

// ReviewerPaths are the files ExternalPlanReviewers reads. A zero
// field is skipped.
type ReviewerPaths struct {
	Home       string // the user's home: ~/.claude/settings.json and plugins
	ProjectDir string // the session's cwd: .claude/settings{,.local}.json
	Managed    string // the managed-settings.json path; see ManagedSettingsPath
}

// ManagedSettingsPath is where Claude Code reads enterprise-managed
// settings on this platform, or "" where Hive does not know it.
func ManagedSettingsPath() string {
	switch runtime.GOOS {
	case "darwin":
		return "/Library/Application Support/ClaudeCode/managed-settings.json"
	case "linux":
		return "/etc/claude-code/managed-settings.json"
	case "windows":
		return `C:\ProgramData\ClaudeCode\managed-settings.json`
	}
	return ""
}

type claudeHookGroupFile struct {
	Matcher string `json:"matcher"`
}

type claudeSettingsFile struct {
	Hooks           map[string][]claudeHookGroupFile `json:"hooks"`
	EnabledPlugins  map[string]bool                  `json:"enabledPlugins"`
	DisableAllHooks bool                             `json:"disableAllHooks"`
}

// readClaudeJSON decodes path into v, reporting whether it could. A
// missing or malformed file is simply "not configured": detection must
// never fail a review, only decide who gives it.
func readClaudeJSON(path string, v any) bool {
	if path == "" {
		return false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return json.Unmarshal(trimBOM(raw), v) == nil
}

// reviewsExitPlanMode reports whether any PermissionRequest group
// targets ExitPlanMode by name.
//
// ponytail: matcher-less and "*" PermissionRequest hooks are treated as
// observers, not reviewers — Hive's own hook is one, and so is any
// logger. Revisit if a real reviewer ships as a catch-all.
func reviewsExitPlanMode(hooks map[string][]claudeHookGroupFile) bool {
	for _, g := range hooks["PermissionRequest"] {
		m := strings.TrimSpace(g.Matcher)
		if m == "" || m == "*" {
			continue
		}
		re, err := regexp.Compile("^(?:" + m + ")$")
		if err != nil {
			continue
		}
		if re.MatchString("ExitPlanMode") {
			return true
		}
	}
	return false
}

// pluginHooks returns the hooks a plugin at installPath declares:
// hooks/hooks.json, or a "hooks" object or path in its manifest.
func pluginHooks(installPath string) map[string][]claudeHookGroupFile {
	var f struct {
		Hooks map[string][]claudeHookGroupFile `json:"hooks"`
	}
	if readClaudeJSON(filepath.Join(installPath, "hooks", "hooks.json"), &f) && len(f.Hooks) > 0 {
		return f.Hooks
	}
	var manifest struct {
		Hooks json.RawMessage `json:"hooks"`
	}
	if !readClaudeJSON(filepath.Join(installPath, ".claude-plugin", "plugin.json"), &manifest) || len(manifest.Hooks) == 0 {
		return nil
	}
	var inline struct {
		Hooks map[string][]claudeHookGroupFile `json:"hooks"`
	}
	if json.Unmarshal(manifest.Hooks, &inline) == nil && len(inline.Hooks) > 0 {
		return inline.Hooks
	}
	var rel string
	if json.Unmarshal(manifest.Hooks, &rel) == nil && rel != "" {
		if readClaudeJSON(filepath.Join(installPath, filepath.FromSlash(rel)), &f) {
			return f.Hooks
		}
	}
	return nil
}

// ExternalPlanReviewers lists every ExitPlanMode reviewer other than
// Hive's: hooks in the user, project, local and managed settings, and
// installed plugins whose hooks review ExitPlanMode. Sorted, so callers
// and tests get a stable order.
func ExternalPlanReviewers(p ReviewerPaths) []ExternalReviewer {
	var sources []string
	if p.Home != "" {
		sources = append(sources, filepath.Join(p.Home, ".claude", "settings.json"))
	}
	if p.ProjectDir != "" {
		sources = append(sources,
			filepath.Join(p.ProjectDir, ".claude", "settings.json"),
			filepath.Join(p.ProjectDir, ".claude", "settings.local.json"))
	}
	sources = append(sources, p.Managed)

	var out []ExternalReviewer
	enabled := map[string]bool{}
	for _, path := range sources {
		var s claudeSettingsFile
		if !readClaudeJSON(path, &s) {
			continue
		}
		if s.DisableAllHooks {
			// No hook runs at all, reviewers included.
			return nil
		}
		// Later sources override earlier ones, which is the order
		// Claude applies them in.
		for id, on := range s.EnabledPlugins {
			enabled[id] = on
		}
		if reviewsExitPlanMode(s.Hooks) {
			out = append(out, ExternalReviewer{Kind: ReviewerSettings, ID: path, Active: true})
		}
	}

	if p.Home != "" {
		var installed struct {
			Plugins map[string][]struct {
				InstallPath string `json:"installPath"`
			} `json:"plugins"`
		}
		readClaudeJSON(filepath.Join(p.Home, ".claude", "plugins", "installed_plugins.json"), &installed)
		for id, installs := range installed.Plugins {
			for _, in := range installs {
				if reviewsExitPlanMode(pluginHooks(in.InstallPath)) {
					out = append(out, ExternalReviewer{Kind: ReviewerPlugin, ID: id, Active: enabled[id]})
					break
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// HasActiveExternalReviewer reports whether any reviewer in rs would
// prompt for ExitPlanMode.
func HasActiveExternalReviewer(rs []ExternalReviewer) bool {
	for _, r := range rs {
		if r.Active {
			return true
		}
	}
	return false
}

// FormatPlanFeedback is the message a denied plan goes back to the
// agent with. It is the one place that wording lives: the Claude hook
// and the Pi tool both forward it verbatim.
//
// The framing is load-bearing. Verified live on Claude Code 2.1.282: a
// bare "Reviewer comments: … instead" was read by the model as a prompt
// injection and ignored, while the same content attributed to the user
// was applied. So it says who asked, and what to do next.
func FormatPlanFeedback(source string, comments []wire.PlanComment, feedback string) string {
	resubmit := "call ExitPlanMode again"
	if source == wire.PlanReviewSourcePi {
		resubmit = "call hive_submit_plan again"
	}
	var b strings.Builder
	b.WriteString("The user reviewed your plan in Hive and did not approve it yet. ")
	b.WriteString("Revise the plan to address their comments, then " + resubmit + ".\n")
	n := 0
	for _, c := range comments {
		text := strings.TrimSpace(c.Text)
		quote := strings.TrimSpace(c.Quote)
		if text == "" && quote == "" {
			continue
		}
		if n == 0 {
			b.WriteString("\nUser comments on specific passages:\n")
		}
		n++
		b.WriteString("\n")
		b.WriteString(strconv.Itoa(n))
		b.WriteString(". On \"")
		b.WriteString(strings.Join(strings.Fields(quote), " "))
		b.WriteString("\":\n")
		for _, line := range strings.Split(text, "\n") {
			b.WriteString("   > ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	if fb := strings.TrimSpace(feedback); fb != "" {
		b.WriteString("\nOverall feedback from the user:\n")
		b.WriteString(fb)
		b.WriteString("\n")
	}
	return b.String()
}
