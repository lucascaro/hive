// Package laya talks to a user-run Laya decision model and keeps the
// labelled screen corpus it is evaluated against (spec 458).
package laya

import "regexp"

// secretPatterns are the shapes a captured terminal screen most often
// leaks. Deliberately specific: a corpus of terminal screens is full of
// hashes, ids and base64 that are not secrets, and a pattern that
// redacts every long hex string would destroy the text a classifier
// needs. The LLM review pass (testdata/corpus/README.md) is the net for
// what these miss.
var secretPatterns = []struct {
	name string
	re   *regexp.Regexp
}{
	{"private key", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?(-----END [A-Z ]*PRIVATE KEY-----|$)`)},
	{"anthropic key", regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{16,}`)},
	{"openai-style key", regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}`)},
	{"github token", regexp.MustCompile(`\b(gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,})`)},
	{"aws access key", regexp.MustCompile(`\b(AKIA|ASIA)[A-Z0-9]{16}\b`)},
	{"slack token", regexp.MustCompile(`\bxox[abposr]-[A-Za-z0-9-]{10,}`)},
	{"google api key", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`)},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`)},
	{"bearer token", regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{16,}`)},
	{"assigned secret", regexp.MustCompile(`(?i)\b[A-Z0-9_]*(TOKEN|SECRET|PASSWORD|PASSWD|API_?KEY)[A-Z0-9_]*\s*[=:]\s*['"]?[^\s'"]{8,}`)},
	{"email", regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`)},
	{"home path", regexp.MustCompile(`(/Users/|/home/|C:\\Users\\)[^/\\\s]+`)},
}

// Scrub replaces every match of a secret pattern with a placeholder
// naming what was there. Home directories keep their prefix so a path
// still reads as a path.
func Scrub(s string) string {
	for _, p := range secretPatterns {
		if p.name == "home path" {
			s = p.re.ReplaceAllString(s, "${1}USER")
			continue
		}
		s = p.re.ReplaceAllString(s, "<redacted "+p.name+">")
	}
	return s
}

// Secrets lists the names of the secret patterns s still matches. A
// scrubbed "home path" (…/USER) is not a finding.
func Secrets(s string) []string {
	var found []string
	for _, p := range secretPatterns {
		for _, m := range p.re.FindAllStringSubmatch(s, -1) {
			if p.name == "home path" && (m[0] == m[1]+"USER") {
				continue
			}
			found = append(found, p.name)
			break
		}
	}
	return found
}
