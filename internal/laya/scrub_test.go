package laya

import (
	"strings"
	"testing"
)

func TestScrubRedactsEachShape(t *testing.T) {
	for name, in := range map[string]string{
		"anthropic key":    "export X=sk-ant-api03-abcdefghijklmnopqrstuv",
		"openai-style key": "key sk-proj-abcdefghijklmnopqrstuvwx",
		"github token":     "ghp_abcdefghijklmnopqrstuvwxyz0123",
		"aws access key":   "AKIAABCDEFGHIJKLMNOP",
		"slack token":      "xoxb-1234567890-abcdef",
		"jwt":              "eyJhbGciOiJIUzI1.eyJzdWIiOiIxMjM0.SflKxwRJSMeKKF2Q",
		"bearer token":     "Authorization: Bearer abcdef0123456789abcdef",
		"assigned secret":  "DB_PASSWORD=hunter2hunter2",
		"email":            "commit by someone@example.com",
		"private key":      "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXk\n-----END OPENSSH PRIVATE KEY-----",
	} {
		out := Scrub(in)
		if got := Secrets(out); len(got) != 0 {
			t.Errorf("%s: still found %v after scrub: %q", name, got, out)
		}
		if got := Secrets(in); len(got) == 0 {
			t.Errorf("%s: Secrets found nothing in %q", name, in)
		}
	}
}

func TestScrubKeepsHomePathsReadable(t *testing.T) {
	out := Scrub("cd /Users/alice/code && ls /home/bob")
	if out != "cd /Users/USER/code && ls /home/USER" {
		t.Errorf("Scrub = %q", out)
	}
	if got := Secrets(out); len(got) != 0 {
		t.Errorf("a scrubbed home path is still reported: %v", got)
	}
}

// Terminal screens are full of hex and ids that are not secrets; the
// scrubber must leave the text a classifier needs.
func TestScrubLeavesOrdinaryScreens(t *testing.T) {
	in := strings.Join([]string{
		"commit 3fa9c1d2e4b5a6978f0e1d2c3b4a5968778695a4",
		"Allow this command? (y/n)  npm test",
		"sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
		"> ",
	}, "\n")
	if out := Scrub(in); out != in {
		t.Errorf("Scrub changed an ordinary screen:\n%s", out)
	}
}
