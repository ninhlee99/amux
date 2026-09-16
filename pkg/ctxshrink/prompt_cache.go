package ctxshrink

import (
	"encoding/json"
	"strings"
)

const (
	// MaxAnthropicCacheBreakpoints is the strict limit imposed by the Anthropic API.
	MaxAnthropicCacheBreakpoints = 4

	// MinSystemPromptRunesToCache skips ephemeral caching on trivial system strings.
	MinSystemPromptRunesToCache = 100
)

// countBreakpoints recursively scans a JSON structure and counts existing "cache_control" declarations.
func countBreakpoints(v any) int {
	if v == nil {
		return 0
	}
	count := 0
	switch val := v.(type) {
	case map[string]any:
		if cc, ok := val["cache_control"]; ok && cc != nil {
			count++
		}
		for _, child := range val {
			count += countBreakpoints(child)
		}
	case []any:
		for _, elem := range val {
			count += countBreakpoints(elem)
		}
	}
	return count
}

// OptimizeAnthropicPromptCaching inspects an Anthropic /v1/messages JSON request body and
// strategically injects cache_control breakpoints (up to Anthropic's hard limit of 4):
//  1. The system prompt block (caches base system instructions across all turns).
//  2. The last tool in tools[] (caches all tool definitions).
//  3. The penultimate conversation turn in messages[] (caches full historical context prefix).
//
// This achieves an ~85-90% input token discount on subsequent turns while staying strictly
// within Anthropic's <= 4 cache breakpoint rules.
func OptimizeAnthropicPromptCaching(body []byte) ([]byte, bool) {
	if len(body) == 0 {
		return body, false
	}

	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return body, false
	}

	// Must be an Anthropic Messages request (has "messages")
	rawMsgs, ok := root["messages"]
	if !ok {
		return body, false
	}

	breakpoints := countBreakpoints(root)
	if breakpoints >= MaxAnthropicCacheBreakpoints {
		// Already at or over the limit, do not inject more
		return body, false
	}

	modified := false

	// 1. Optimize system prompt block
	if breakpoints < MaxAnthropicCacheBreakpoints {
		if sys, hasSys := root["system"]; hasSys && sys != nil {
			switch s := sys.(type) {
			case string:
				if len([]rune(strings.TrimSpace(s))) >= MinSystemPromptRunesToCache {
					root["system"] = []any{
						map[string]any{
							"type": "text",
							"text": s,
							"cache_control": map[string]any{
								"type": "ephemeral",
							},
						},
					}
					breakpoints++
					modified = true
				}
			case []any:
				if len(s) > 0 {
					// Check if last block already has cache_control
					lastIdx := len(s) - 1
					if block, isMap := s[lastIdx].(map[string]any); isMap {
						if _, hasCC := block["cache_control"]; !hasCC {
							block["cache_control"] = map[string]any{"type": "ephemeral"}
							breakpoints++
							modified = true
						}
					}
				}
			}
		}
	}

	// 2. Optimize tools block (caching the last tool caches all preceding tools)
	if breakpoints < MaxAnthropicCacheBreakpoints {
		if rawTools, hasTools := root["tools"]; hasTools && rawTools != nil {
			if toolsList, isList := rawTools.([]any); isList && len(toolsList) > 0 {
				lastIdx := len(toolsList) - 1
				if lastTool, isMap := toolsList[lastIdx].(map[string]any); isMap {
					if _, hasCC := lastTool["cache_control"]; !hasCC {
						lastTool["cache_control"] = map[string]any{"type": "ephemeral"}
						breakpoints++
						modified = true
					}
				}
			}
		}
	}

	// 3. Optimize messages history (penultimate turn messages[len-2] caches prefix)
	if breakpoints < MaxAnthropicCacheBreakpoints {
		if msgsList, isList := rawMsgs.([]any); isList && len(msgsList) >= 2 {
			penultIdx := len(msgsList) - 2
			if turn, isMap := msgsList[penultIdx].(map[string]any); isMap {
				contentVal, hasContent := turn["content"]
				if hasContent && contentVal != nil {
					switch c := contentVal.(type) {
					case string:
						turn["content"] = []any{
							map[string]any{
								"type": "text",
								"text": c,
								"cache_control": map[string]any{
									"type": "ephemeral",
								},
							},
						}
						breakpoints++
						modified = true
					case []any:
						if len(c) > 0 {
							lastContentIdx := len(c) - 1
							if contentBlock, isMap := c[lastContentIdx].(map[string]any); isMap {
								if _, hasCC := contentBlock["cache_control"]; !hasCC {
									contentBlock["cache_control"] = map[string]any{"type": "ephemeral"}
									breakpoints++
									modified = true
								}
							}
						}
					}
				}
			}
		}
	}

	if !modified {
		return body, false
	}

	newBody, err := json.Marshal(root)
	if err != nil {
		return body, false
	}
	return newBody, true
}
