package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// SettingsFileName is the file, beside CustomFileName under the
// directory passed to SetCustomDir, that holds agent behaviour
// settings.
//
// Like agents.json it is written by hivegui and read by hived at spawn
// time, which is why no IPC is needed: the next session the daemon
// starts sees the new value. It is agent configuration, not registry
// state, so it lives here rather than behind the wire protocol.
const SettingsFileName = "agent-settings.json"

// ClaudeTaskToolsEnv is the Claude Code variable that makes its
// task-tracking tools (TaskCreate / TaskUpdate / TaskList / TaskGet)
// available on every model. Claude Code provides them by default only
// on older models; on current ones (Opus 5, Sonnet 5) a session gets
// no task tools at all unless it opts in, and without them Hive
// receives no plan to show.
// See code.claude.com/docs/en/tools-reference, "Task tool availability".
const ClaudeTaskToolsEnv = "CLAUDE_CODE_ENABLE_TODO_TOOLS"

// PiTodoToolEnv tells Hive's Pi extension (internal/agent/pi/hive.ts)
// whether to register its hive_todo tool: "0" means off. Unlike
// ClaudeTaskToolsEnv it is Hive's own variable, not the user's.
const PiTodoToolEnv = "HIVE_PI_TODO_TOOL"

// Settings is the user-facing shape of agent-settings.json.
type Settings struct {
	// ClaudeTaskTools opts Hive's Claude sessions into Claude Code's
	// task tools, which is where the sidebar's plan progress comes from.
	// On by default: without it the feature shows nothing on the
	// current default model. Off costs the plan indicator and saves the
	// context those tools spend in every session.
	ClaudeTaskTools bool `json:"claude_task_tools"`
	// PiTodoTool has Hive's Pi extension add a hive_todo tool to the Pi
	// sessions Hive starts. Pi ships no todo tool, so this is where a Pi
	// session's plan comes from. On by default for the same reason as
	// ClaudeTaskTools; off saves the tool's context and keeps Hive from
	// adding anything to the user's agent.
	PiTodoTool bool `json:"pi_todo_tool"`
	// PlanReview holds an agent's plan for the user to approve or deny
	// in Hive before it implements it: Claude's ExitPlanMode, and a
	// hive_submit_plan tool for Pi. Off by default. Read live by the
	// daemon on every review, so switching it off takes effect at once.
	PlanReview bool `json:"plan_review"`
	// PlanReviewer decides who reviews a Claude plan when another
	// reviewer (plannotator, …) is also installed: PlanReviewerExternal
	// (the default) leaves it to that tool, PlanReviewerHive disables
	// a plugin reviewer for the Claude sessions Hive starts. Fixed per
	// session at spawn, because that is the only time Hive can disable
	// a plugin.
	PlanReviewer string `json:"plan_reviewer"`
}

// PlanReviewer values.
const (
	PlanReviewerExternal = "external"
	PlanReviewerHive     = "hive"
)

// settingsFile is the on-disk shape. Fields are pointers so a key
// missing from a hand-edited or older file reads as "use the default"
// rather than as false — a file written before a setting existed must
// not silently switch that setting off.
type settingsFile struct {
	ClaudeTaskTools *bool   `json:"claude_task_tools,omitempty"`
	PiTodoTool      *bool   `json:"pi_todo_tool,omitempty"`
	PlanReview      *bool   `json:"plan_review,omitempty"`
	PlanReviewer    *string `json:"plan_reviewer,omitempty"`
}

// DefaultSettings is what a fresh install, or a missing key, means.
func DefaultSettings() Settings {
	return Settings{ClaudeTaskTools: true, PiTodoTool: true, PlanReviewer: PlanReviewerExternal}
}

func (f settingsFile) resolve() Settings {
	s := DefaultSettings()
	if f.ClaudeTaskTools != nil {
		s.ClaudeTaskTools = *f.ClaudeTaskTools
	}
	if f.PiTodoTool != nil {
		s.PiTodoTool = *f.PiTodoTool
	}
	if f.PlanReview != nil {
		s.PlanReview = *f.PlanReview
	}
	// Anything but an explicit "hive" is the safe default: deferring to
	// an installed reviewer never disables a tool the user set up.
	if f.PlanReviewer != nil && *f.PlanReviewer == PlanReviewerHive {
		s.PlanReviewer = PlanReviewerHive
	}
	return s
}

func settingsPath() (string, error) {
	customMu.Lock()
	dir := customDir
	customMu.Unlock()
	if dir == "" {
		return "", errors.New("no config directory configured")
	}
	return filepath.Join(dir, SettingsFileName), nil
}

// LoadSettings reads agent-settings.json for the Settings screen. A
// missing file is the defaults. A file that exists but will not parse
// is an ERROR, not the defaults, for the same reason LoadCustom refuses
// to swallow one: a save over silently-defaulted values would overwrite
// the file the user was trying to fix.
func LoadSettings() (Settings, error) {
	path, err := settingsPath()
	if err != nil {
		return DefaultSettings(), nil
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return DefaultSettings(), nil
	}
	if err != nil {
		return DefaultSettings(), fmt.Errorf("read %s: %w", SettingsFileName, err)
	}
	var f settingsFile
	if err := json.Unmarshal(trimBOM(raw), &f); err != nil {
		return DefaultSettings(), fmt.Errorf("parse %s: %w", SettingsFileName, err)
	}
	return f.resolve(), nil
}

