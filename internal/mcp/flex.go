package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// addFlexTool is a drop-in replacement for mcp.AddTool that pre-infers the
// input schema and loosens its primitive types so LLMs can pass numbers as
// strings (and vice versa) without being rejected at the validation layer.
// The handler still unmarshals into In; use FlexInt/FlexBool in In's fields
// to actually coerce the resulting values.
func addFlexTool[In, Out any](s *mcp.Server, t *mcp.Tool, h mcp.ToolHandlerFor[In, Out]) {
	if t.InputSchema == nil {
		var zero In
		rt := reflect.TypeOf(zero)
		if rt != nil {
			if schema, err := jsonschema.ForType(rt, &jsonschema.ForOptions{}); err == nil {
				loosenPrimitiveTypes(schema)
				t.InputSchema = schema
			}
		}
	}
	mcp.AddTool(s, t, h)
}

// FlexInt is an integer that also accepts JSON strings containing an integer.
// LLM clients sometimes send numeric MCP arguments as quoted strings; FlexInt
// coerces these back to int rather than failing the whole call.
type FlexInt int

// UnmarshalJSON accepts either a JSON number or a JSON string holding an
// integer. Trims whitespace; empty strings are treated as absent.
func (f *FlexInt) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	// Number form.
	if b[0] != '"' {
		var n int
		if err := json.Unmarshal(b, &n); err != nil {
			return fmt.Errorf("FlexInt: %w", err)
		}
		*f = FlexInt(n)
		return nil
	}
	// String form.
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("FlexInt: %w", err)
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	i, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("FlexInt: cannot parse %q as integer", s)
	}
	*f = FlexInt(i)
	return nil
}

// FlexBool is a bool that also accepts JSON strings like "true"/"false"/"1"/"0".
type FlexBool bool

func (f *FlexBool) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	if b[0] != '"' {
		var v bool
		if err := json.Unmarshal(b, &v); err != nil {
			return fmt.Errorf("FlexBool: %w", err)
		}
		*f = FlexBool(v)
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("FlexBool: %w", err)
	}
	v, err := strconv.ParseBool(strings.TrimSpace(s))
	if err != nil {
		return fmt.Errorf("FlexBool: cannot parse %q as boolean", s)
	}
	*f = FlexBool(v)
	return nil
}

// loosenPrimitiveTypes walks a jsonschema.Schema and replaces every primitive
// type (integer, number, boolean) with a multi-type union that also accepts
// strings. This pairs with FlexInt / FlexBool, whose UnmarshalJSON coerces
// string inputs back to the underlying primitive.
//
// Applied to inferred tool input schemas so LLMs sending "2" instead of 2
// aren't rejected at the validation layer.
func loosenPrimitiveTypes(s *jsonschema.Schema) {
	if s == nil {
		return
	}
	loosen := func(t string) []string {
		switch t {
		case "integer":
			return []string{"integer", "string"}
		case "number":
			return []string{"number", "string"}
		case "boolean":
			return []string{"boolean", "string"}
		}
		return nil
	}
	if alt := loosen(s.Type); alt != nil {
		s.Type = ""
		s.Types = alt
	} else if len(s.Types) > 0 {
		// Already multi-type; append "string" if any primitive is present.
		wantString := false
		hasString := false
		for _, t := range s.Types {
			if t == "string" {
				hasString = true
			}
			if t == "integer" || t == "number" || t == "boolean" {
				wantString = true
			}
		}
		if wantString && !hasString {
			s.Types = append(s.Types, "string")
		}
	}
	for _, prop := range s.Properties {
		loosenPrimitiveTypes(prop)
	}
	if s.Items != nil {
		loosenPrimitiveTypes(s.Items)
	}
	for _, sub := range s.PrefixItems {
		loosenPrimitiveTypes(sub)
	}
	if s.AdditionalProperties != nil {
		loosenPrimitiveTypes(s.AdditionalProperties)
	}
	for _, sub := range s.AllOf {
		loosenPrimitiveTypes(sub)
	}
	for _, sub := range s.AnyOf {
		loosenPrimitiveTypes(sub)
	}
	for _, sub := range s.OneOf {
		loosenPrimitiveTypes(sub)
	}
	if s.Not != nil {
		loosenPrimitiveTypes(s.Not)
	}
}
