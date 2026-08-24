package registry

import (
	"context"
	"encoding/json"
	"os/exec"
	"sync"
	"time"
)

// GitHub PR state is the second opinion on "is this branch merged".
// Patch-id detection (worktree.squashMerged) misses a squash that was
// edited on the way in — conflict resolution, or a branch updated
// against main just before merging. GitHub knows regardless.
//
// This is the only network call in the worktree browser, and it is
// strictly additive: every failure mode (gh not installed, not
// authenticated, offline, not a GitHub remote) yields nil, which can
// only leave branches looking unmerged. It never marks one unmerged.

const (
	ghTimeout  = 5 * time.Second
	ghCacheTTL = 60 * time.Second
	// One call covers the repo; 200 is generous for "recently merged"
	// while keeping the response small.
	ghPRLimit = "200"
)

// ghMergedLookup is the seam tests stub. Production points at
// ghMergedHeads.
var ghMergedLookup = ghMergedHeads

type ghCacheEntry struct {
	at  time.Time
	set map[string]bool
}

var (
	ghCacheMu sync.Mutex
	ghCache   = map[string]ghCacheEntry{}
)

// ghMergedHeads returns the head branch names of merged pull requests
// in the repo at root, or nil when GitHub cannot be asked. Cached per
// repo for ghCacheTTL — every worktree mutation re-lists, and the
// modal must not pay a round trip each time.
func ghMergedHeads(root string) map[string]bool {
	if root == "" {
		return nil
	}
	ghCacheMu.Lock()
	if e, ok := ghCache[root]; ok && time.Since(e.at) < ghCacheTTL {
		ghCacheMu.Unlock()
		return e.set
	}
	ghCacheMu.Unlock()

	set := queryGHMergedHeads(root)

	ghCacheMu.Lock()
	ghCache[root] = ghCacheEntry{at: time.Now(), set: set}
	ghCacheMu.Unlock()
	return set
}

func queryGHMergedHeads(root string) map[string]bool {
	ctx, cancel := context.WithTimeout(context.Background(), ghTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", "pr", "list",
		"--state", "merged", "--limit", ghPRLimit, "--json", "headRefName")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var prs []struct {
		HeadRefName string `json:"headRefName"`
	}
	if err := json.Unmarshal(out, &prs); err != nil {
		return nil
	}
	set := map[string]bool{}
	for _, pr := range prs {
		if pr.HeadRefName != "" {
			set[pr.HeadRefName] = true
		}
	}
	return set
}
