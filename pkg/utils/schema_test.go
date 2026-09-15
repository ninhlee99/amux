package utils

import (
	"encoding/json"
	"testing"
)

func TestNormalizeJSONSchema(t *testing.T) {
	got := NormalizeJSONSchema(nil)
	if string(got) != `{"type":"object","properties":{}}` {
		t.Fatalf("nil → %s", got)
	}
	in := json.RawMessage(`{"type":"object"}`)
	if string(NormalizeJSONSchema(in)) != `{"properties":{},"type":"object"}` {
		t.Fatalf("properties addition failed: %s", NormalizeJSONSchema(in))
	}

	geminiSchema := json.RawMessage(`{"type":"OBJECT","properties":{"cmd":{"type":"STRING"},"count":{"type":"INTEGER"}}}`)
	normalized := NormalizeJSONSchema(geminiSchema)
	var m map[string]any
	if err := json.Unmarshal(normalized, &m); err != nil {
		t.Fatalf("unmarshal normalized failed: %v", err)
	}
	if m["type"] != "object" {
		t.Errorf("expected object, got %v", m["type"])
	}
	props := m["properties"].(map[string]any)
	if props["cmd"].(map[string]any)["type"] != "string" {
		t.Errorf("expected string, got %v", props["cmd"])
	}
	if props["count"].(map[string]any)["type"] != "integer" {
		t.Errorf("expected integer, got %v", props["count"])
	}
}

