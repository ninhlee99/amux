package runtime

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"amux-accounts/pkg/tools/jsonrepair"
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

// ValidateToolCall validates that a tool call strictly complies with the native tool definition.
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

// NormalizeToolCallArguments heals, reconciles aliases, and coerces types for a tool call.
func NormalizeToolCallArguments(call types.ToolCall, def NativeToolDefinition) (types.ToolCall, error) {
	raw := strings.TrimSpace(call.Arguments)
	if raw == "" || raw == "null" {
		raw = "{}"
	}

	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		// Attempt JSON repair
		repaired := jsonrepair.Repair(raw)
		if err2 := json.Unmarshal([]byte(repaired), &args); err2 != nil {
			return call, fmt.Errorf("invalid JSON arguments for %s: %w", call.Name, err)
		}
	}

	// Unwrap outer wrappers if present (e.g. {"parameters": {...}} or {"arguments": {...}})
	if len(args) == 1 {
		for _, wrapperKey := range []string{"parameters", "arguments", "input"} {
			if inner, ok := args[wrapperKey].(map[string]any); ok && inner != nil {
				args = inner
				break
			}
		}
	}

	// Parameter alias clusters
	aliasClusters := [][]string{
		{"CommandLine", "command", "cmd", "script", "input", "code"},
		{"AbsolutePath", "file_path", "path", "TargetFile", "filename", "file"},
		{"CodeContent", "content", "contents", "text", "body"},
		{"TargetContent", "old_string", "old_str", "old", "original", "search"},
		{"ReplacementContent", "new_string", "new_str", "new", "replacement", "replace"},
		{"name", "workflow_name", "workflow", "id", "skill_name", "skill"},
		{"inputs", "arguments", "args", "params", "parameters"},
		{"ServerName", "server_name", "server", "Server"},
		{"ToolName", "tool_name", "tool", "Tool"},
		{"Arguments", "arguments", "args", "params"},
		{"Cwd", "cwd", "dir", "directory"},
		{"StartLine", "start_line", "start", "offset"},
		{"EndLine", "end_line", "end", "limit"},
		{"AllowMultiple", "allow_multiple"},
		{"Append", "append"},
		{"Overwrite", "overwrite"},
	}

	for _, p := range def.Parameters {
		if _, exists := args[p.Name]; exists {
			continue
		}

		// Try case-insensitive direct match
		found := false
		for k, v := range args {
			if strings.EqualFold(k, p.Name) {
				args[p.Name] = v
				found = true
				break
			}
		}
		if found {
			continue
		}

		// Try cluster aliases
		for _, cluster := range aliasClusters {
			inCluster := false
			for _, member := range cluster {
				if strings.EqualFold(member, p.Name) {
					inCluster = true
					break
				}
			}
			if inCluster {
				for _, alt := range cluster {
					for k, v := range args {
						if strings.EqualFold(k, alt) && v != nil {
							args[p.Name] = v
							found = true
							break
						}
					}
					if found {
						break
					}
				}
			}
			if found {
				break
			}
		}
	}

	// Type healing
	for _, p := range def.Parameters {
		val, exists := args[p.Name]
		if !exists || val == nil {
			continue
		}

		switch p.Type {
		case "integer", "number":
			if strVal, ok := val.(string); ok {
				if n, err := strconv.Atoi(strings.TrimSpace(strVal)); err == nil {
					args[p.Name] = n
				} else if f, err := strconv.ParseFloat(strings.TrimSpace(strVal), 64); err == nil {
					args[p.Name] = f
				}
			}
		case "boolean":
			if strVal, ok := val.(string); ok {
				lower := strings.ToLower(strings.TrimSpace(strVal))
				if lower == "true" || lower == "1" {
					args[p.Name] = true
				} else if lower == "false" || lower == "0" {
					args[p.Name] = false
				}
			}
		}
	}

	marshaled, err := json.Marshal(args)
	if err != nil {
		return call, err
	}
	call.Arguments = string(marshaled)
	return call, nil
}
