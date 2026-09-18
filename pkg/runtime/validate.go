package runtime

import (
	"encoding/json"
	"fmt"
	"strings"

	"amux-accounts/pkg/types"
)

// ValidationError contains details when a model-generated tool call violates the native schema.
type ValidationError struct {
	ToolName        string   `json:"tool_name"`
	MissingRequired []string `json:"missing_required,omitempty"`
	Message         string   `json:"message"`
}

func (e *ValidationError) Error() string {
	if e == nil {
		return ""
	}
	if len(e.MissingRequired) > 0 {
		return fmt.Sprintf("tool '%s' missing required properties: %s", e.ToolName, strings.Join(e.MissingRequired, ", "))
	}
	return fmt.Sprintf("tool '%s' validation error: %s", e.ToolName, e.Message)
}

// ValidateToolCall validates that a tool call complies with the native tool definition.
func ValidateToolCall(call types.ToolCall, def NativeToolDefinition) *ValidationError {
	if len(def.Parameters) == 0 {
		return nil
	}

	var args map[string]any
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
		return &ValidationError{
			ToolName: call.Name,
			Message:  "arguments must be a valid JSON object: " + err.Error(),
		}
	}

	var missing []string
	for _, p := range def.Parameters {
		if p.Required {
			if _, exists := args[p.Name]; !exists {
				missing = append(missing, p.Name)
			}
		}
	}

	if len(missing) > 0 {
		return &ValidationError{
			ToolName:        call.Name,
			MissingRequired: missing,
			Message:         fmt.Sprintf("missing required fields: %v", missing),
		}
	}

	return nil
}
