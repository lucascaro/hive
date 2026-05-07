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
// the "update:available" Wails event. Frontend reads .Available to
// decide whether to show the banner; .URL goes to OpenURL on click.
type UpdateInfo struct {
	Available bool   `json:"available"`
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	URL       string `json:"url"`
	// Skipped is true when the running build is "dev" (untagged) — the
	// frontend uses this to differentiate a real "you're up to date"
	// from "we can't tell" in the manual-check flow.
	Skipped bool `json:"skipped"`
}

// CheckForUpdate hits the GitHub releases API and reports whether a
// newer tagged release than the running build exists. Bound to Wails
// so the frontend can trigger a manual check from the menu.
//
// Network errors are returned to the caller; a "dev" build is not an
// error — it returns Skipped=true so the UI can show a sensible
// message instead of a misleading "up to date".
func (a *App) CheckForUpdate() (UpdateInfo, error) {
	current := buildinfo.Version()
	info := UpdateInfo{Current: current}
	if current == "dev" {
		info.Skipped = true
		return info, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
	if latest != "" && info.URL != "" && compareSemver(current, latest) < 0 {
		info.Available = true
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
