// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package acp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// envFixture is one shared submit_result envelope case; the files in
// testdata/resultschema/envelope are also the contract any other
// implementation of the envelope check (the workflow engine's) is tested
// against. Canonical is what a passing envelope is stored as.
type envFixture struct {
	Schema    json.RawMessage `json:"schema"` // absent: a session with no schema
	Envelope  json.RawMessage `json:"envelope"`
	OK        bool            `json:"ok"`
	Error     string          `json:"error"`
	Canonical string          `json:"canonical"`
}

func TestCanonicalEnvelope_Fixtures(t *testing.T) {
	files, _ := filepath.Glob("testdata/resultschema/envelope/*.json")
	if len(files) == 0 {
		t.Fatal("no fixtures")
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var fx envFixture
		if err := json.Unmarshal(b, &fx); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		var s *Schema
		if len(fx.Schema) > 0 {
			if s, err = ParseSchema(fx.Schema); err != nil {
				t.Errorf("%s: schema refused: %v", f, err)
				continue
			}
		}
		got, err := CanonicalEnvelope(s, fx.Envelope)
		switch {
		case fx.OK && err != nil:
			t.Errorf("%s: want ok, got %v", f, err)
		case fx.OK && fx.Canonical != "" && string(got) != fx.Canonical:
			t.Errorf("%s: canonical = %s, want %s", f, got, fx.Canonical)
		case !fx.OK && err == nil:
			t.Errorf("%s: want error %q, got ok %s", f, fx.Error, got)
		case !fx.OK && err.Error() != fx.Error:
			t.Errorf("%s: error = %q, want %q", f, err, fx.Error)
		}
	}
}
