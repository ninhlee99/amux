package runtime

import (
	"strings"
	"sync"

	"amux-accounts/pkg/types"
)

// Registry manages discovered runtime manifests and native tool definitions with alias resolution.
type Registry struct {
	mu        sync.RWMutex
	manifests map[string]*RuntimeManifest
	tools     map[string]NativeToolDefinition
	aliases   map[string]string // alias (lowercase) -> canonical tool name
}

// NewRegistry creates a fresh empty Registry instance.
func NewRegistry() *Registry {
	r := &Registry{
		manifests: make(map[string]*RuntimeManifest),
		tools:     make(map[string]NativeToolDefinition),
		aliases:   make(map[string]string),
	}
	r.registerDefaultAliases()
	return r
}

// NewDefaultRegistry creates a Registry pre-populated with AMUX standard coding tools.
func NewDefaultRegistry(workspaceDir string) *Registry {
	r := NewRegistry()
	manifest := FromToolDefs("NativeRuntime", StandardToolDefs())
	r.RegisterManifest(manifest)
	return r
}

// GlobalRegistry is the default singleton registry pre-populated with standard coding tools.
var GlobalRegistry = NewDefaultRegistry(".")

func (r *Registry) registerDefaultAliases() {
	aliasPairs := map[string][]string{
		"Bash": {
			"bash", "run_command", "shell", "run_terminal_command",
			"exec_command", "terminal", "execute_command", "cmd", "sh",
		},
		"Read": {
			"read", "view_file", "read_file", "fileread", "view", "cat",
			"show_file", "get_file_content", "open_file",
		},
		"Write": {
			"write", "write_to_file", "write_file", "filewrite", "create_file",
			"new_file", "save_file", "touch",
		},
		"Edit": {
			"edit", "replace_file_content", "fileedit", "edit_file",
			"str_replace_editor", "patch", "apply_patch", "modify_file", "update_file",
		},
		"Workflow": {
			"workflow", "run_workflow", "execute_workflow", "workflow_run", "apply_workflow",
		},
		"call_mcp_tool": {
			"call_mcp_tool", "mcp", "invoke_mcp",
		},
	}

	for target, alts := range aliasPairs {
		for _, alt := range alts {
			r.aliases[strings.ToLower(alt)] = target
		}
	}
}

// RegisterManifest registers a runtime manifest and indexes its tools and aliases.
func (r *Registry) RegisterManifest(m *RuntimeManifest) {
	if m == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	r.manifests[m.Runtime] = m
	for _, t := range m.Tools {
		r.tools[t.Name] = t
		r.tools[strings.ToLower(t.Name)] = t
		r.aliases[strings.ToLower(t.Name)] = t.Name
	}
}

// Lookup retrieves a tool definition by name, case-insensitive match, or alias.
func (r *Registry) Lookup(name string) (NativeToolDefinition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return NativeToolDefinition{}, false
	}

	// 1. Direct match
	if t, ok := r.tools[trimmed]; ok {
		return t, true
	}

	// 2. Lowercase match
	lower := strings.ToLower(trimmed)
	if t, ok := r.tools[lower]; ok {
		return t, true
	}

	// 3. Alias match
	if canonical, ok := r.aliases[lower]; ok {
		if t, ok := r.tools[canonical]; ok {
			return t, true
		}
		if t, ok := r.tools[strings.ToLower(canonical)]; ok {
			return t, true
		}
	}

	// 4. MCP prefixes (mcp__* or amux_*)
	if strings.HasPrefix(lower, "mcp__") || strings.HasPrefix(lower, "mcp_") || strings.HasPrefix(lower, "amux_") {
		if t, ok := r.tools["call_mcp_tool"]; ok {
			return t, true
		}
	}

	return NativeToolDefinition{}, false
}

// ToolDefs returns all registered tools as types.ToolDef.
func (r *Registry) ToolDefs() []types.ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]bool)
	var defs []types.ToolDef
	for _, m := range r.manifests {
		for _, t := range m.Tools {
			if seen[t.Name] {
				continue
			}
			seen[t.Name] = true
			defs = append(defs, types.ToolDef{
				Name:        t.Name,
				Description: t.Description,
				InputSchema: t.InputSchema,
			})
		}
	}
	return defs
}
