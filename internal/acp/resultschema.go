// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package acp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Typed results (spec 528). A workflow node's output schema is a small,
// closed subset of JSON Schema — exactly what the workflow IR can say
// (scripts/workflow-proto/schema.ts):
//
//	{"type":"string"[, "enum":["a","b"]]}
//	{"type":"number"}   {"type":"boolean"}
//	{"type":"array","items":<schema>}
//	{"type":"object","properties":{…},"required":[every key],"additionalProperties":false}
//
// The validator is hand-written so the daemon takes no dependency for a
// dozen rules, and so a rejection can name the offending path, which the
// model reads as a tool error and retries on. The shared fixtures in
// testdata/resultschema/ pin the behaviour for any other implementation.

const (
	// MaxResultSchemaBytes bounds a result schema's JSON.
	MaxResultSchemaBytes = 8 << 10
	// MaxResultSchemaDepth bounds schema nesting; the top-level object is
	// depth 1.
	MaxResultSchemaDepth = 8
)

// ErrResultSchema wraps every CheckSchema refusal.
var ErrResultSchema = errors.New("invalid result schema")

// Schema is a checked result schema.
type Schema struct {
	typ        string
	enum       []string
	items      *Schema
	props      map[string]*Schema
	propOrder  []string
	hasEnumSet bool
}

// CheckSchema reports why raw is not an acceptable result schema, or nil.
func CheckSchema(raw json.RawMessage) error {
	_, err := ParseSchema(raw)
	return err
}

// ParseSchema checks raw and returns it ready to validate values. The
// top level must be an object schema: a node's data is a record.
func ParseSchema(raw json.RawMessage) (*Schema, error) {
	if len(raw) > MaxResultSchemaBytes {
		return nil, fmt.Errorf("%w: larger than %d bytes", ErrResultSchema, MaxResultSchemaBytes)
	}
	var v any
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrResultSchema, err)
	}
	if dec.More() {
		return nil, fmt.Errorf("%w: trailing data", ErrResultSchema)
	}
	s, err := parseSchemaNode(v, "schema", 1)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrResultSchema, err)
	}
	if s.typ != "object" {
		return nil, fmt.Errorf("%w: the top level must be an object schema", ErrResultSchema)
	}
	return s, nil
}

func parseSchemaNode(v any, path string, depth int) (*Schema, error) {
	if depth > MaxResultSchemaDepth {
		return nil, fmt.Errorf("%s: nested deeper than %d levels", path, MaxResultSchemaDepth)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: must be an object", path)
	}
	typ, _ := m["type"].(string)
	allowed := map[string][]string{
		"string":  {"type", "enum"},
		"number":  {"type"},
		"boolean": {"type"},
		"array":   {"type", "items"},
		"object":  {"type", "properties", "required", "additionalProperties"},
	}[typ]
	if allowed == nil {
		return nil, fmt.Errorf("%s: type must be string, number, boolean, array or object", path)
	}
	for k := range m {
		if !slices.Contains(allowed, k) {
			return nil, fmt.Errorf("%s: %q is not supported on a %s schema", path, k, typ)
		}
	}
	s := &Schema{typ: typ}
	switch typ {
	case "string":
		if e, has := m["enum"]; has {
			list, ok := e.([]any)
			if !ok || len(list) == 0 {
				return nil, fmt.Errorf("%s.enum: must be a non-empty array of strings", path)
			}
			for _, x := range list {
				str, ok := x.(string)
				if !ok {
					return nil, fmt.Errorf("%s.enum: must be a non-empty array of strings", path)
				}
				s.enum = append(s.enum, str)
			}
			s.hasEnumSet = true
		}
	case "array":
		it, has := m["items"]
		if !has {
			return nil, fmt.Errorf("%s: an array schema needs items", path)
		}
		sub, err := parseSchemaNode(it, path+".items", depth+1)
		if err != nil {
			return nil, err
		}
		s.items = sub
	case "object":
		if ap, has := m["additionalProperties"]; !has || ap != false {
			return nil, fmt.Errorf(`%s: additionalProperties must be false`, path)
		}
		props, ok := m["properties"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s: an object schema needs properties", path)
		}
		reqList, ok := m["required"].([]any)
		if !ok {
			return nil, fmt.Errorf("%s: an object schema needs required", path)
		}
		req := map[string]bool{}
		for _, r := range reqList {
			name, ok := r.(string)
			if !ok {
				return nil, fmt.Errorf("%s.required: must list property names", path)
			}
			req[name] = true
		}
		s.props = make(map[string]*Schema, len(props))
		s.propOrder = []string{} // never nil: JSON() must emit "required": []
		for name := range props {
			if !req[name] {
				return nil, fmt.Errorf("%s.required: must list every property (%q is missing)", path, name)
			}
			s.propOrder = append(s.propOrder, name)
		}
		sort.Strings(s.propOrder)
		for name := range req {
			if _, ok := props[name]; !ok {
				return nil, fmt.Errorf("%s.required: %q is not a property", path, name)
			}
		}
		for _, name := range s.propOrder {
			sub, err := parseSchemaNode(props[name], path+".properties."+name, depth+1)
			if err != nil {
				return nil, err
			}
			s.props[name] = sub
		}
	}
	return s, nil
}

