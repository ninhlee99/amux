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

// ValidateCallsAgainstManifest validates all tool calls against the active host manifest.
// Catches unlisted/hallucinated tools, missing required parameters, and malformed JSON.
func ValidateCallsAgainstManifest(calls []types.ToolCall, m *RuntimeManifest) []*ValidationError {
	if len(calls) == 0 || m == nil {
		return nil
	}

	byName := make(map[string]NativeToolDefinition, len(m.Tools))
	for _, t := range m.Tools {
		byName[strings.ToLower(t.Name)] = t
	}

	var errs []*ValidationError
	for _, c := range calls {
		def, exists := byName[strings.ToLower(c.Name)]
		if !exists {
			errs = append(errs, &ValidationError{
				ToolName: c.Name,
				Message:  fmt.Sprintf("tool '%s' is not supported in %s (unlisted/hallucinated tool)", c.Name, m.Runtime),
			})
			continue
		}
		if vErr := ValidateToolCall(c, def); vErr != nil {
			errs = append(errs, vErr)
		}
	}
	return errs
}
