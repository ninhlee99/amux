package runtime

import (
	"strings"
	"sync"

	"amux-accounts/pkg/types"
)

// Registry manages discovered runtime manifests and native tool definitions.
type Registry struct {
	mu        sync.RWMutex
	manifests map[string]*RuntimeManifest
	tools     map[string]NativeToolDefinition
}

// NewRegistry creates a fresh Registry instance.
func NewRegistry() *Registry {
	return &Registry{
		manifests: make(map[string]*RuntimeManifest),
		tools:     make(map[string]NativeToolDefinition),
	}
}

// GlobalRegistry is the default singleton registry.
var GlobalRegistry = NewRegistry()

// RegisterManifest registers a runtime manifest and indexes its tools.
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
	}
}

// GetTool retrieves a tool definition by name (case-insensitive fallback).
func (r *Registry) GetTool(name string) (NativeToolDefinition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if t, ok := r.tools[name]; ok {
		return t, true
	}
	if t, ok := r.tools[strings.ToLower(name)]; ok {
		return t, true
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
