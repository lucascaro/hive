package registry

import (
	"context"
	"encoding/json"
	"os/exec"
	"sync"
	"time"

	"github.com/lucascaro/hive/internal/worktree"
)

// GitHub PR state is the second opinion on "is this branch merged".
// Patch-id detection (worktree.squashMerged) misses a squash that was
// edited on the way in — conflict resolution, or a branch updated
// against main just before merging. GitHub knows regardless.
//
// A merged PR names a branch AND the commit that was merged. Both
// matter: the name alone says nothing about what the branch points at
// now. Someone who keeps committing after their PR merges — or who
// reuses a branch name, or whose PR came from a fork's "wip" — still
// has work that no merge contains, and treating that as merged would
// clear the unpushed refusal and delete it. So the OID is carried
// alongside the name and the local tip must be reachable from it.
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

// ghMerged is what a merged-PR listing yields: branch name to the
// commit GitHub merged. Use ghConfirms rather than indexing it — a
// name match on its own is not proof the branch is merged.
type ghMerged map[string]string

// ghConfirms reports whether GitHub says this branch was merged AND
// the branch still points inside that merge. Anything the local repo
// cannot verify (OID never fetched, branch moved on) reports false,
// which leaves the branch looking unmerged.
func ghConfirms(repoRoot, branch string, merged ghMerged) bool {
	oid := merged[branch]
	if oid == "" {
		return false
	}
	return worktree.IsAncestor(repoRoot, branch, oid)
}

type ghCacheEntry struct {
	at  time.Time
	set ghMerged
}

var (
	ghCacheMu sync.Mutex
	ghCache   = map[string]ghCacheEntry{}
)

// ghMergedHeads returns the head branch and merged commit of every
// merged pull request in the repo at root, or nil when GitHub cannot
// be asked. Cached per repo for ghCacheTTL — every worktree mutation
// re-lists, and the modal must not pay a round trip each time.
func ghMergedHeads(root string) ghMerged {
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

func queryGHMergedHeads(root string) ghMerged {
	ctx, cancel := context.WithTimeout(context.Background(), ghTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", "pr", "list",
		"--state", "merged", "--limit", ghPRLimit,
		"--json", "headRefName,headRefOid")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var prs []struct {
		HeadRefName string `json:"headRefName"`
		HeadRefOid  string `json:"headRefOid"`
	}
	if err := json.Unmarshal(out, &prs); err != nil {
		return nil
	}
	set := ghMerged{}
	for _, pr := range prs {
		if pr.HeadRefName != "" && pr.HeadRefOid != "" {
			// Newest wins: a reused branch name's latest merge is the
			// one whose OID the local tip could still be inside.
			if _, seen := set[pr.HeadRefName]; !seen {
				set[pr.HeadRefName] = pr.HeadRefOid
			}
		}
	}
	return set
}
