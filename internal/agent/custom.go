package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
)

// CustomFileName is the file, under the directory passed to
// SetCustomDir, that holds user-defined agents.
const CustomFileName = "agents.json"

// Custom is one user-defined agent as stored in agents.json.
//
// It is deliberately a subset of Def: ResumeArgs and CaptureSessionIDFn
// are Go funcs that JSON cannot express, so custom agents get no
// resume/session-id capture. Restart re-runs Cmd instead of resuming
// the prior conversation.
//
// ponytail: no resume for custom agents. If this becomes a real
// complaint, the upgrade path is a declarative resume template in the
// JSON (e.g. {"resumeCmd": ["mytool", "--resume", "{{id}}"]}) expanded
// into a ResumeArgs closure at load time.
type Custom struct {
	// ID is assigned once, at first save, by slugging Name. It is
	// never recomputed on rename: registry entries persist only the
	// agent ID (internal/registry/persist.go), and Revive re-resolves
	// the command through Get(ID), so a changing ID would silently
	// break revive for every session already created with this agent.
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Cmd   []string `json:"cmd"`
	Color string   `json:"color"`
}

const defaultCustomColor = "#64748b"

// Custom-agent state. The mutex matters: defsByID is read-only, but
// the cache below turns Get/All into read-modify-write, and hived
// calls them concurrently from Registry.Create and from the
// CaptureSessionIDFn goroutines.
var (
	customMu    sync.Mutex
	customDir   string
	customCache []Def
	cacheStamp  string // "<modtime>-<size>"; "" means "not loaded"
)

// SetCustomDir points the loader at the directory holding
// CustomFileName, and is called once at startup by both hived and
// hivegui. The directory is injected rather than imported because
// internal/registry imports this package, so calling
// registry.StateDir() here would be an import cycle.
func SetCustomDir(dir string) {
	customMu.Lock()
	defer customMu.Unlock()
	customDir = dir
	customCache, cacheStamp = nil, ""
}

// customDefs returns the currently configured custom agents, reloading
// from disk when the file has changed since the last read. This is why
// no reload IPC is needed: hivegui writes the file and hived picks it
// up on its next Get.
//
// A missing, unreadable, or malformed file yields no custom agents —
// never an error and never a panic. Bricking the launcher because a
// hand-edited config has a stray comma would be a far worse failure
// than silently dropping the customs.
func customDefs() []Def {
	customMu.Lock()
	defer customMu.Unlock()
	if customDir == "" {
		return nil
	}
	path := filepath.Join(customDir, CustomFileName)
	st, err := os.Stat(path)
	if err != nil {
		customCache, cacheStamp = nil, ""
		return nil
	}
	stamp := fmt.Sprintf("%d-%d", st.ModTime().UnixNano(), st.Size())
	if stamp == cacheStamp {
		return customCache
	}
	// Cache the stamp even on failure so a broken file is logged once
	// per edit rather than on every session launch.
	cacheStamp, customCache = stamp, nil

	raw, err := os.ReadFile(path)
	if err != nil {
		log.Printf("agent: read %s: %v (custom agents disabled)", path, err)
		return nil
	}
	var list []Custom
	if err := json.Unmarshal(raw, &list); err != nil {
		log.Printf("agent: parse %s: %v (custom agents disabled)", path, err)
		return nil
	}
	defs, rejected := validateCustom(list)
	for _, e := range rejected {
		log.Printf("agent: %s: %v", CustomFileName, e)
	}
	customCache = defs
	return customCache
}

// validateCustom splits list into usable Defs and one error per
// rejected entry. Shared by the loader (which logs and skips) and by
// SaveCustom (which refuses to write) so the GUI and the on-disk file
// can never disagree about what counts as valid.
func validateCustom(list []Custom) ([]Def, []error) {
	var (
		defs     []Def
		rejected []error
		seen     = make(map[ID]bool, len(list))
	)
	for i, c := range list {
		id := ID(strings.TrimSpace(c.ID))
		name := strings.TrimSpace(c.Name)
		label := name
		if label == "" {
			label = fmt.Sprintf("entry %d", i+1)
		}

		if id == "" {
			// An empty id after assignIDs means the name was blank or
			// slugged to nothing ("!!!"). Say that, rather than
			// blaming an id the user never sees or types.
			if name == "" {
				rejected = append(rejected, fmt.Errorf("%s: name is required", label))
			} else {
				rejected = append(rejected, fmt.Errorf(
					"%s: name must contain at least one letter or number", label))
			}
			continue
		}
		if _, isBuiltin := defsByID[id]; isBuiltin {
			rejected = append(rejected, fmt.Errorf(
				"%s: %q is a built-in agent and cannot be redefined", label, id))
			continue
		}
		if seen[id] {
			rejected = append(rejected, fmt.Errorf("%s: duplicate id %q", label, id))
			continue
		}
		cmd := trimCmd(c.Cmd)
		if len(cmd) == 0 {
			rejected = append(rejected, fmt.Errorf("%s: command is empty", label))
			continue
		}

		seen[id] = true
		if name == "" {
			name = string(id)
		}
		color := safeColor(c.Color)
		defs = append(defs, Def{ID: id, Name: name, Cmd: cmd, Color: color})
	}
	return defs, rejected
}

