package config

import (
	"encoding/json"
	"strings"
)

// KeyBinding is one configurable action's set of key strings. It marshals as a
// JSON array (the canonical form) but accepts either a string or an array on
// unmarshal so configs written before keybindings supported multiple keys per
// action keep loading. Empty entries and surrounding whitespace are stripped.
type KeyBinding []string

// First returns the first non-empty key, or "" if none is set. Useful for
// rendering a single-key label (e.g. in help hints) without panicking on an
// empty binding.
func (kb KeyBinding) First() string {
	for _, k := range kb {
		if k != "" {
			return k
		}
	}
	return ""
}

// HelpKey returns a display string for the binding, joining all configured
// keys with "/" (e.g. "up/k"). Returns "" if no keys are set.
func (kb KeyBinding) HelpKey() string {
	parts := make([]string, 0, len(kb))
	for _, k := range kb {
		if k != "" {
			parts = append(parts, k)
		}
	}
	return strings.Join(parts, "/")
}

// MarshalJSON forces the array form even when the slice is nil, so the
// on-disk shape stays consistent (`[]` instead of `null`) regardless of how
// the field was constructed in memory.
func (kb KeyBinding) MarshalJSON() ([]byte, error) {
	if kb == nil {
		return []byte("[]"), nil
	}
	return json.Marshal([]string(kb))
}

