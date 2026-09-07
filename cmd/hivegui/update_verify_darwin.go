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
//
//   - spctl --assess reports on notarization, and is advisory. Under
//     `spctl --master-disable` it does not evaluate at all — it prints
//     "assessments are disabled" — so refusing on that would break
//     updates for a machine whose owner already opted out of
//     Gatekeeper globally.
//
//     But it must not be merely advisory either, and the reason is
//     revocation. A `codesign` signature made with a secure timestamp
//     stays valid after the certificate is revoked — that is what
//     timestamping is for. So the Team ID pin alone cannot tell a
//     stolen-and-revoked key from a good one, and revoking a
//     compromised Developer ID (which docs/releasing-signed-macos.md
//     prescribes) would do nothing for the updater. spctl is the only
//     revocation-aware check in this path.
//
//     So the two cases are separated: a non-evaluating spctl is
//     advisory, an actively rejecting one refuses the update.
//
// Returns nil when this build has no pin (see buildinfo.signingTeamID).
func verifyDeveloperIDSignature(ctx context.Context, bundle string) error {
	teamID := buildinfo.SigningTeamID()
	if teamID == "" {
		return nil
	}

	// Deliberately detached from the caller's context. stageRelease's
	// ctx carries the whole-download deadline, which by this point may
	// have minutes or seconds left; inheriting it would let a slow
	// download surface as "this update is not signed by the Hive
	// developer" — accusing the developer of tampering because a
	// timer ran out. Verification gets its own budget.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), verifyTimeout)
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
		detail := strings.TrimSpace(out)
		if assessmentsDisabled(detail) {
			log.Printf("hivegui: Gatekeeper assessments are disabled on this machine, so notarization "+
				"could not be checked for %s. The Team ID pin passed and the update was accepted; "+
				"a revoked certificate would not be detected.", bundle)
			return nil
		}
		return fmt.Errorf("this update failed Apple's notarization check — download discarded: %s", detail)
	}
	return nil
}

// assessmentsDisabled reports whether spctl declined to evaluate
// rather than actually rejecting the bundle.
//
// `spctl --master-disable` makes --assess answer "assessments are
// disabled" for everything. That is a non-answer, not a verdict, and
// it must not be confused with a real rejection — one means "this
// machine opted out of Gatekeeper", the other means "Apple says no".
func assessmentsDisabled(out string) bool {
	return strings.Contains(strings.ToLower(out), "assessments are disabled")
}