// CanonicalEnvelope validates a submit_result {status, summary, data}
// envelope and returns the canonical form to store and broadcast. s is the
// schema of data, or nil for a session without one.
//
// Keys are matched exactly, never case-insensitively (as encoding/json's
// struct decoding does), so {"status":"ok","Status":"error"} cannot be read
// one way here and another by a consumer: any key other than status,
// summary and data is refused. status must be exactly "ok" or "error";
// summary, when present, a string. With status "error" (a task that could
// not be done reports no data) any data is dropped; with "ok", data is
// required when s is set and must match it.
func CanonicalEnvelope(s *Schema, envelope json.RawMessage) (json.RawMessage, error) {
	var env map[string]json.RawMessage
	if err := json.Unmarshal(envelope, &env); err != nil || env == nil {
		return nil, errors.New("the result must be a JSON object")
	}
	for k := range env {
		if k != "status" && k != "summary" && k != "data" {
			return nil, fmt.Errorf("%s: unexpected field", strconv.Quote(k))
		}
	}
	var status string
	if json.Unmarshal(env["status"], &status) != nil || (status != "ok" && status != "error") {
		return nil, errors.New(`"status" must be exactly "ok" or "error"`)
	}
	// Built key by key, in the order status, summary, data, so the stored
	// form is stable and compact.
	var out bytes.Buffer
	out.WriteString(`{"status":` + strconv.Quote(status))
	if raw, ok := env["summary"]; ok {
		var sum string
		if json.Unmarshal(raw, &sum) != nil {
			return nil, errors.New(`"summary" must be a string`)
		}
		b, _ := json.Marshal(sum)
		out.WriteString(`,"summary":`)
		out.Write(b)
	}
	if status == "ok" {
		data, has := env["data"]
		if s != nil {
			if !has {
				return nil, errors.New("data: required when status is ok")
			}
			if err := s.CheckValue("data", data); err != nil {
				return nil, err
			}
		}
		if has {
			out.WriteString(`,"data":`)
			if err := json.Compact(&out, data); err != nil {
				return nil, err
			}
		}
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}

// JSON returns the schema as a decoded JSON value, for embedding in a
// tool definition.
func (s *Schema) JSON() any {
	switch s.typ {
	case "string":
		m := map[string]any{"type": "string"}
		if s.hasEnumSet {
			m["enum"] = s.enum
		}
		return m
	case "array":
		return map[string]any{"type": "array", "items": s.items.JSON()}
	case "object":
		props := make(map[string]any, len(s.props))
		for k, v := range s.props {
			props[k] = v.JSON()
		}
		return map[string]any{"type": "object", "properties": props, "required": s.propOrder, "additionalProperties": false}
	}
	return map[string]any{"type": s.typ}
}

// CheckValue validates the JSON value in raw against s. A rejection
// names the failing path from root, e.g.
//
//	data.findings[2].severity: "urgent" is not one of high|low
func (s *Schema) CheckValue(root string, raw json.RawMessage) error {
	var v any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return fmt.Errorf("%s: not valid JSON: %v", root, err)
	}
	return s.check(v, root)
}

func jsonKind(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case json.Number, float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "unknown"
}

func (s *Schema) check(v any, path string) error {
	if got := jsonKind(v); got != s.typ {
		return fmt.Errorf("%s: expected %s, got %s", path, s.typ, got)
	}
	switch s.typ {
	case "number":
		// UseNumber keeps the literal; 1e400 is valid JSON but not a number
		// a JS consumer can hold (it becomes Infinity).
		if f, err := strconv.ParseFloat(v.(json.Number).String(), 64); err != nil || math.IsInf(f, 0) {
			return fmt.Errorf("%s: %s is out of range", path, v)
		}
	case "string":
		if s.hasEnumSet && !slices.Contains(s.enum, v.(string)) {
			return fmt.Errorf("%s: %s is not one of %s", path, strconv.Quote(v.(string)), strings.Join(s.enum, "|"))
		}
	case "array":
		for i, x := range v.([]any) {
			if err := s.items.check(x, path+"["+strconv.Itoa(i)+"]"); err != nil {
				return err
			}
		}
	case "object":
		m := v.(map[string]any)
		for _, name := range s.propOrder {
			x, ok := m[name]
			if !ok {
				return fmt.Errorf("%s.%s: required field is missing", path, name)
			}
			if err := s.props[name].check(x, path+"."+name); err != nil {
				return err
			}
		}
		var extra []string
		for k := range m {
			if _, ok := s.props[k]; !ok {
				extra = append(extra, k)
			}
		}
		if len(extra) > 0 {
			sort.Strings(extra)
			return fmt.Errorf("%s.%s: unexpected field", path, extra[0])
		}
	}
	return nil
}