// safeColor returns c if it is a plain hex color, and the default
// otherwise.
//
// The value is substituted into a CSS custom property that the launcher
// sets on a row (`--agent-color`), so an arbitrary string is a CSS
// injection: `url("http://…")` in the `background` shorthand made the
// webview issue an outbound request just from opening the launcher —
// verified, not theoretical. Built-in colors were compile-time
// constants; custom agents are the first user-controlled source, and
// agents.json is meant to be hand-edited and shared.
//
// Substituting rather than rejecting is deliberate: a bad color should
// not drop an otherwise valid agent on the load path, and the reset to
// grey is visible feedback on the save path. The CSS sink also uses
// background-color rather than the shorthand as a second layer.
func safeColor(c string) string {
	c = strings.TrimSpace(c)
	// #rgb, #rgba, #rrggbb, #rrggbbaa — what the color input emits and
	// what every built-in uses.
	if n := len(c) - 1; strings.HasPrefix(c, "#") && (n == 3 || n == 4 || n == 6 || n == 8) {
		if strings.IndexFunc(c[1:], func(r rune) bool {
			return !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F'))
		}) < 0 {
			return c
		}
	}
	return defaultCustomColor
}

// trimCmd drops blank argv elements, which is what a trailing space in
// the settings form's whitespace split produces.
func trimCmd(cmd []string) []string {
	out := make([]string, 0, len(cmd))
	for _, a := range cmd {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	return out
}

// LoadCustom returns the raw custom-agent entries as stored on disk,
// for the settings UI to edit and write back. Unlike customDefs it
// does not drop invalid entries — the user needs to see a broken row
// in order to fix it.
//
// Unlike customDefs it also does not swallow a parse failure. The
// launcher can safely degrade to built-ins on a broken file, but the
// settings modal cannot: an empty list there is indistinguishable
// from "no agents defined", and saving over it would overwrite the
// file the user was trying to fix. A missing file is (nil, nil); an
// unreadable or malformed one is an error the caller must surface.
func LoadCustom() ([]Custom, error) {
	customMu.Lock()
	dir := customDir
	customMu.Unlock()
	if dir == "" {
		return nil, nil
	}
	path := filepath.Join(dir, CustomFileName)
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", CustomFileName, err)
	}
	var list []Custom
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("parse %s: %w", CustomFileName, err)
	}
	return list, nil
}

// SaveCustom validates and atomically writes the full custom-agent
// list, assigning an ID to any entry that lacks one.
//
// Validation runs here, not only in the loader, because a loader
// warning goes to hived.log where nobody using the GUI will ever see
// it. A rejected agent must fail the save so the error surfaces in the
// settings modal, where the user is looking.
func SaveCustom(list []Custom) error {
	customMu.Lock()
	dir := customDir
	customMu.Unlock()
	if dir == "" {
		return errors.New("no config directory configured")
	}

	assignIDs(list)
	if _, rejected := validateCustom(list); len(rejected) > 0 {
		return errors.Join(rejected...)
	}

	blob, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	blob = append(blob, '\n')
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// Temp file in the same directory so the rename stays atomic.
	tmp, err := os.CreateTemp(dir, CustomFileName+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op once the rename succeeds
	if _, err := tmp.Write(blob); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, CustomFileName)); err != nil {
		return err
	}

	customMu.Lock()
	customCache, cacheStamp = nil, ""
	customMu.Unlock()
	return nil
}

// assignIDs fills in an ID for new entries by slugging Name, keeping
// existing IDs untouched so a rename never changes an agent's
// identity. Collisions with built-ins or with earlier entries get a
// numeric suffix; anything still invalid is caught by validateCustom.
func assignIDs(list []Custom) {
	taken := make(map[ID]bool, len(list))
	for _, c := range list {
		if id := ID(strings.TrimSpace(c.ID)); id != "" {
			taken[id] = true
		}
	}
	for i := range list {
		if strings.TrimSpace(list[i].ID) != "" {
			continue
		}
		base := slugify(list[i].Name)
		if base == "" {
			continue // validateCustom rejects it with a clear message
		}
		id := ID(base)
		for n := 2; taken[id] || defsByID[id].ID != ""; n++ {
			id = ID(fmt.Sprintf("%s-%d", base, n))
		}
		taken[id] = true
		list[i].ID = string(id)
	}
}

// slugify reduces a display name to a lowercase identifier.
//
// Letters and digits are kept per Unicode, not just ASCII: an id is
// only ever a map key and a JSON string, so there is no reason to
// mangle "Ünïcödé Tool" into "n-c-d-tool" or reduce a name written in
// a non-Latin script to nothing.
func slugify(name string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			prevDash = false
		case b.Len() > 0 && !prevDash:
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
