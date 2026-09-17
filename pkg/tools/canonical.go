package tools

import (
	"encoding/json"
	"fmt"
)

// UniversalTool is the canonical intermediate representation of a tool definition.
// InputSchema is preserved 100% intact (properties, required, enum, items, oneOf, anyOf,
// default, description, additionalProperties, etc.).
type UniversalTool struct {
	ID          string                 `json:"id,omitempty"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema"`
	Source      string                 `json:"source,omitempty"` // "anthropic", "openai", "gemini", "mcp", "agent"
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// UniversalToolCall is the canonical representation of an invoked tool call.
type UniversalToolCall struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
	RawJSON   string                 `json:"raw_json,omitempty"`
}

// UniversalToolResult is the canonical representation of a tool execution outcome.
type UniversalToolResult struct {
	ToolCallID string                 `json:"tool_call_id"`
	Success    bool                   `json:"success"`
	Output     string                 `json:"output"`
	Error      *UniversalToolError    `json:"error,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// UniversalToolError represents a structured error produced during tool invocation.
type UniversalToolError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
	Provider  string `json:"provider,omitempty"`
}

func (e *UniversalToolError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("[%s] %s (retryable: %v)", e.Code, e.Message, e.Retryable)
}

// ParseArguments parses a raw JSON string into a map[string]interface{}.
func ParseArguments(raw string) (map[string]interface{}, error) {
	if raw == "" {
		return make(map[string]interface{}), nil
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil, err
	}
	return args, nil
}

// SerializeArguments serializes map[string]interface{} into a JSON string.
func SerializeArguments(args map[string]interface{}) string {
	if args == nil {
		return "{}"
	}
	b, err := json.Marshal(args)
	if err != nil {
		return "{}"
	}
	return string(b)
}
