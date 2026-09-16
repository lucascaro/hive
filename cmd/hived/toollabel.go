package main

import (
	"net/url"
	"strings"

	"github.com/lucascaro/hive/internal/wire"
)

// Tool label derivation — the privacy rule from
// docs/design-docs/agent-activity.md, enforced at the reporter.
//
// tool_input routinely contains secrets: full shell command lines,
// file contents, URLs with tokens. It never crosses the socket. This
// file turns it into a short label — a basename, a command head, a URL
// host — and only that label is sent. The daemon receives a tool name
// and a label, so there is nothing for it to strip and nothing for it
// to forget to strip.
//
// Enforcing it here rather than in the daemon is the whole point: the
// ring is readable by every GUI window and by hivebar, and a redaction
// the daemon performs is a redaction the daemon can regress.

// labelKeys is an ALLOWLIST, and that direction matters. A tool whose
// arguments we do not recognise yields no label at all, so a new
// Claude tool cannot silently start shipping its arguments over the
// socket the day it ships. The cost is that a new tool reads as
// "Bash" with no detail until someone adds its key here.
var labelKeys = []struct {
	key    string
	derive func(string) string
}{
	// Shell commands. Head only — see commandHead.
	{"command", commandHead},
	// Paths, from the file tools.
	{"file_path", baseName},
	{"path", baseName},
	{"notebook_path", baseName},
	// Network fetches: the host, never the query string, which is
	// where tokens live.
	{"url", urlHost},
}

// deriveToolTarget produces the label for one tool call, or "" when
// nothing in the input is on the allowlist.
//
// input is whatever came out of the hook payload's tool_input. It is
// typed as any because the payload is decoded loosely and tool_input
// is routinely absent, null, or (from a malformed hook) a bare string.
func deriveToolTarget(input any) string {
	m, ok := input.(map[string]any)
	if !ok {
		// Missing, null, or not an object. No label — deliberately not
		// a stringification of whatever it was, which is exactly how
		// raw arguments would leak.
		return ""
	}
	for _, lk := range labelKeys {
		v, ok := m[lk.key].(string)
		if !ok || v == "" {
			continue
		}
		if out := lk.derive(v); out != "" {
			return capLabel(out)
		}
	}
	return ""
}

// commandHead takes the first token of a shell command, plus the
// second when it reads as a subcommand rather than an argument.
//
// This is deliberately stricter than "everything before the first
// shell metacharacter", which is the obvious implementation and which
// leaks: `curl -H "Authorization: Bearer sk-live-…"` has no
// metacharacter in it at all, so that rule would have sent the whole
// credential. Here it yields "curl".
//
// A shell metacharacter still terminates the head — otherwise
// `sleep 12; touch x` would render as `sleep 12;` with the separator
// glued on — but terminating there is NOT sufficient on its own,
// which is why the two-token rule runs afterwards.
//
//	npm test                      -> "npm test"
//	git commit -m "secret"        -> "git commit"
//	curl -H "Authorization: …"    -> "curl"
//	sleep 12; touch probe.txt     -> "sleep 12"
func commandHead(cmd string) string {
	if i := strings.IndexAny(cmd, ";|&\n\r<>()"); i >= 0 {
		cmd = cmd[:i]
	}
	fields := strings.Fields(cmd)
	// Leading NAME=value words are environment assignments, not the
	// command — and they are exactly where an inline secret goes:
	// `GITHUB_TOKEN=ghp_… gh api user`. Taken as the head, the token was
	// the label. Skip them, as the shell does.
	for len(fields) > 0 && isEnvAssignment(fields[0]) {
		fields = fields[1:]
	}
	if len(fields) == 0 {
		return ""
	}
	// The command word gets checked too. A path is reduced to its
	// basename, like any other path label: `/home/alice/bin/tool` must
	// not name the user. Anything still carrying expansion, quoting or a
	// credential shape yields no label at all rather than a guess.
	head := baseName(fields[0])
	if !isCommandName(head) {
		return ""
	}
	if len(fields) > 1 && isSubcommand(fields[1]) {
		head += " " + fields[1]
	}
	return head
}

