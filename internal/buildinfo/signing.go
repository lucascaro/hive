package buildinfo

// signingTeamID is the Apple Developer Team ID that macOS release
// builds are signed with. Unlike buildIDOverride and versionOverride
// above, this is a committed constant rather than a link-time stamp,
// and that difference is deliberate.
//
// The in-app updater refuses a downloaded release whose signature does
// not chain to this team (see cmd/hivegui/update_verify_darwin.go). If
// the value arrived via -ldflags from scripts/release.sh, then every
// build made without those flags would carry an empty pin and silently
// skip that check — and those builds are not rare. The GUI's
// latest-commit updater rebuilds from a git checkout by running
// ./build.sh with no credentials (stageLatest), and so does anyone
// building on a second machine or from a fork. Baking the ID in means
// a credential-free build still verifies the releases it downloads.
//
// Empty disables verification. scripts/release.sh refuses to publish
// while it is empty, so an unpinned build cannot reach users.
//
// A var rather than a const so tests can override it; nothing else
// writes to it.
var signingTeamID = "2ZY25TNMX6"

// SigningTeamID returns the Apple Developer Team ID that release
// builds are signed with, or "" when this build has no pin.
func SigningTeamID() string { return signingTeamID }

// SetSigningTeamIDForTest overrides SigningTeamID() for the lifetime
// of the caller's test. Defer the returned restore function (or pass
// it to t.Cleanup). Tests that mutate this must not run with
// t.Parallel().
func SetSigningTeamIDForTest(value string) (restore func()) {
	prev := signingTeamID
	signingTeamID = value
	return func() { signingTeamID = prev }
}
