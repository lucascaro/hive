package buildinfo

// DaemonContract is the compatibility generation of everything the
// daemon exposes to a client: the wire frames it understands, the
// session semantics behind them, and the registry state it persists.
//
// Bump it whenever a GUI built against the new tree cannot correctly
// drive a daemon built against the old one. Do NOT bump it for
// GUI-only changes, or for daemon-side changes a client cannot
// observe (a refactor, a log line, a comment): a needless bump costs
// the user every running session, because the GUI answers a bump by
// demanding a full restart instead of a cheap reload.
//
// This is deliberately NOT BuildID. BuildID is a git revision, so it
// changes for a CSS tweak — comparing it treated every rebuild as a
// stale daemon, which is the bug this constant exists to fix. It is
// also not wire.PROTOCOL_VERSION: that one is a hard gate (the daemon
// refuses a mismatched HELLO outright, see internal/daemon), so it
// cannot express "these two can still talk, but the GUI should
// restart the daemon to pick up new behavior".
//
// scripts/check-daemon-contract.sh fails CI on a PR that touches
// daemon-side code without changing this value.
const DaemonContract = 1

// Identity is this binary's full build identity. `hived --version
// --json` prints it, and Welcome carries the same three values, so a
// GUI can interrogate BOTH the daemon it is talking to and a staged
// update bundle it has not installed yet — and decide between a
// GUI-only reload and a full restart before the user commits to
// either.
type Identity struct {
	Release        string `json:"release"`
	BuildID        string `json:"build_id"`
	DaemonContract int    `json:"daemon_contract"`
}

// CurrentIdentity returns this binary's Identity.
func CurrentIdentity() Identity {
	return Identity{
		Release:        Version(),
		BuildID:        BuildID(),
		DaemonContract: DaemonContract,
	}
}
