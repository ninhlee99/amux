package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"amux-accounts/pkg/types"
	"amux-accounts/pkg/utils"
)

// ClaudeTool is the tools[] entry Claude Code sends on /v1/messages.
type ClaudeTool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"input_schema"`
	CacheControl any             `json:"cache_control,omitempty"`
}

// ClaudeToolUseBlock is a content block type=tool_use.
type ClaudeToolUseBlock struct {
	Type  string          `json:"type"` // tool_use
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// ParseClaudeTools extracts tool defs from a Claude Code / Anthropic body.
func ParseClaudeTools(body []byte) ([]types.ToolDef, error) {
	var wrap struct {
		Tools []ClaudeTool `json:"tools"`
	}
	if err := json.Unmarshal(body, &wrap); err != nil {
		return nil, err
	}
	out := make([]types.ToolDef, 0, len(wrap.Tools))
	for _, t := range wrap.Tools {
		if strings.TrimSpace(t.Name) == "" {
			continue
		}
		hasCache := t.CacheControl != nil
		out = append(out, types.ToolDef{
			Name:         t.Name,
			Description:  t.Description,
			InputSchema:  utils.NormalizeJSONSchema(t.InputSchema),
			CacheControl: hasCache,
		})
	}
	return out, nil
}

// ToClaudeTools converts canonical defs to Claude Code tools[].
// If prompt caching is desired, Anthropic expects cache_control: {"type": "ephemeral"}
// on the last tool declaration.
func ToClaudeTools(defs []types.ToolDef) []ClaudeTool {
	out := make([]ClaudeTool, 0, len(defs))
	for i, d := range defs {
		ct := ClaudeTool{
			Name:        d.Name,
			Description: d.Description,
			InputSchema: utils.NormalizeJSONSchema(d.InputSchema),
		}
		if d.CacheControl || i == len(defs)-1 {
			ct.CacheControl = map[string]string{"type": "ephemeral"}
		}
		out = append(out, ct)
	}
	return out
}

// ToClaudeToolUseBlocks converts calls to Anthropic content blocks for SSE/JSON.
func ToClaudeToolUseBlocks(calls []types.ToolCall) []ClaudeToolUseBlock {
	out := make([]ClaudeToolUseBlock, 0, len(calls))
	for _, c := range calls {
		input := json.RawMessage(`{}`)
		if strings.TrimSpace(c.Arguments) != "" && json.Valid([]byte(c.Arguments)) {
			input = json.RawMessage(c.Arguments)
		}
		out = append(out, ClaudeToolUseBlock{
			Type:  "tool_use",
			ID:    c.ID,
			Name:  c.Name,
			Input: input,
		})
	}
	return out
}

// FromClaudeToolUseBlocks extracts tool calls from Anthropic content blocks.
func FromClaudeToolUseBlocks(blocks []map[string]json.RawMessage) []types.ToolCall {
	var out []types.ToolCall
	for _, b := range blocks {
		var typ string
		_ = json.Unmarshal(b["type"], &typ)
		if typ != "tool_use" {
			continue
		}
		var id, name string
		_ = json.Unmarshal(b["id"], &id)
		_ = json.Unmarshal(b["name"], &name)
		args := "{}"
		if len(b["input"]) > 0 && string(b["input"]) != "null" {
			args = string(b["input"])
		}
		out = append(out, types.ToolCall{ID: id, Name: name, Arguments: args})
	}
	return out
}

// MarshalClaudeMessagesRequest encodes req for Anthropic /v1/messages.
// Tools (input_schema), tool calls (tool_use), and tool results (tool_result)
// are properly formatted according to Anthropic API requirements with role alternation.
func MarshalClaudeMessagesRequest(req *types.ChatRequest, model string) ([]byte, error) {
	if req == nil {
		return nil, fmt.Errorf("nil request")
	}
	if model == "" {
		model = req.Model
	}
	if model == "" {
		model = "claude-3-7-sonnet-20250219"
	}

	payload := map[string]any{
		"model":      model,
		"max_tokens": 8192,
		"stream":     true,
	}
	if !req.Stream {
		payload["stream"] = false
	}
	if req.Temperature > 0 {
		payload["temperature"] = req.Temperature
	}
	if req.Thinking && req.ThinkingBudget > 0 {
		payload["thinking"] = map[string]any{
			"type":          "enabled",
			"budget_tokens": req.ThinkingBudget,
		}
	}
	if len(req.Tools) > 0 {
		payload["tools"] = ToClaudeTools(req.Tools)
		if req.ToolChoice != nil {
			if tc := normalizeClaudeToolChoice(req.ToolChoice); tc != nil {
				payload["tool_choice"] = tc
			}
		}
	}

	var systemInstructions []string
	type anthropicBlock map[string]any
	type anthropicMsg struct {
		Role    string           `json:"role"`
		Content []anthropicBlock `json:"content"`
	}

	var rawMsgs []anthropicMsg

	for _, m := range req.Messages {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		switch role {
		case "system":
			if strings.TrimSpace(m.Content) != "" {
				systemInstructions = append(systemInstructions, m.Content)
			}
		case "tool":
			callID := m.ToolCallID
			if callID == "" {
				callID = m.Name
			}
			content := m.Content
			if content == "" {
				content = "{}"
			}
			rawMsgs = append(rawMsgs, anthropicMsg{
				Role: "user",
				Content: []anthropicBlock{
					{
						"type":         "tool_result",
						"tool_use_id": callID,
						"content":     content,
					},
				},
			})
		case "assistant":
			var blocks []anthropicBlock
			if strings.TrimSpace(m.Content) != "" {
				blocks = append(blocks, anthropicBlock{
					"type": "text",
					"text": m.Content,
				})
			}
			for _, tc := range m.ToolCalls {
				callID := tc.ID
				if callID == "" {
					callID = tc.Name
				}
				args := json.RawMessage(`{}`)
				if strings.TrimSpace(tc.Arguments) != "" && json.Valid([]byte(tc.Arguments)) {
					args = json.RawMessage(tc.Arguments)
				}
				blocks = append(blocks, anthropicBlock{
					"type":  "tool_use",
					"id":    callID,
					"name":  tc.Name,
					"input": args,
				})
			}
			if len(blocks) > 0 {
				rawMsgs = append(rawMsgs, anthropicMsg{
					Role:    "assistant",
					Content: blocks,
				})
			}
		default: // "user"
			if strings.TrimSpace(m.Content) == "" && len(m.ToolCalls) == 0 {
				continue
			}
			block := anthropicBlock{
				"type": "text",
				"text": m.Content,
			}
			if m.CacheControl {
				block["cache_control"] = map[string]string{"type": "ephemeral"}
			}
			rawMsgs = append(rawMsgs, anthropicMsg{
				Role:    "user",
				Content: []anthropicBlock{block},
			})
		}
	}

	if len(systemInstructions) > 0 {
		sysText := strings.Join(systemInstructions, "\n\n")
		if req.SystemCacheControl {
			payload["system"] = []anthropicBlock{
				{
					"type":          "text",
					"text":          sysText,
					"cache_control": map[string]string{"type": "ephemeral"},
				},
			}
		} else {
			payload["system"] = sysText
		}
	}

	// Coalesce messages to enforce Anthropic alternating role requirement:
	// User -> Assistant -> User -> Assistant...
	var finalMsgs []anthropicMsg
	for _, m := range rawMsgs {
		if len(finalMsgs) == 0 {
			if m.Role != "user" {
				// Anthropic requires the first message to be user
				finalMsgs = append(finalMsgs, anthropicMsg{
					Role:    "user",
					Content: []anthropicBlock{{"type": "text", "text": "Hello"}},
				})
			}
			finalMsgs = append(finalMsgs, m)
			continue
		}
		last := &finalMsgs[len(finalMsgs)-1]
		if last.Role == m.Role {
			// Merge consecutive same-role messages (e.g. multiple tool_results in user role)
			last.Content = append(last.Content, m.Content...)
		} else {
			finalMsgs = append(finalMsgs, m)
		}
	}

	if len(finalMsgs) == 0 {
		finalMsgs = []anthropicMsg{
			{
				Role:    "user",
				Content: []anthropicBlock{{"type": "text", "text": "Hello"}},
			},
		}
	}

	// Ensure at least one message-level cache breakpoint near the conversation tail
	// so multi-turn conversations achieve prompt cache hits on past turns.
	hasMsgCache := false
	for _, fm := range finalMsgs {
		for _, b := range fm.Content {
			if b["cache_control"] != nil {
				hasMsgCache = true
				break
			}
		}
		if hasMsgCache {
			break
		}
	}
	if !hasMsgCache && len(finalMsgs) >= 2 {
		// Place cache breakpoint on the turn before the latest turn
		targetIdx := len(finalMsgs) - 2
		if len(finalMsgs[targetIdx].Content) > 0 {
			lastBlk := finalMsgs[targetIdx].Content[len(finalMsgs[targetIdx].Content)-1]
			lastBlk["cache_control"] = map[string]string{"type": "ephemeral"}
		}
	}

	payload["messages"] = finalMsgs
	return json.Marshal(payload)
}

func normalizeClaudeToolChoice(tc any) any {
	if tc == nil {
		return nil
	}
	switch v := tc.(type) {
	case string:
		switch v {
		case "auto":
			return map[string]string{"type": "auto"}
		case "none":
			return nil
		case "required", "any":
			return map[string]string{"type": "any"}
		default:
			return map[string]string{"type": "tool", "name": v}
		}
	case map[string]any:
		return v
	}
	return map[string]string{"type": "auto"}
}
