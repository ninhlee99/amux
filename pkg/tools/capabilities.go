package tools

import (
	"regexp"
	"strings"
)

// ModelCapabilities defines what parameters and features a target model or upstream supports.
type ModelCapabilities struct {
	SupportsTemperature     bool
	SupportsTools           bool
	SupportsThinking        bool
	SupportsReasoningEffort bool
}

// CapabilityRule defines a pattern matcher and modifier for model capabilities.
type CapabilityRule struct {
	Name  string
	Match func(provider, model string) bool
	Apply func(*ModelCapabilities)
}

var (
	// reOpenAIReasoning matches exact o-series models: o1, o1-mini, o1-preview, o3, o3-mini, o4, etc.
	// with optional vendor prefix (e.g. "openai/o3-mini").
	reOpenAIReasoning = regexp.MustCompile(`^(?:[a-zA-Z0-9_.-]+/)?o[134](?:-[a-zA-Z0-9_.-]+)?$`)

	// reDeepSeekReasoning matches deepseek-r1, deepseek-reasoner, or standalone r1 models.
	reDeepSeekReasoning = regexp.MustCompile(`^(?:[a-zA-Z0-9_.-]+/)?(?:deepseek-(?:r1(?:-[a-zA-Z0-9_.-]+)?|reasoner)|r1(?:-[a-zA-Z0-9_.-]+)?)$`)

	// reGPT5Reasoning matches gpt-5, gpt-5-mini, gpt-5.6-terra, etc.
	reGPT5Reasoning = regexp.MustCompile(`^(?:[a-zA-Z0-9_.-]+/)?gpt-5(?:\.[0-9]+)?(?:-[a-zA-Z0-9_.-]+)?$`)
)

// DefaultCapabilityRules provides the table-driven registry of known upstream model rules.
var DefaultCapabilityRules = []CapabilityRule{
	{
		Name: "codex-api-boundary",
		Match: func(provider, model string) bool {
			p := strings.ToLower(strings.TrimSpace(provider))
			m := strings.ToLower(strings.TrimSpace(model))
			// Only match actual Codex providers (codex, codex:*, codex-*, codexcli:*)
			if p == "codex" || strings.HasPrefix(p, "codex:") || strings.HasPrefix(p, "codex-") ||
				strings.HasPrefix(p, "codex_") || strings.HasPrefix(p, "codexcli") {
				return true
			}
			// Or designated codex model families
			return m == "gpt-5-codex" || strings.HasPrefix(m, "codex-") || strings.HasPrefix(m, "codex/")
		},
		Apply: func(c *ModelCapabilities) {
			// Codex Responses API strictly rejects temperature with HTTP 400 "Unsupported parameter: temperature"
			c.SupportsTemperature = false
		},
	},
	{
		Name: "openai-reasoning-models",
		Match: func(provider, model string) bool {
			m := strings.ToLower(strings.TrimSpace(model))
			return reOpenAIReasoning.MatchString(m) || reGPT5Reasoning.MatchString(m)
		},
		Apply: func(c *ModelCapabilities) {
			c.SupportsTemperature = false
			c.SupportsReasoningEffort = true
			c.SupportsThinking = true
		},
	},
	{
		Name: "deepseek-reasoning-models",
		Match: func(provider, model string) bool {
			m := strings.ToLower(strings.TrimSpace(model))
			return reDeepSeekReasoning.MatchString(m)
		},
		Apply: func(c *ModelCapabilities) {
			c.SupportsTemperature = false
			c.SupportsReasoningEffort = true
			c.SupportsThinking = true
		},
	},
}

// GetModelCapabilities inspects the provider and model to determine what capabilities are supported.
func GetModelCapabilities(provider, model string) ModelCapabilities {
	// Default baseline for standard conversational models
	caps := ModelCapabilities{
		SupportsTemperature:     true,
		SupportsTools:           true,
		SupportsThinking:        false,
		SupportsReasoningEffort: false,
	}

	for _, rule := range DefaultCapabilityRules {
		if rule.Match(provider, model) {
			rule.Apply(&caps)
		}
	}

	return caps
}

