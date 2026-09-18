package runtime

import (
	"encoding/json"
	"sort"
	"strings"

	"amux-accounts/pkg/types"
)

// ToolParameter describes one property in a tool's JSON schema.
type ToolParameter struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Required    bool     `json:"required"`
	Enum        []string `json:"enum,omitempty"`
}

// NativeToolDefinition represents a single native runtime capability.
type NativeToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  []ToolParameter `json:"parameters,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

// RuntimeManifest encapsulates all capabilities exposed by an execution environment.
type RuntimeManifest struct {
	Runtime     string                 `json:"runtime"`
	Description string                 `json:"description,omitempty"`
	Tools       []NativeToolDefinition `json:"tools"`
	Metadata    map[string]any         `json:"metadata,omitempty"`
}

// DetectRuntimeName accurately detects the host runtime environment from dialect and tools.
func DetectRuntimeName(dialect string, tools []types.ToolDef) string {
	d := strings.ToLower(dialect)
	switch {
	case d == "claude":
		return "ClaudeCode"
	case d == "gemini" || d == "antigravity":
		return "Antigravity"
	case d == "cursor":
		return "Cursor"
	case d == "codex":
		return "Codex"
	}
	// Fingerprint tool names if dialect is generic or empty
	for _, t := range tools {
		name := strings.ToLower(t.Name)
		if name == "run_command" || name == "replace_file_content" || name == "call_mcp_tool" || name == "view_file" {
			return "Antigravity"
		}
		if name == "bash" || name == "fileedit" || name == "fileread" || name == "filewrite" {
			return "ClaudeCode"
		}
		if name == "run_terminal_command" || name == "read_file" {
			return "Cursor"
		}
	}
	if dialect != "" {
		return strings.ToUpper(dialect[:1]) + dialect[1:]
	}
	return "NativeRuntime"
}

// DiscoverManifest dynamically builds a RuntimeManifest from a client request.
func DiscoverManifest(req *types.ChatRequest) *RuntimeManifest {
	if req == nil {
		return &RuntimeManifest{Runtime: "Generic"}
	}
	runtimeName := DetectRuntimeName(req.ClientDialect, req.Tools)
	return FromToolDefs(runtimeName, req.Tools)
}

// FromToolDefs parses provider-agnostic types.ToolDef slice into a structured RuntimeManifest.
func FromToolDefs(runtimeName string, defs []types.ToolDef) *RuntimeManifest {
	m := &RuntimeManifest{
		Runtime:  runtimeName,
		Tools:    make([]NativeToolDefinition, 0, len(defs)),
		Metadata: make(map[string]any),
	}

	for _, d := range defs {
		if strings.TrimSpace(d.Name) == "" {
			continue
		}
		nd := NativeToolDefinition{
			Name:        d.Name,
			Description: d.Description,
			InputSchema: d.InputSchema,
			Parameters:  parseSchemaParameters(d.InputSchema),
		}
		m.Tools = append(m.Tools, nd)
	}

	return m
}

// ToToolDefs converts the manifest tools back to standard types.ToolDef slice.
func (m *RuntimeManifest) ToToolDefs() []types.ToolDef {
	if m == nil {
		return nil
	}
	defs := make([]types.ToolDef, 0, len(m.Tools))
	for _, t := range m.Tools {
		defs = append(defs, types.ToolDef{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
	}
	return defs
}

type rawJSONSchema struct {
	Type       string                     `json:"type"`
	Required   []string                   `json:"required"`
	Properties map[string]rawPropertySpec `json:"properties"`
}

type rawPropertySpec struct {
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Enum        []string `json:"enum"`
}

func parseSchemaParameters(raw json.RawMessage) []ToolParameter {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var s rawJSONSchema
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil
	}

	reqMap := make(map[string]bool, len(s.Required))
	for _, r := range s.Required {
		reqMap[r] = true
	}

	var params []ToolParameter
	// First add required parameters in declared order
	for _, r := range s.Required {
		prop, exists := s.Properties[r]
		pType := "any"
		pDesc := ""
		var pEnum []string
		if exists {
			if prop.Type != "" {
				pType = prop.Type
			}
			pDesc = prop.Description
			pEnum = prop.Enum
		}
		params = append(params, ToolParameter{
			Name:        r,
			Type:        pType,
			Description: pDesc,
			Required:    true,
			Enum:        pEnum,
		})
	}

	// Then add optional parameters sorted
	var optionalKeys []string
	for k := range s.Properties {
		if !reqMap[k] {
			optionalKeys = append(optionalKeys, k)
		}
	}
	sort.Strings(optionalKeys)

	for _, k := range optionalKeys {
		prop := s.Properties[k]
		pType := "any"
		if prop.Type != "" {
			pType = prop.Type
		}
		params = append(params, ToolParameter{
			Name:        k,
			Type:        pType,
			Description: prop.Description,
			Required:    false,
			Enum:        prop.Enum,
		})
	}

	return params
}
