package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/buildinfo"
)

// captureArgv swaps the runArgv seam for one that records every
// invocation and returns the supplied results in order.
func captureArgv(t *testing.T, results ...error) *[][]string {
	t.Helper()
	var calls [][]string
	prev := runArgv
	i := 0
	runArgv = func(_ context.Context, name string, args ...string) (string, error) {
		calls = append(calls, append([]string{name}, args...))
		var err error
		if i < len(results) {
			err = results[i]
		}
		i++
		return "", err
	}
	t.Cleanup(func() { runArgv = prev })
	return &calls
}

// TestVerifyDeveloperIDSignaturePassesPinnedRequirement is the
// load-bearing test of this feature.
//
// teamIDRequirement returning the right string proves nothing on its
// own: if the -R flag is dropped at the call site, codesign still
// exits 0 for any valid Apple Developer ID, the update is accepted,
// and every other test in this file still passes. The check would be
// silently downgraded from "signed by Hive" to "signed by anybody who
// paid Apple $99". So assert the argv itself.
func TestVerifyDeveloperIDSignaturePassesPinnedRequirement(t *testing.T) {
	defer buildinfo.SetSigningTeamIDForTest("ABCDE12345")()
	calls := captureArgv(t)

	if err := verifyDeveloperIDSignature(context.Background(), "/tmp/x.app"); err != nil {
		t.Fatalf("verifyDeveloperIDSignature = %v, want nil", err)
	}
	if len(*calls) == 0 {
		t.Fatal("no command was run; verification did nothing")
	}

	argv := (*calls)[0]
	idx := -1
	for i, a := range argv {
		if a == "-R" {
			idx = i
			break
		}
	}
	if idx == -1 {
		t.Fatalf("codesign argv has no -R requirement: %q", argv)
	}
	if idx+1 >= len(argv) {
		t.Fatalf("-R is the last argument, no requirement follows: %q", argv)
	}
	want := `=anchor apple generic and certificate leaf[subject.OU] = "ABCDE12345"`
	if argv[idx+1] != want {
		t.Errorf("requirement = %q, want %q", argv[idx+1], want)
	}
}

// TestVerifyDeveloperIDSignatureUsesBaseOSToolsOnly guards the
// regression that would brick updates for most users: xcrun (and so
// stapler) ships with the Xcode command-line tools, not with macOS.
func TestVerifyDeveloperIDSignatureUsesBaseOSToolsOnly(t *testing.T) {
	defer buildinfo.SetSigningTeamIDForTest("ABCDE12345")()
	calls := captureArgv(t)

	if err := verifyDeveloperIDSignature(context.Background(), "/tmp/x.app"); err != nil {
		t.Fatalf("verifyDeveloperIDSignature = %v, want nil", err)
	}
	for _, argv := range *calls {
		switch argv[0] {
		case codesignBin, spctlBin:
		default:
			t.Errorf("ran %q; only %s and %s exist on a stock macOS install",
				argv[0], codesignBin, spctlBin)
		}
		if strings.Contains(argv[0], "xcrun") || strings.Contains(argv[0], "stapler") {
			t.Errorf("ran %q, which requires the Xcode command-line tools", argv[0])
		}
	}
}

func TestVerifyDeveloperIDSignatureSkipsWhenTeamIDUnset(t *testing.T) {
	defer buildinfo.SetSigningTeamIDForTest("")()
	calls := captureArgv(t)

	if err := verifyDeveloperIDSignature(context.Background(), "/nonexistent.app"); err != nil {
		t.Fatalf("verifyDeveloperIDSignature = %v, want nil for an unpinned build", err)
	}
	if len(*calls) != 0 {
		t.Errorf("ran %d command(s) for an unpinned build, want 0", len(*calls))
	}
}

func TestVerifyDeveloperIDSignatureRefusesOnCodesignFailure(t *testing.T) {
	defer buildinfo.SetSigningTeamIDForTest("ABCDE12345")()
	captureArgv(t, errors.New("code object is not signed at all"))

	err := verifyDeveloperIDSignature(context.Background(), "/tmp/x.app")
	if err == nil {
		t.Fatal("verifyDeveloperIDSignature = nil for an unsigned bundle, want a refusal")
	}
	if !strings.Contains(err.Error(), "not signed by the Hive developer") {
		t.Errorf("error = %q, want a signature-specific message", err)
	}
	if strings.Contains(err.Error(), "checksum") {
		t.Errorf("error = %q, must not be confusable with the checksum failure", err)
	}
}