// SaveSettings writes agent-settings.json atomically.
func SaveSettings(s Settings) error {
	path, err := settingsPath()
	if err != nil {
		return err
	}
	reviewer := PlanReviewerExternal
	if s.PlanReviewer == PlanReviewerHive {
		reviewer = PlanReviewerHive
	}
	blob, err := json.MarshalIndent(settingsFile{
		ClaudeTaskTools: &s.ClaudeTaskTools, PiTodoTool: &s.PiTodoTool,
		PlanReview: &s.PlanReview, PlanReviewer: &reviewer,
	}, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Dir(path), SettingsFileName, append(blob, '\n'))
}

var settingsWarnOnce sync.Once

// spawnSettings is LoadSettings for the spawn path, where the answer
// must never be an error: a malformed file degrades to the defaults
// and is logged once, rather than failing a session launch over a
// stray comma in a preferences file.
func spawnSettings() Settings {
	s, err := LoadSettings()
	if err != nil {
		settingsWarnOnce.Do(func() {
			log.Printf("agent: %v (using default agent settings)", err)
		})
	}
	return s
}

// claudeSpawnEnv is the Claude adapter's environment: the task-tool
// opt-in, when the setting allows it.
//
// Two conditions keep it from getting in the way:
//
//   - It shares claudeSpawnArgs' gate, claudeHooksAvailable — a
//     resolved hived path AND a Claude version whose hooks are
//     verified. The tools cost context in every session, and their only
//     value to Hive is the plan the HOOKS report. A session that gets no
//     hooks would pay that cost for nothing.
//   - It never overrides the user. session.Options.Env is appended to
//     the inherited environment and the later duplicate wins, so
//     appending this unconditionally would silently flip a user's own
//     CLAUDE_CODE_ENABLE_TODO_TOOLS=0. An explicit choice in the
//     daemon's environment is left alone, whatever its value.
func claudeSpawnEnv(sp SpawnInfo) []string {
	if !claudeHooksAvailable(sp) {
		return nil
	}
	st := spawnSettings()
	// Hive's own variable, so it is explicit both ways and always set:
	// an inherited value must never decide who reviews this session.
	env := []string{PlanReviewerEnv + "=" + claudeSpawnReviewer(st)}
	if _, set := lookupEnv(ClaudeTaskToolsEnv); set {
		return env
	}
	if !st.ClaudeTaskTools {
		return env
	}
	return append(env, ClaudeTaskToolsEnv+"=1")
}

// piSpawnEnv is the Pi adapter's environment: whether the extension
// registers its hive_todo tool.
//
// It shares piSpawnArgs' gate — no extension on disk means no -e, and a
// variable for an extension that is not loaded would be noise. And it is
// explicit both ways, where claudeSpawnEnv only ever adds "=1": this is
// Hive's own variable, so a value inherited by the daemon (hived started
// from inside a Hive-spawned Pi session, say) must never override the
// setting. The later duplicate wins, so appending it always is enough.
func piSpawnEnv(sp SpawnInfo) []string {
	if piSpawnArgs(sp) == nil {
		return nil
	}
	// The heartbeat is only safe against a daemon that understands the
	// ordering key; this daemon does, and it is the one spawning pi.
	heartbeat := PiHeartbeatEnv + "=1"
	st := spawnSettings()
	return []string{
		PiTodoToolEnv + "=" + boolEnv(st.PiTodoTool),
		PiPlanReviewEnv + "=" + boolEnv(st.PlanReview),
		heartbeat,
	}
}

func boolEnv(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// PiHeartbeatEnv enables the Pi extension's state heartbeat (spec 423).
// Set only by a daemon that orders extension events by instance and
// seq: an older daemon would re-apply every heartbeat through the
// timestamp guard and re-raise a wait the user had cleared.
const PiHeartbeatEnv = "HIVE_PI_HEARTBEAT"

// PiPlanReviewEnv tells Hive's Pi extension whether to register its
// hive_submit_plan tool: "1" when plan review was on at spawn. The
// daemon still checks the setting live on every review, so switching it
// off mid-session works; switching it on reaches new Pi sessions.
const PiPlanReviewEnv = "HIVE_PI_PLAN_REVIEW"

// PlanReviewerEnv carries a Claude session's spawn-time reviewer choice
// to its hook (the hook inherits Claude's environment). "hive" means
// Hive disabled any plugin reviewer for this session, so the daemon
// reviews even though a reviewer is installed; anything else means an
// installed reviewer keeps ExitPlanMode.
const PlanReviewerEnv = "HIVE_PLAN_REVIEWER"

// lookupEnv is os.LookupEnv, swappable so tests can model a user who
// set the variable without mutating the real process environment
// under parallel tests.
var lookupEnv = os.LookupEnv
