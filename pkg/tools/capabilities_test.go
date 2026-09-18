package tools_test

import (
	"encoding/json"
	"strings"
	"testing"

	"amux-accounts/pkg/tools"
	"amux-accounts/pkg/types"
)

func TestModelCapabilities_TemperatureFiltering(t *testing.T) {
	// Standard model (e.g. gpt-4o) should support temperature
	capsStandard := tools.GetModelCapabilities("openai", "gpt-4o")
	if !capsStandard.SupportsTemperature {
		t.Errorf("expected gpt-4o to support temperature")
	}

	// Reasoning models (o1, o3, o3-mini, gpt-5) must reject temperature
	reasoningModels := []string{"o1", "o1-mini", "o3", "o3-mini", "gpt-5", "deepseek-r1"}
	for _, m := range reasoningModels {
		caps := tools.GetModelCapabilities("openai", m)
		if caps.SupportsTemperature {
			t.Errorf("expected %s to NOT support temperature", m)
		}
	}

	// Codex backend must reject temperature
	capsCodex := tools.GetModelCapabilities("codexcli:01", "gpt-5.6-terra")
	if capsCodex.SupportsTemperature {
		t.Errorf("expected codex adapter to NOT support temperature")
	}
}

func TestMarshalOpenAIChatRequest_FiltersTemperatureForReasoning(t *testing.T) {
	// Request with explicit temperature on reasoning model
	req := &types.ChatRequest{
		Model:               "o3-mini",
		Temperature:         0.7,
		ExplicitTemperature: true,
		Messages: []types.ChatMessage{
			{Role: "user", Content: "Hello"},
		},
	}

	data, err := tools.MarshalOpenAIChatRequest(req)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	payloadStr := string(data)
	if strings.Contains(payloadStr, `"temperature"`) {
		t.Fatalf("payload for o3-mini MUST NOT contain temperature, got: %s", payloadStr)
	}

	// Request with explicit temperature on standard model
	reqStandard := &types.ChatRequest{
		Model:               "gpt-4o",
		Temperature:         0.7,
		ExplicitTemperature: true,
		Messages: []types.ChatMessage{
			{Role: "user", Content: "Hello"},
		},
	}

	dataStandard, err := tools.MarshalOpenAIChatRequest(reqStandard)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(dataStandard, &m); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if _, hasTemp := m["temperature"]; !hasTemp {
		t.Fatalf("expected payload for gpt-4o to contain temperature, got: %s", string(dataStandard))
	}
}
