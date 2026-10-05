package tools

import (
	"regexp"
	"strconv"
)

// DefaultClaudeModel is used when neither the client nor the account names a
// model. Bare ids only — current Claude model ids carry no date suffix.
const DefaultClaudeModel = "claude-opus-5-5"

// DefaultClaudeSonnetModel is the current Sonnet: the gateway's cost-safe
// default for Claude Code traffic (same price as the previous Sonnet 5).
const DefaultClaudeSonnetModel = "claude-sonnet-5-5"

// CurrentClaudeModels is advertised on /v1/models (newest first).
var CurrentClaudeModels = []struct{ ID, Name string }{
	{"claude-opus-5-5", "Claude Opus 5.5"},
	{"claude-sonnet-5-5", "Claude Sonnet 5.5"},
	{"claude-fable-5-1", "Claude Fable 5.1"},
	{"claude-opus-5", "Claude Opus 5"},
	{"claude-sonnet-5", "Claude Sonnet 5"},
	{"claude-opus-4-8", "Claude Opus 4.8"},
	{"claude-haiku-4-5", "Claude Haiku 4.5"},
}

var reClaudeFamily = regexp.MustCompile(`^(?:[a-z0-9_.-]+/)?(?:anthropic\.)?claude-(opus|sonnet|haiku|fable|mythos)-(\d+)(?:-(\d{1,2}))?(?:$|[-@:])`)

// ClaudeGeneration describes which request features a Claude model accepts.
type ClaudeGeneration struct {
	Family       string
	Major, Minor int
	Known        bool // false for legacy "claude-3-7-sonnet-…" style or non-Claude ids
}

// ClaudeModelGeneration parses ids like "claude-opus-4-8",
// "claude-haiku-4-5-20251001" or "anthropic.claude-sonnet-5-5".
func ClaudeModelGeneration(model string) ClaudeGeneration {
	m := reClaudeFamily.FindStringSubmatch(model)
	if m == nil {
		return ClaudeGeneration{}
	}
	g := ClaudeGeneration{Family: m[1], Known: true}
	g.Major, _ = strconv.Atoi(m[2])
	if m[3] != "" {
		g.Minor, _ = strconv.Atoi(m[3])
	}
	return g
}

func (g ClaudeGeneration) atLeast(major, minor int) bool {
	return g.Known && (g.Major > major || (g.Major == major && g.Minor >= minor))
}

// AdaptiveThinking: 4.6+ (except Haiku) use {type:"adaptive"}; budget_tokens
// is rejected from 4.7 on and deprecated on 4.6.
func (g ClaudeGeneration) AdaptiveThinking() bool {
	return g.Family != "haiku" && g.atLeast(4, 6)
}

// AcceptsSampling: temperature/top_p/top_k were removed from 4.7 on (and on
// every 5.x model); 4.6 and older still take them.
func (g ClaudeGeneration) AcceptsSampling() bool {
	return !(g.Family != "haiku" && g.atLeast(4, 7))
}

// AcceptsForcedToolChoice: tool_choice any/tool is a 400 on Fable/Mythos 5.1+,
// Opus 5.5+ and Sonnet 5.5+.
func (g ClaudeGeneration) AcceptsForcedToolChoice() bool {
	switch g.Family {
	case "fable", "mythos":
		return !g.atLeast(5, 1)
	case "opus", "sonnet":
		return !g.atLeast(5, 5)
	}
	return true
}
