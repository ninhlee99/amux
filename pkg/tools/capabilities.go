package tools

import "strings"

// ModelCapabilities defines what parameters and features a target model or upstream supports.
type ModelCapabilities struct {
	SupportsTemperature     bool
	SupportsTools           bool
	SupportsThinking        bool
	SupportsReasoningEffort bool
}

// GetModelCapabilities inspects the provider and model to determine what capabilities are supported.
func GetModelCapabilities(provider, model string) ModelCapabilities {
	m := strings.ToLower(model)
	p := strings.ToLower(provider)

	// Default baseline for standard conversational models
	caps := ModelCapabilities{
		SupportsTemperature:     true,
		SupportsTools:           true,
		SupportsThinking:        false,
		SupportsReasoningEffort: false,
	}

	// 1. Codex Responses API strictly rejects temperature with HTTP 400 "Unsupported parameter: temperature"
	if strings.Contains(p, "codex") || strings.Contains(m, "codex") {
		caps.SupportsTemperature = false
	}

	// 2. OpenAI Reasoning models (o1, o3, o3-mini, gpt-5, r1, reasoning) reject temperature
	if strings.Contains(m, "o1") || strings.Contains(m, "o3") ||
		strings.Contains(m, "gpt-5") || strings.Contains(m, "reasoning") ||
		strings.Contains(m, "r1") {
		caps.SupportsTemperature = false
		caps.SupportsReasoningEffort = true
		caps.SupportsThinking = true
	}

	return caps
}
