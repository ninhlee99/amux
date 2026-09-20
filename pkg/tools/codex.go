package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"amux-accounts/pkg/types"
	"amux-accounts/pkg/utils"
)

// CodexResponsesTool is the flat Responses API tools[] entry Codex CLI and
// chatgpt.com/backend-api/codex/responses speak (name at the top level, not
// nested under function).
type CodexResponsesTool struct {
	Type        string          `json:"type"` // "function"
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// ParseCodexTools extracts tool defs from a Codex OpenAI chat.completions body.
func ParseCodexTools(body []byte) ([]types.ToolDef, error) {
	return parseOpenAITools(body)
}

// ParseCodexResponsesTools extracts defs from a Responses API tools[] value.
// Accepts both the flat Codex/Responses shape and nested Chat Completions
// {"type":"function","function":{...}} so either client dialect round-trips.
func ParseCodexResponsesTools(raw []byte) []types.ToolDef {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var list []map[string]any
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil
	}
	out := make([]types.ToolDef, 0, len(list))
	for _, t := range list {
		name, _ := t["name"].(string)
		desc, _ := t["description"].(string)
		var params json.RawMessage
		if fn, ok := t["function"].(map[string]any); ok {
			if fnName, ok := fn["name"].(string); ok && fnName != "" {
				name = fnName
			}
			if fnDesc, ok := fn["description"].(string); ok && fnDesc != "" {
				desc = fnDesc
			}
			if p, ok := fn["parameters"]; ok {
				params, _ = json.Marshal(p)
			}
		} else if p, ok := t["parameters"]; ok {
			params, _ = json.Marshal(p)
		}
		if strings.TrimSpace(name) == "" {
			continue
		}
		out = append(out, types.ToolDef{
			Name:        name,
			Description: desc,
			InputSchema: utils.NormalizeJSONSchema(params),
		})
	}
	return out
}

// ToCodexTools converts canonical defs to Codex/OpenAI chat.completions tools[].
func ToCodexTools(defs []types.ToolDef) []openAITool {
	return toOpenAITools(defs)
}

// ToCodexResponsesTools converts canonical defs to Codex Responses tools[].
func ToCodexResponsesTools(defs []types.ToolDef) []CodexResponsesTool {
	out := make([]CodexResponsesTool, 0, len(defs))
	for _, d := range defs {
		out = append(out, CodexResponsesTool{
			Type:        "function",
			Name:        d.Name,
			Description: d.Description,
			Parameters:  utils.NormalizeJSONSchema(d.InputSchema),
		})
	}
	return out
}

// ToCodexToolCalls maps canonical calls to Codex chat.completions tool_calls.
func ToCodexToolCalls(calls []types.ToolCall) []OpenAIToolCall {
	return ToOpenAIToolCalls(calls)
}

// FromCodexToolCalls maps Codex chat.completions tool_calls to canonical.
func FromCodexToolCalls(calls []OpenAIToolCall) []types.ToolCall {
	return FromOpenAIToolCalls(calls)
}

// MarshalCodexChatRequest encodes req for an OpenAI-compatible upstream
// when the client dialect is Codex (full-context agent transcripts).
func MarshalCodexChatRequest(req *types.ChatRequest) ([]byte, error) {
	return MarshalOpenAIChatRequest(req)
}

// MarshalCodexResponsesRequest encodes req for the Codex Responses backend
// (chatgpt.com/backend-api/codex/responses). Client tools (Claude input_schema,
// AGY functionDeclarations, OpenAI function.parameters) arrive canonical and
// leave as flat Responses tools[] plus function_call / function_call_output
// input items so the model can call them natively.
func MarshalCodexResponsesRequest(req *types.ChatRequest) ([]byte, error) {
	if req == nil {
		return nil, fmt.Errorf("nil request")
	}
	payload := map[string]any{
		"model":  req.Model,
		"store":  false,
		"stream": true,
	}
	if !req.Stream {
		payload["stream"] = false
	}
	// NOTE: Codex Responses API (chatgpt.com/backend-api/codex/responses)
	// rejects temperature with HTTP 400 "Unsupported parameter: temperature".
	// Do not forward it regardless of the value in req.Temperature.
	if len(req.Tools) > 0 {
		payload["tools"] = ToCodexResponsesTools(req.Tools)
		payload["parallel_tool_calls"] = true
		if req.ToolChoice != nil {
			payload["tool_choice"] = normalizeOpenAIToolChoice(req.ToolChoice)
		}
	}

	var instructions []string
	var input []map[string]any
	for _, m := range req.Messages {
		switch strings.ToLower(strings.TrimSpace(m.Role)) {
		case "system":
			if strings.TrimSpace(m.Content) != "" {
				instructions = append(instructions, m.Content)
			}
		case "tool":
			callID := m.ToolCallID
			if callID == "" {
				callID = m.Name
			}
			input = append(input, map[string]any{
				"type":    "function_call_output",
				"call_id": callID,
				"output":  m.Content,
			})
		case "assistant":
			if strings.TrimSpace(m.Content) != "" {
				input = append(input, map[string]any{
					"role":    "assistant",
					"content": m.Content,
				})
			}
			for _, tc := range m.ToolCalls {
				callID := tc.ID
				if callID == "" {
					callID = tc.Name
				}
				args := tc.Arguments
				if strings.TrimSpace(args) == "" {
					args = "{}"
				}
				input = append(input, map[string]any{
					"type":      "function_call",
					"name":      tc.Name,
					"call_id":   callID,
					"arguments": args,
				})
			}
		default:
			if strings.TrimSpace(m.Content) == "" && len(m.ToolCalls) == 0 {
				continue
			}
			role := m.Role
			if role == "" {
				role = "user"
			}
			input = append(input, map[string]any{
				"role":    role,
				"content": m.Content,
			})
		}
	}
	if len(instructions) > 0 {
		payload["instructions"] = strings.Join(instructions, "\n\n")
	}
	if len(input) == 0 {
		input = []map[string]any{{"role": "user", "content": "Hello"}}
	}
	payload["input"] = input
	return json.Marshal(payload)
}
