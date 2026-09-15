// Package utils holds small shared helpers used across packages
// (JSON Schema normalization, etc.).
package utils

import (
	"encoding/json"
	"strings"
)

// NormalizeJSONSchema returns a usable JSON Schema object. Empty or null
// input becomes {"type":"object","properties":{}}.
// Normalizes uppercase types (e.g. Gemini OBJECT, STRING, ARRAY, INTEGER, NUMBER, BOOLEAN)
// to standard JSON Schema lowercase types so Claude and OpenAI/Codex APIs accept them.
func NormalizeJSONSchema(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return raw
	}
	normalizeSchemaNode(m)
	if typ, ok := m["type"].(string); ok && typ == "object" {
		if _, hasProps := m["properties"]; !hasProps {
			m["properties"] = map[string]any{}
		}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return json.RawMessage(b)
}

func normalizeSchemaNode(v any) {
	switch node := v.(type) {
	case map[string]any:
		if typ, ok := node["type"].(string); ok {
			node["type"] = strings.ToLower(typ)
		}
		for _, val := range node {
			normalizeSchemaNode(val)
		}
	case []any:
		for _, item := range node {
			normalizeSchemaNode(item)
		}
	}
}