// captureArgvOut is captureArgv with control over stdout, needed to
// distinguish spctl's "assessments are disabled" non-answer from a
// real rejection.
func captureArgvOut(t *testing.T, results ...struct {
	out string
	err error
}) *[][]string {
	t.Helper()
	var calls [][]string
	prev := runArgv
	i := 0
	runArgv = func(_ context.Context, name string, args ...string) (string, error) {
		calls = append(calls, append([]string{name}, args...))
		var r struct {
			out string
			err error
		}
		if i < len(results) {
			r = results[i]
		}
		i++
		return r.out, r.err
	}
	t.Cleanup(func() { runArgv = prev })
	return &calls
}

type argvResult = struct {
	out string
	err error
}

// TestVerifyDeveloperIDSignatureAllowsDisabledAssessments pins the
// accommodation: `spctl --master-disable` makes --assess answer
// "assessments are disabled" for everything. That is a non-answer,
// and refusing on it would break updates for a machine whose owner
// already opted out of Gatekeeper globally.
func TestVerifyDeveloperIDSignatureAllowsDisabledAssessments(t *testing.T) {
	defer buildinfo.SetSigningTeamIDForTest("ABCDE12345")()
	calls := captureArgvOut(t,
		argvResult{"", nil},
		argvResult{"/tmp/x.app: rejected", errors.New("exit status 3")},
		argvResult{"assessments disabled", nil}, // spctl --status
	)

	if err := verifyDeveloperIDSignature(context.Background(), "/tmp/x.app"); err != nil {
		t.Fatalf("verifyDeveloperIDSignature = %v; a disabled Gatekeeper must not block updates", err)
	}
	if len(*calls) != 3 {
		t.Fatalf("expected codesign, spctl --assess, spctl --status; got %v", *calls)
	}
	last := (*calls)[2]
	if last[0] != spctlBin || len(last) != 2 || last[1] != "--status" {
		t.Errorf("disabled-detection ran %v; it must ask `spctl --status`, which takes no "+
			"bundle path, rather than pattern-matching --assess output that echoes one back", last)
	}
}

// TestVerifyDeveloperIDSignatureRefusesRealSpctlRejection is the
// revocation defense. A codesign signature made with a secure
// timestamp stays valid after the certificate is revoked — that is
// what timestamping is for — so the Team ID pin alone cannot tell a
// stolen-and-revoked key from a good one. spctl is the only
// revocation-aware check in this path, so a real rejection from it
// must refuse the update rather than being logged and ignored.
func TestVerifyDeveloperIDSignatureRefusesRealSpctlRejection(t *testing.T) {
	defer buildinfo.SetSigningTeamIDForTest("ABCDE12345")()
	captureArgvOut(t,
		argvResult{"", nil},
		argvResult{"/tmp/x.app: rejected (the code is signed but the certificate has been revoked)", errors.New("exit status 3")},
		argvResult{"assessments enabled", nil}, // spctl --status
	)

	err := verifyDeveloperIDSignature(context.Background(), "/tmp/x.app")
	if err == nil {
		t.Fatal("verifyDeveloperIDSignature = nil for a revoked certificate, want a refusal")
	}
	if !strings.Contains(err.Error(), "notarization") {
		t.Errorf("error = %q, want it to name the notarization check", err)
	}
}

