package tools

import (
	"encoding/json"
	"testing"

	"amux-accounts/pkg/types"
)

func TestClaudeModelGeneration(t *testing.T) {
	cases := []struct {
		model                      string
		adaptive, sampling, forced bool
	}{
		{"claude-opus-5-5", true, false, false},
		{"claude-sonnet-5-5", true, false, false},
		{"claude-fable-5-1", true, false, false},
		{"claude-opus-5", true, false, true},
		{"claude-opus-4-8", true, false, true},
		{"claude-sonnet-4-6", true, true, true},
		{"claude-haiku-4-5-20251001", false, true, true},
		{"anthropic.claude-opus-5-5", true, false, false},
		{"claude-3-7-sonnet-20250219", false, true, true},
		{"gpt-5", false, true, true},
	}
	for _, c := range cases {
		g := ClaudeModelGeneration(c.model)
		if g.AdaptiveThinking() != c.adaptive || g.AcceptsSampling() != c.sampling || g.AcceptsForcedToolChoice() != c.forced {
			t.Errorf("%s: adaptive=%v sampling=%v forced=%v (gen %+v)", c.model,
				g.AdaptiveThinking(), g.AcceptsSampling(), g.AcceptsForcedToolChoice(), g)
		}
	}
}

func TestMarshalClaude_ModernModelDropsRejectedParams(t *testing.T) {
	req := &types.ChatRequest{
		Messages:       []types.ChatMessage{{Role: "user", Content: "hi"}},
		Temperature:    0.7,
		Thinking:       true,
		ThinkingBudget: 4096,
		Tools:          []types.ToolDef{{Name: "Bash", InputSchema: json.RawMessage(`{"type":"object"}`)}},
		ToolChoice:     "required",
	}
	b, err := MarshalClaudeMessagesRequest(req, "claude-opus-5-5")
	if err != nil {
		t.Fatal(err)
	}
	var p map[string]any
	_ = json.Unmarshal(b, &p)
	if _, ok := p["temperature"]; ok {
		t.Error("temperature must be dropped for claude-opus-5-5")
	}
	if th := p["thinking"].(map[string]any); th["type"] != "adaptive" || th["budget_tokens"] != nil {
		t.Errorf("thinking = %v", th)
	}
	if tc := p["tool_choice"].(map[string]any); tc["type"] != "auto" {
		t.Errorf("tool_choice = %v", tc)
	}

	b, _ = MarshalClaudeMessagesRequest(req, "claude-haiku-4-5")
	_ = json.Unmarshal(b, &p)
	if p["temperature"] != 0.7 || p["thinking"].(map[string]any)["budget_tokens"] != float64(4096) || p["tool_choice"].(map[string]any)["type"] != "any" {
		t.Errorf("haiku 4.5 payload changed unexpectedly: %v", p)
	}
}

func TestMarshalClaude_DefaultModelIsCurrent(t *testing.T) {
	b, _ := MarshalClaudeMessagesRequest(&types.ChatRequest{Messages: []types.ChatMessage{{Role: "user", Content: "x"}}}, "")
	var p map[string]any
	_ = json.Unmarshal(b, &p)
	if p["model"] != DefaultClaudeModel {
		t.Fatalf("model = %v", p["model"])
	}
}
