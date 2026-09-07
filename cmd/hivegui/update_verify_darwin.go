package main

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/lucascaro/hive/internal/buildinfo"
)

// Absolute paths, deliberately. `xcrun stapler` is the obvious way to
// check a notarization ticket and it is the wrong one here: stapler
// ships with the Xcode Command Line Tools, not with macOS, so on an
// ordinary user's Mac it fails with "xcrun: unable to find utility".
// This check is fail-closed — a failure discards the download — so
// reaching for a tool most users do not have would refuse every
// update for almost everyone. codesign and spctl are both base OS.
const (
	codesignBin = "/usr/bin/codesign"
	spctlBin    = "/usr/sbin/spctl"
)

// verifyTimeout bounds each verification command. spctl contacts
// Apple's notary service when a staple is missing or unreadable, and
// on a captive-portal network that call can hang indefinitely — which
// would wedge the staging goroutine rather than failing it. Var so a
// test can shrink it.
var verifyTimeout = 60 * time.Second

// runArgv is the seam tests replace. It returns combined output so a
// codesign rejection reaches the user instead of a bare exit code.
var runArgv = func(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return string(out), err
}

// teamIDRequirement builds the codesign requirement that pins a
// bundle to one Apple Developer team.
//
// This string is the entire security value of the check. Plain
// `codesign --verify` proves only that *some* valid Developer ID
// signed the bundle — an attacker who pays Apple $99 satisfies it.
// Pinning subject.OU (the Team ID) is what makes it mean "signed by
// us".
func teamIDRequirement(teamID string) string {
	return fmt.Sprintf("=anchor apple generic and certificate leaf[subject.OU] = %q", teamID)
}

// verifyDeveloperIDSignature refuses a staged bundle that is not
// signed by Hive's Apple Developer team.
//
// Two checks, with different standing:
//
//   - codesign with the Team ID requirement is load-bearing. A failure
//     always refuses the update.
//   - spctl --assess reports on notarization, and is advisory. Under
//     `spctl --master-disable` it does not evaluate at all — it prints
//     "assessments are disabled" — so treating its non-answer as a
//     failure would refuse updates on a machine whose owner has
//     already opted out of Gatekeeper globally, and treating it as
//     authoritative would let that same setting silently turn this
//     half of the check into a no-op. It is logged, not enforced; the
//     Team ID pin still holds either way.
//
// Returns nil when this build has no pin (see buildinfo.signingTeamID).
func verifyDeveloperIDSignature(ctx context.Context, bundle string) error {
	teamID := buildinfo.SigningTeamID()
	if teamID == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, verifyTimeout)
	defer cancel()

	out, err := runArgv(ctx, codesignBin,
		"--verify", "--deep", "--strict",
		"-R", teamIDRequirement(teamID),
		bundle)
	if err != nil {
		detail := strings.TrimSpace(out)
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("this update is not signed by the Hive developer (team %s) — download discarded: %s", teamID, detail)
	}

	if out, err := runArgv(ctx, spctlBin, "--assess", "--type", "execute", bundle); err != nil {
		log.Printf("hivegui: notarization assessment did not pass for %s (advisory, Team ID pin held): %v: %s",
			bundle, err, strings.TrimSpace(out))
	}
	return nil
}