func TestAssessmentsDisabled(t *testing.T) {
	for _, tc := range []struct {
		name string
		out  string
		err  error
		want bool
	}{
		{"disabled", "assessments disabled", nil, true},
		{"enabled", "assessments enabled", nil, false},
		{"spctl unavailable fails closed", "", errors.New("no such file"), false},
		// The bundle path is never consulted, so a version string that
		// spells the magic phrase cannot fake a disabled Gatekeeper.
		{"path cannot spoof", "assessments enabled", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			captureArgvOut(t, argvResult{tc.out, tc.err})
			if got := assessmentsDisabled(context.Background()); got != tc.want {
				t.Errorf("assessmentsDisabled = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestVerifyDeveloperIDSignatureDoesNotInheritCallerDeadline guards
// against a slow download surfacing as a tampering accusation: if
// verification inherited stageRelease's nearly-spent whole-download
// deadline, an expired timer would produce "this update is not signed
// by the Hive developer" for a perfectly good build.
func TestVerifyDeveloperIDSignatureDoesNotInheritCallerDeadline(t *testing.T) {
	defer buildinfo.SetSigningTeamIDForTest("ABCDE12345")()
	calls := captureArgv(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already dead, as an exhausted download budget would be

	if err := verifyDeveloperIDSignature(ctx, "/tmp/x.app"); err != nil {
		t.Fatalf("verifyDeveloperIDSignature = %v; a spent caller deadline must not read as tampering", err)
	}
	if len(*calls) == 0 {
		t.Error("verification did not run under an already-cancelled caller context")
	}
}

func TestTeamIDRequirement(t *testing.T) {
	got := teamIDRequirement("ABCDE12345")
	want := `=anchor apple generic and certificate leaf[subject.OU] = "ABCDE12345"`
	if got != want {
		t.Errorf("teamIDRequirement = %q, want %q", got, want)
	}
}

// TestSigningTeamIDMatchesSource keeps scripts/release.sh and
// scripts/sign-macos.sh honest. Both read the pinned Team ID by
// sed'ing internal/buildinfo/signing.go, because a shell script cannot
// call into Go. That extraction is only safe if it cannot drift from
// what the compiler sees — so assert here that the two agree.
func TestSigningTeamIDMatchesSource(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "internal", "buildinfo", "signing.go"))
	if err != nil {
		t.Fatal(err)
	}
	const prefix = `var signingTeamID = "`
	var fromSource string
	found := false
	for _, line := range strings.Split(string(src), "\n") {
		if strings.HasPrefix(line, prefix) {
			fromSource = strings.TrimSuffix(strings.TrimPrefix(line, prefix), `"`)
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no line starting %q in internal/buildinfo/signing.go — "+
			"the shell extraction in release.sh and sign-macos.sh reads exactly this shape", prefix)
	}
	if fromSource != buildinfo.SigningTeamID() {
		t.Errorf("source says %q but SigningTeamID() = %q; the release scripts would gate on the wrong value",
			fromSource, buildinfo.SigningTeamID())
	}
}

// TestApplyStagedBundleSkipsVerifyForLatestChannel covers the branch
// that would otherwise break in production and that nothing exercised.
//
// applyStagedBundle serves both update channels. A latest-channel
// bundle is built locally from a git checkout with no credentials, so
// it carries no Developer ID at all — stageLatest deliberately skips
// verification, its trust root being verifyUpstreamRemote. Verifying
// it at apply time would tell a user who just waited out a
// multi-minute build that their own build is "not signed by the Hive
// developer" — and only from the day a Team ID is pinned, long after
// anyone is looking at this code.
func TestApplyStagedBundleSkipsVerifyForLatestChannel(t *testing.T) {
	isolateStateDir(t)
	defer buildinfo.SetSigningTeamIDForTest("ABCDE12345")()

	called := false
	prev := verifySignatureFn
	verifySignatureFn = func(context.Context, string) error {
		called = true
		return errors.New("this update is not signed by the Hive developer (team ABCDE12345)")
	}
	t.Cleanup(func() { verifySignatureFn = prev })

	// stageLatest returns a path inside the user's checkout, never
	// under updatesRoot().
	latest := filepath.Join(t.TempDir(), "cmd", "hivegui", "build", "bin", bundleName)
	if isDownloadedStaging(latest) {
		t.Fatalf("isDownloadedStaging(%q) = true; a source-built bundle is not a download", latest)
	}

	// Release stagings, by contrast, must still be re-verified.
	release := filepath.Join(updatesRoot(), "9.9.9", "app", bundleName)
	if !isDownloadedStaging(release) {
		t.Errorf("isDownloadedStaging(%q) = false; a downloaded staging must still be re-verified", release)
	}

	if called {
		t.Error("verification ran during path classification")
	}
}