// UnmarshalJSON accepts either a JSON string ("a") or array (["a","f"]) and
// normalizes to []string. This preserves backwards compatibility with configs
// written when each KeybindingsConfig field was a single string.
func (kb *KeyBinding) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*kb = nil
		return nil
	}
	if trimmed[0] == '[' {
		var arr []string
		if err := json.Unmarshal(data, &arr); err != nil {
			return err
		}
		out := make([]string, 0, len(arr))
		for _, s := range arr {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		*kb = out
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	s = strings.TrimSpace(s)
	if s == "" {
		*kb = nil
		return nil
	}
	*kb = []string{s}
	return nil
}

// Config is the user-facing configuration loaded from ~/.config/hive/config.json.
type Config struct {
	SchemaVersion               int                       `json:"schema_version"`
	Theme                       string                    `json:"theme"`
	PreviewRefreshMs            int                       `json:"preview_refresh_ms"`
	AgentTitleOverridesUserTitle bool                     `json:"agent_title_overrides_user_title"`
	HideAttachHint              bool                      `json:"hide_attach_hint,omitempty"`
	HideWhatsNew                bool                      `json:"hide_whats_new,omitempty"`
	LastSeenVersion             string                    `json:"last_seen_version,omitempty"`
	// StartupView selects the initial view shown on startup.
	// "sidebar" (default) shows the normal session list.
	// "grid" opens the current-project grid.
	// "grid-all" opens the all-projects grid.
	StartupView                 string                    `json:"startup_view,omitempty"`
	// BellSound selects the audio played when a background session rings
	// its terminal bell. See internal/audio for the accepted identifiers
	// (normal, bee, chime, ping, knock, silent).
	BellSound                   string                    `json:"bell_sound,omitempty"`
	// BellVolume controls playback loudness as a percentage (1–100).
	// A value of 0 is treated as 100 (full volume) for backwards compatibility
	// with configs written before this field existed.
	BellVolume                  int                       `json:"bell_volume,omitempty"`
	// Multiplexer selects the backend for managing terminal sessions.
	// "tmux" (default) uses the external tmux binary.
	// "native" uses Go's built-in PTY daemon; no external binary needed.
	Multiplexer                 string                    `json:"multiplexer,omitempty"`
	// HideGridInputHint suppresses the first-use hint shown when entering grid
	// input mode. Set to true (via the "d" key in the hint dialog) to skip the
	// hint on subsequent activations. Tracked independently from HideAttachHint.
	HideGridInputHint bool `json:"hide_grid_input_hint,omitempty"`
	// DisableGridInput disables the in-place input mode in grid view.
	// When false (default), pressing 'i' on a focused grid cell enters input
	// mode and forwards keystrokes to that session without a full attach.
	// Set to true to opt out of this feature (e.g. if 'i' is needed for
	// another keybinding or the forwarding latency is unacceptable).
	DisableGridInput bool `json:"disable_grid_input,omitempty"`
	// DetachKey is the single-key combination that returns the user from an
	// attached session back to the Hive TUI. Accepted form is
	// "ctrl+<lowercase-letter>" (e.g. "ctrl+q", "ctrl+d"). Defaults to
	// "ctrl+q". The key is enforced by the active multiplexer backend
	// (tmux installs a server-side bind-key; the native backend intercepts
	// the matching control byte on stdin), so it is not part of the
	// in-TUI Keybindings struct.
	DetachKey                   string                    `json:"detach_key,omitempty"`
	Agents                      map[string]AgentProfile   `json:"agents"`
	TeamDefaults                TeamDefaultsConfig        `json:"team_defaults"`
	Hooks                       HooksConfig               `json:"hooks"`
	Keybindings                 KeybindingsConfig         `json:"keybindings"`
}

// AgentProfile defines how to launch a specific agent type.
type AgentProfile struct {
	Cmd        []string        `json:"cmd"`
	InstallCmd []string        `json:"install_cmd,omitempty"`
	Status     StatusDetection `json:"status,omitempty"`
}

// StatusDetection configures how hive detects whether a session is running,
// waiting for input, or idle. Regex patterns are matched against pane titles
// or last-line content.
type StatusDetection struct {
	WaitTitle   string `json:"wait_title,omitempty"`   // regex on pane title → waiting
	RunTitle    string `json:"run_title,omitempty"`     // regex on pane title → running
	WaitPrompt  string `json:"wait_prompt,omitempty"`  // regex on last non-empty line → waiting
	IdlePrompt  string `json:"idle_prompt,omitempty"`  // regex on last non-empty line → idle (if configured but not matched → waiting)
	StableTicks int    `json:"stable_ticks,omitempty"` // polls before running→idle/waiting (default 2)
}

// TeamDefaultsConfig specifies defaults when creating a new team.
type TeamDefaultsConfig struct {
	Orchestrator string `json:"orchestrator"`
	WorkerCount  int    `json:"worker_count"`
	WorkerAgent  string `json:"worker_agent"`
}

// HooksConfig controls the hook system.
type HooksConfig struct {
	Enabled bool   `json:"enabled"`
	Dir     string `json:"dir"`
}

// KeybindingsConfig maps action names to one or more key strings. Every field
// is a KeyBinding so users can bind multiple keys to the same action (e.g.
// `["up", "k"]`). Existing configs that stored a single string are loaded
// transparently via KeyBinding.UnmarshalJSON.
type KeybindingsConfig struct {
	NewWorktreeSession KeyBinding `json:"new_worktree_session"`
	NewProject     KeyBinding `json:"new_project"`
	NewSession     KeyBinding `json:"new_session"`
	NewTeam        KeyBinding `json:"new_team"`
	KillSession    KeyBinding `json:"kill_session"`
	KillTeam       KeyBinding `json:"kill_team"`
	Rename         KeyBinding `json:"rename"`
	Attach         KeyBinding `json:"attach"`
	ToggleCollapse KeyBinding `json:"toggle_collapse"`
	CollapseItem   KeyBinding `json:"collapse_item"`
	ExpandItem     KeyBinding `json:"expand_item"`
	NavUp          KeyBinding `json:"nav_up"`
	NavDown        KeyBinding `json:"nav_down"`
	NavProjectUp   KeyBinding `json:"nav_project_up"`
	NavProjectDown KeyBinding `json:"nav_project_down"`
	CursorUp       KeyBinding `json:"cursor_up"`
	CursorDown     KeyBinding `json:"cursor_down"`
	CursorLeft     KeyBinding `json:"cursor_left"`
	CursorRight    KeyBinding `json:"cursor_right"`
	JumpProject1   KeyBinding `json:"jump_project_1"`
	Filter         KeyBinding `json:"filter"`
	SidebarView    KeyBinding `json:"sidebar_view"`
	GridOverview   KeyBinding `json:"grid_overview"`
	Palette        KeyBinding `json:"palette"`
	Help           KeyBinding `json:"help"`
	TmuxHelp       KeyBinding `json:"tmux_help"`
	Settings       KeyBinding `json:"settings"`
	Quit           KeyBinding `json:"quit"`
	QuitKill       KeyBinding `json:"quit_kill"`
	ColorNext      KeyBinding `json:"color_next"`
	ColorPrev      KeyBinding `json:"color_prev"`
	SessionColorNext KeyBinding `json:"session_color_next"`
	SessionColorPrev KeyBinding `json:"session_color_prev"`
	MoveUp         KeyBinding `json:"move_up"`
	MoveDown       KeyBinding `json:"move_down"`
	MoveLeft       KeyBinding `json:"move_left"`
	MoveRight      KeyBinding `json:"move_right"`
	InputMode      KeyBinding `json:"input_mode"`
	Detach         KeyBinding `json:"detach"`
	ToggleAll      KeyBinding `json:"toggle_all"`
}
