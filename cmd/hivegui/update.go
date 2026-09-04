package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lucascaro/hive/internal/buildinfo"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// updateRepo is the upstream "owner/repo" used for both the API
// endpoint and the prefix we accept in the release URL we hand to
// the OS browser. Forks that want their own update check should
// patch this one constant (or set updateReleasesAPI + updateURLPrefix
// directly via an init in a fork-specific file).
const updateRepo = "lucascaro/hive"

// updateReleasesAPI is the GitHub releases endpoint we poll. Var so
// tests can point it at a stub server.
var updateReleasesAPI = "https://api.github.com/repos/" + updateRepo + "/releases/latest"

// updateURLPrefix is the only URL prefix we'll accept from the
// release JSON's html_url before passing it to the OS browser.
// Defense in depth: GitHub's html_url is always under github.com,
// but if the response were ever spoofed (compromised mirror, MITM
// without TLS, or a future GitHub bug), this stops us from handing
// a file:// or javascript: URL to BrowserOpenURL.
var updateURLPrefix = "https://github.com/" + updateRepo + "/"

// updateCheckInterval is how often the background loop re-checks
// after the initial post-startup probe. Var, not const, so tests can
// shrink it.
var updateCheckInterval = 6 * time.Hour

// UpdateInfo is the payload of both CheckForUpdate's return value and
// the "update:available" Wails event.
//
// Available means strictly "a newer tagged release than the running
// build exists" — it does NOT also imply that we trust the URL. URL
// is set only when the release's html_url passes updateURLPrefix; the
// frontend renders without a Download button when it's empty so a
// suspicious response can't push a file:// or javascript: URL into
// BrowserOpenURL, but the user is still told an update exists rather
// than being silently rewritten to "up to date".
type UpdateInfo struct {
	Available bool   `json:"available"`
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	URL       string `json:"url"`
	// Skipped is true when the check could not produce an answer — an
	// untagged "dev" build on the release channel, or a latest-channel
	// checkout with no upstream. The frontend uses it to differentiate a
	// real "you're up to date" from "we can't tell".
	Skipped bool `json:"skipped"`
	// Channel is the channel this result came from, so the frontend can
	// label a version ("2.4.0") differently from a commit ("8e65349")
	// without having to re-read the settings.
	Channel string `json:"channel"`
	// Stage drives the action button: idle | available | staging |
	// ready | error. Staging and beyond are set by the apply path, not
	// by the check.
	Stage string `json:"stage"`
	// Message is human-readable detail for the current Stage — progress
	// text while staging, the reason when Skipped, the failure when
	// Stage is error.
	Message string `json:"message"`
	// RestartKind says what applying this update will cost, and is the
	// reason the staging path bothers to run the staged hived at all.
	// RestartGUI means the staged daemon's contract matches the running
	// one, so the GUI can be relaunched and every session survives;
	// RestartFull means hived itself has to be replaced, which ends
	// them. Empty until a bundle is staged, and RestartFull whenever we
	// could not find out — never guess in the direction that silently
	// reloads into an incompatible daemon.
	RestartKind string `json:"restartKind,omitempty"`
}

// Values of UpdateInfo.RestartKind.
const (
	RestartGUI  = "gui"
	RestartFull = "full"
)

// Stages of the update action button. The frontend maps these to
// labels (Update / Updating… / Restart) in lib/update-state.ts.
const (
	StageIdle      = "idle"
	StageAvailable = "available"
	StageStaging   = "staging"
	StageReady     = "ready"
	StageError     = "error"
)

// CheckForUpdate hits the GitHub releases API and reports whether a
// newer tagged release than the running build exists. Bound to Wails
// so the frontend can trigger a manual check from the menu.
//
// Network errors are returned to the caller; a "dev" build is not an
// error — it returns Skipped=true so the UI can show a sensible
// message instead of a misleading "up to date".
func (a *App) CheckForUpdate() (UpdateInfo, error) {
	settings, err := loadUpdateSettings()
	if err != nil {
		// A corrupt update.json must not silently downgrade the channel
		// to release and start offering the user a different kind of
		// update than the one they picked.
		return UpdateInfo{Stage: StageError, Message: err.Error()}, err
	}
	if settings.Channel == ChannelLatest {
		repo, err := resolveSourceRepo(settings.SourceRepo)
		if err != nil {
			return UpdateInfo{
				Channel: ChannelLatest,
				Current: buildinfo.BuildID(),
				Stage:   StageIdle,
				Skipped: true,
				Message: err.Error(),
			}, nil
		}
		info, err := checkLatest(repo)
		if err == nil {
			a.rememberCheck(info)
		}
		return info, err
	}
	info, err := a.checkRelease()
	if err == nil {
		a.rememberCheck(info)
	}
	return info, err
}

