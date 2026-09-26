package laya

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// The labelled corpus: testdata/corpus/<agent>/<state>/<name>.txt, one
// captured screen per file, its directory its true label. See
// testdata/corpus/README.md for how screens get in here.
const corpusDir = "testdata/corpus"

var (
	// corpusAgents may appear in the corpus; requiredAgents must. Aider
	// is allowed but not required: it was not installed where the first
	// corpus was captured (spec 458 Decision log).
	corpusAgents   = []string{"aider", "codex", "shell", "pi"}
	requiredAgents = []string{"codex", "shell", "pi"}
	corpusStates   = []string{"working", "idle", "waiting_input", "waiting_permission", "error"}
)

type sample struct {
	agent, state, path, text string
}

func loadCorpus(t *testing.T) []sample {
	t.Helper()
	var out []sample
	err := filepath.WalkDir(corpusDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".txt") {
			return err
		}
		rel, _ := filepath.Rel(corpusDir, path)
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) != 3 {
			t.Errorf("%s: want <agent>/<state>/<name>.txt", path)
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out = append(out, sample{agent: parts[0], state: parts[1], path: path, text: string(raw)})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// Spec criterion 1: the corpus covers every required agent and every
// state.
// Not every pair — a plain shell never waits for tool permission.
func TestCorpusCoverage(t *testing.T) {
	seenAgent, seenState := map[string]int{}, map[string]int{}
	for _, s := range loadCorpus(t) {
		seenAgent[s.agent]++
		seenState[s.state]++
		if !contains(corpusAgents, s.agent) {
			t.Errorf("%s: unknown agent %q", s.path, s.agent)
		}
		if !contains(corpusStates, s.state) {
			t.Errorf("%s: unknown state %q", s.path, s.state)
		}
	}
	for _, a := range requiredAgents {
		if seenAgent[a] == 0 {
			t.Errorf("no captures from %s", a)
		}
	}
	for _, st := range corpusStates {
		if seenState[st] == 0 {
			t.Errorf("no captures labelled %s", st)
		}
	}
}

// Nothing matching a secret pattern may be committed. Run
// scripts/laya-scrub before adding captures.
func TestCorpusHasNoSecrets(t *testing.T) {
	for _, s := range loadCorpus(t) {
		if found := Secrets(s.text); len(found) != 0 {
			t.Errorf("%s: contains %v — run scripts/laya-scrub", s.path, found)
		}
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// TestCorpusAccuracy scores a running Laya server against the corpus.
// Skipped unless HIVE_LAYA_URL names one (HIVE_LAYA_MODEL and
// HIVE_LAYA_API_KEY are passed through). The target — 90% overall, 95%
// recall on the two waits — is a follow-up, not a merge gate (spec 458
// Decision log), so falling short is reported, and fails only with
// HIVE_LAYA_ENFORCE=1.
func TestCorpusAccuracy(t *testing.T) {
	url := os.Getenv("HIVE_LAYA_URL")
	if url == "" {
		t.Skip("HIVE_LAYA_URL not set; no Laya server to score")
	}
	req := Request{BaseURL: url, Model: os.Getenv("HIVE_LAYA_MODEL"), APIKey: os.Getenv("HIVE_LAYA_API_KEY")}
	// The same no-redirect client as production: this sends screen text
	// and any HIVE_LAYA_API_KEY to the URL under test, and nowhere else.
	client := NewClient()
	confusion := map[string]map[string]int{}
	var total, right, waits, waitsRight int
	for _, s := range loadCorpus(t) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		got, err := Classify(ctx, client, req, s.text)
		cancel()
		if err != nil {
			t.Fatalf("%s: %v", s.path, err)
		}
		gotName := stateDir(got)
		if confusion[s.state] == nil {
			confusion[s.state] = map[string]int{}
		}
		confusion[s.state][gotName]++
		total++
		if gotName == s.state {
			right++
		}
		if s.state == "waiting_input" || s.state == "waiting_permission" {
			waits++
			// Either wait raises the same attention; recall is about
			// not missing one.
			if gotName == "waiting_input" || gotName == "waiting_permission" {
				waitsRight++
			}
		}
	}
	if total == 0 {
		t.Fatal("empty corpus")
	}
	for _, want := range corpusStates {
		row := ""
		for _, got := range corpusStates {
			row += fmt.Sprintf(" %3d", confusion[want][got])
		}
		t.Logf("%-19s%s", want, row)
	}
	acc := float64(right) / float64(total)
	recall := 1.0
	if waits > 0 {
		recall = float64(waitsRight) / float64(waits)
	}
	t.Logf("accuracy %.1f%% (%d/%d), waiting recall %.1f%% (%d/%d)", 100*acc, right, total, 100*recall, waitsRight, waits)
	if (acc < 0.90 || recall < 0.95) && os.Getenv("HIVE_LAYA_ENFORCE") == "1" {
		t.Errorf("below target: want >=90%% accuracy and >=95%% waiting recall")
	}
}

// stateDir is the corpus directory name for a wire state.
func stateDir(s string) string {
	if s == wire.StateIdle {
		return "idle"
	}
	return s
}