// isEnvAssignment reports whether a shell word is a NAME=value
// assignment: a valid variable name followed by `=`.
func isEnvAssignment(tok string) bool {
	name, _, found := strings.Cut(tok, "=")
	if !found || name == "" {
		return false
	}
	for i, r := range name {
		letter := r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
		if !letter && (i == 0 || r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// isCommandName reports whether a (basenamed) command word is safe to
// show. It is looser than isSubcommand — real executables carry digits
// and dots (`python3`, `node20`, `deploy.sh`) — but refuses anything that
// is not a plain name: an unexpanded `$VAR`, a quote, an `=`, `@` or `:`.
func isCommandName(tok string) bool {
	if tok == "" {
		return false
	}
	return !strings.ContainsAny(tok, `=$"'@:`+"`")
}

// maxSubcommandLen bounds a second word. Real subcommands are short
// (`test`, `commit`, `cherry-pick`); a long token in that position is far
// more likely to be an argument — an ID, a key, a hostname.
const maxSubcommandLen = 20

// isSubcommand reports whether a token reads as a subcommand (`test`,
// `commit`) rather than data — a flag, a path, an assignment, a quoted
// value, or something shaped like a credential or a host. Refusal is the
// conservative direction: a false negative costs one word of context,
// a false positive leaks.
//
// Rejected:
//   - flags (`-x`), and anything containing a path separator, `=`, `$`, a
//     quote or a backtick — the original rule;
//   - `@` and `:` — `deploy@prod-db`, `host:port`, `user:token`;
//   - `.` — a filename (`manage.py`, `prod.tfvars`, `id_rsa.pub`) is an
//     argument, and a sensitive one as often as not; subcommands do not
//     carry a dot;
//   - any digit — API keys, tokens, IDs and version pins almost always
//     carry one, and subcommands almost never do;
//   - two or more `-`/`_` separators — the shape of key prefixes like
//     `sk-live-…`, `xoxb-…-…`, `ghp_…`; one is allowed, for `cherry-pick`;
//   - longer than maxSubcommandLen.
//
// ponytail: this is a heuristic about what a secret LOOKS like, chosen
// by the operator over a fixed allowlist of known subcommands. Its
// ceiling is known and stated rather than hidden: a short, all-letter
// secret with at most one separator (`mytool AbCdEfGhIjKlMnOp`) still
// passes. The upgrade path, if that ever matters, is the allowlist —
// the same direction labelKeys already takes, where unknown means no
// label.
func isSubcommand(tok string) bool {
	if tok == "" || strings.HasPrefix(tok, "-") || len(tok) > maxSubcommandLen {
		return false
	}
	if strings.ContainsAny(tok, `/\=$"'@:.`+"`") {
		return false
	}
	if strings.ContainsAny(tok, "0123456789") {
		return false
	}
	return strings.Count(tok, "-")+strings.Count(tok, "_") < 2
}

// baseName is a separator-agnostic basename. filepath.Base is not
// usable here: it honours only the separator of the platform it was
// compiled for, so a Windows path reported to a daemon on macOS would
// render whole — `internal\agentstate\machine.go` rather than
// `machine.go`. The Pi extension's TypeScript half must match this.
func baseName(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// urlHost keeps the host and discards everything else. The path and
// the query are where credentials appear (`?token=…`), and the host
// alone answers "what is it talking to".
func urlHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		// Unparseable, or a relative reference with no host. No label
		// rather than a guess — returning the raw string here would
		// defeat the entire rule.
		return ""
	}
	return u.Host
}

// capLabel bounds the label. A long one means the derivation
// over-captured, so this is a backstop against a leak as much as a
// size limit. Cut on a rune boundary: the content is agent-authored.
func capLabel(s string) string {
	if len(s) <= wire.MaxTargetLen {
		return s
	}
	cut := wire.MaxTargetLen
	for cut > 0 && s[cut]&0xC0 == 0x80 {
		cut--
	}
	return s[:cut]
}
