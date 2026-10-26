// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package acp

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckSchema_Subset(t *testing.T) {
	obj := func(props, req string) string {
		return `{"type":"object","properties":` + props + `,"required":` + req + `,"additionalProperties":false}`
	}
	nest := func(depth int) string { // an object nested `depth` levels deep
		s := `{"type":"string"}`
		for i := 1; i < depth; i++ {
			s = obj(`{"a":`+s+`}`, `["a"]`)
		}
		return s
	}
	good := map[string]string{
		"empty object": obj(`{}`, `[]`),
		"all types": obj(`{"s":{"type":"string"},"e":{"type":"string","enum":["a"]},"n":{"type":"number"},`+
			`"b":{"type":"boolean"},"l":{"type":"array","items":{"type":"number"}}}`, `["s","e","n","b","l"]`),
		"depth 8": nest(8),
	}
	for name, s := range good {
		if err := CheckSchema(json.RawMessage(s)); err != nil {
			t.Errorf("%s: unexpected refusal: %v", name, err)
		}
	}
	bad := map[string]string{
		"$ref":                        obj(`{"a":{"$ref":"#/x"}}`, `["a"]`),
		"oneOf":                       obj(`{"a":{"oneOf":[{"type":"string"}]}}`, `["a"]`),
		"partial required":            obj(`{"a":{"type":"string"},"b":{"type":"string"}}`, `["a"]`),
		"required not a property":     obj(`{"a":{"type":"string"}}`, `["a","z"]`),
		"additionalProperties true":   `{"type":"object","properties":{},"required":[],"additionalProperties":true}`,
		"additionalProperties absent": `{"type":"object","properties":{},"required":[]}`,
		"top level not object":        `{"type":"string"}`,
		"top level array":             `{"type":"array","items":{"type":"string"}}`,
		"integer":                     obj(`{"a":{"type":"integer"}}`, `["a"]`),
		"unknown keyword":             obj(`{"a":{"type":"string","minLength":1}}`, `["a"]`),
		"enum on number":              obj(`{"a":{"type":"number","enum":["1"]}}`, `["a"]`),
		"empty enum":                  obj(`{"a":{"type":"string","enum":[]}}`, `["a"]`),
		"array without items":         obj(`{"a":{"type":"array"}}`, `["a"]`),
		"depth 9":                     nest(9),
		"too big":                     obj(`{"a":{"type":"string","enum":["`+strings.Repeat("x", MaxResultSchemaBytes)+`"]}}`, `["a"]`),
		"not json":                    `{`,
		"trailing":                    obj(`{}`, `[]`) + ` {}`,
		"not an object":               `[]`,
	}
	for name, s := range bad {
		err := CheckSchema(json.RawMessage(s))
		if err == nil {
			t.Errorf("%s: accepted", name)
		} else if !errors.Is(err, ErrResultSchema) {
			t.Errorf("%s: %v does not wrap ErrResultSchema", name, err)
		}
	}
}

// fixture is one shared case; internal/acp/testdata/resultschema is
// also the contract any other implementation of this validator (the
// workflow engine's) is tested against.
type fixture struct {
	Schema json.RawMessage `json:"schema"`
	Value  json.RawMessage `json:"value"`
	OK     bool            `json:"ok"`
	Error  string          `json:"error"`
}

func TestCheckResult_Fixtures(t *testing.T) {
	files, _ := filepath.Glob("testdata/resultschema/*.json")
	if len(files) == 0 {
		t.Fatal("no fixtures")
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var fx fixture
		if err := json.Unmarshal(b, &fx); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		s, err := ParseSchema(fx.Schema)
		if err != nil {
			t.Errorf("%s: schema refused: %v", f, err)
			continue
		}
		err = s.CheckValue("data", fx.Value)
		switch {
		case fx.OK && err != nil:
			t.Errorf("%s: want ok, got %v", f, err)
		case !fx.OK && err == nil:
			t.Errorf("%s: want error %q, got ok", f, fx.Error)
		case !fx.OK && err.Error() != fx.Error:
			t.Errorf("%s: error = %q, want %q", f, err, fx.Error)
		}
	}
}

func TestCheckResult_ErrorPath(t *testing.T) {
	s, err := ParseSchema(json.RawMessage(`{"type":"object","properties":{"a":{"type":"array","items":{"type":"array","items":{"type":"boolean"}}}},"required":["a"],"additionalProperties":false}`))
	if err != nil {
		t.Fatal(err)
	}
	err = s.CheckValue("data", json.RawMessage(`{"a":[[true],[false,"x"]]}`))
	if err == nil || err.Error() != "data.a[1][1]: expected boolean, got string" {
		t.Fatalf("err = %v", err)
	}
	if err := s.CheckValue("data", json.RawMessage(`{`)); err == nil {
		t.Fatal("invalid JSON accepted")
	}
}
