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

func TestModelCapabilities_NegativeAndNearMatchingCases(t *testing.T) {
	tests := []struct {
		provider                string
		model                   string
		expectTemperature       bool
		expectReasoningEffort   bool
		expectThinking          bool
		description             string
	}{
		// 1. Models containing "reasoning" as a substring but not reasoning models
		{
			provider:              "openai",
			model:                 "custom-reasoning-evaluator-v1",
			expectTemperature:     true,
			expectReasoningEffort: false,
			expectThinking:        false,
			description:           "model containing 'reasoning' substring must keep baseline capabilities",
		},
		{
			provider:              "openai",
			model:                 "my_reasoning_app",
			expectTemperature:     true,
			expectReasoningEffort: false,
			expectThinking:        false,
			description:           "model containing '_reasoning_' must keep baseline capabilities",
		},

		// 2. Models containing "r1" as a substring but not R1 models
		{
			provider:              "openai",
			model:                 "cluster1-node",
			expectTemperature:     true,
			expectReasoningEffort: false,
			expectThinking:        false,
			description:           "model with 'r1' substring in other words must not trigger reasoning",
		},
		{
			provider:              "openai",
			model:                 "my-model-r123",
			expectTemperature:     true,
			expectReasoningEffort: false,
			expectThinking:        false,
			description:           "model named 'r123' must not trigger R1 reasoning",
		},
		{
			provider:              "openai",
			model:                 "server-r10",
			expectTemperature:     true,
			expectReasoningEffort: false,
			expectThinking:        false,
			description:           "model named 'server-r10' must not trigger R1 reasoning",
		},

		// 3. Custom providers containing "codex" but not actual Codex adapter
		{
			provider:              "company-codex-analytics",
			model:                 "gpt-4o",
			expectTemperature:     true,
			expectReasoningEffort: false,
			expectThinking:        false,
			description:           "custom provider with 'codex' substring must not disable temperature",
		},
		{
			provider:              "my-custom-codex-proxy",
			model:                 "claude-3-5-sonnet",
			expectTemperature:     true,
			expectReasoningEffort: false,
			expectThinking:        false,
			description:           "custom proxy containing 'codex' must not disable temperature",
		},

		// 4. Positive reasoning models
		{
			provider:              "openai",
			model:                 "o1",
			expectTemperature:     false,
			expectReasoningEffort: true,
			expectThinking:        true,
			description:           "exact o1 model",
		},
		{
			provider:              "openai",
			model:                 "o3-mini",
			expectTemperature:     false,
			expectReasoningEffort: true,
			expectThinking:        true,
			description:           "o3-mini model",
		},
		{
			provider:              "openai",
			model:                 "deepseek/deepseek-r1",
			expectTemperature:     false,
			expectReasoningEffort: true,
			expectThinking:        true,
			description:           "prefixed deepseek-r1 model",
		},
		{
			provider:              "openai",
			model:                 "deepseek-reasoner",
			expectTemperature:     false,
			expectReasoningEffort: true,
			expectThinking:        true,
			description:           "deepseek-reasoner model",
		},
		{
			provider:              "openai",
			model:                 "gpt-5.6-terra",
			expectTemperature:     false,
			expectReasoningEffort: true,
			expectThinking:        true,
			description:           "gpt-5.6-terra reasoning model",
		},

		// 5. Positive Codex providers
		{
			provider:              "codex",
			model:                 "gpt-4o",
			expectTemperature:     false,
			expectReasoningEffort: false,
			expectThinking:        false,
			description:           "exact codex provider must reject temperature",
		},
		{
			provider:              "codex:01",
			model:                 "gpt-4o",
			expectTemperature:     false,
			expectReasoningEffort: false,
			expectThinking:        false,
			description:           "sub-account codex:01 must reject temperature",
		},
		{
			provider:              "codexcli:01",
			model:                 "gpt-4o",
			expectTemperature:     false,
			expectReasoningEffort: false,
			expectThinking:        false,
			description:           "codexcli:01 must reject temperature",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			caps := tools.GetModelCapabilities(tc.provider, tc.model)
			if caps.SupportsTemperature != tc.expectTemperature {
				t.Errorf("%s: expected SupportsTemperature=%v, got %v",
					tc.description, tc.expectTemperature, caps.SupportsTemperature)
			}
			if caps.SupportsReasoningEffort != tc.expectReasoningEffort {
				t.Errorf("%s: expected SupportsReasoningEffort=%v, got %v",
					tc.description, tc.expectReasoningEffort, caps.SupportsReasoningEffort)
			}
			if caps.SupportsThinking != tc.expectThinking {
				t.Errorf("%s: expected SupportsThinking=%v, got %v",
					tc.description, tc.expectThinking, caps.SupportsThinking)
			}
		})
	}
}