// checkRelease is the GitHub-releases channel: the original check, and
// still the default.
func (a *App) checkRelease() (UpdateInfo, error) {
	current := buildinfo.Version()
	info := UpdateInfo{Current: current, Channel: ChannelRelease, Stage: StageIdle}
	if current == "dev" {
		info.Skipped = true
		info.Message = "untagged build — switch to the latest channel to track your checkout"
		return info, nil
	}

	// Derive from a.ctx (the Wails runtime context) when set so an
	// in-flight check is cancelled at shutdown instead of holding the
	// process for ~5s. Falls back to Background for the rare case
	// where startup() hasn't run yet.
	parent := context.Background()
	if a.ctx != nil {
		parent = a.ctx
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, updateReleasesAPI, nil)
	if err != nil {
		return info, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "hivegui/"+buildinfo.BuildID())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return info, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return info, fmt.Errorf("github releases: HTTP %d", resp.StatusCode)
	}
	var rel struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return info, fmt.Errorf("decode release: %w", err)
	}
	latest := strings.TrimPrefix(rel.TagName, "v")
	info.Latest = latest
	if strings.HasPrefix(rel.HTMLURL, updateURLPrefix) {
		info.URL = rel.HTMLURL
	}
	// Available reflects ONLY whether there's a newer release. URL
	// trust is reported separately via info.URL (empty = don't show a
	// Download button) so a tampered html_url can't silently rewrite
	// "available" into "up to date".
	if latest != "" && compareSemver(current, latest) < 0 {
		info.Available = true
		info.Stage = StageAvailable
	}
	return info, nil
}

// startUpdateCheckLoop runs a periodic background check every
// updateCheckInterval and emits "update:available" on a positive
// result. The frontend handles the first-load check itself by
// calling CheckForUpdate() once on startup, so this loop is only
// responsible for catching releases that ship while the GUI is
// running. Failures are logged once and ignored — the next tick
// will retry.
func (a *App) startUpdateCheckLoop(ctx context.Context) {
	go func() {
		t := time.NewTicker(updateCheckInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				a.runUpdateCheck()
			}
		}
	}()
}

func (a *App) runUpdateCheck() {
	info, err := a.CheckForUpdate()
	if err != nil {
		log.Printf("hivegui: update check failed: %v", err)
		return
	}
	if info.Available && a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "update:available", info)
	}
}

// compareSemver returns -1 if a < b, 0 if equal, +1 if a > b. Inputs
// are dotted version strings without a leading "v" (e.g. "1.2.3").
// Pre-release suffixes (anything after "-") sort *before* the same
// version with no suffix: "1.0.0-rc1" < "1.0.0".
//
// Limitation: pre-release identifiers are compared lexically, so
// "rc10" sorts BEFORE "rc2". Hive doesn't ship -rcN tags through the
// release script today; if that ever changes, switch to semver.org
// rule 11 (numeric identifiers compared numerically).
//
// Unparseable components compare as 0 with whatever's been compared
// so far — so completely garbage input ("foo" vs "bar") returns 0
// rather than panicking.
func compareSemver(a, b string) int {
	aCore, aPre := splitPre(a)
	bCore, bPre := splitPre(b)
	aParts := strings.Split(aCore, ".")
	bParts := strings.Split(bCore, ".")
	n := len(aParts)
	if len(bParts) > n {
		n = len(bParts)
	}
	for i := 0; i < n; i++ {
		var av, bv int
		if i < len(aParts) {
			av, _ = strconv.Atoi(aParts[i])
		}
		if i < len(bParts) {
			bv, _ = strconv.Atoi(bParts[i])
		}
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}
	// Cores equal; pre-release loses to no-pre-release.
	switch {
	case aPre == "" && bPre == "":
		return 0
	case aPre == "" && bPre != "":
		return 1
	case aPre != "" && bPre == "":
		return -1
	default:
		return strings.Compare(aPre, bPre)
	}
}

func splitPre(v string) (core, pre string) {
	if i := strings.Index(v, "-"); i >= 0 {
		return v[:i], v[i+1:]
	}
	return v, ""
}
