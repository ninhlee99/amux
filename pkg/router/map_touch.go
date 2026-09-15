package router

import (
	"encoding/json"
	"strings"

	"amux-accounts/pkg/nav"
	"amux-accounts/pkg/types"
)

// maybeAutoTouchMap records Read/Grep/Glob paths from the live tool loop into
// the project focus map (0 LLM tokens). Best-effort; never blocks the request.
func maybeAutoTouchMap(req *types.ChatRequest) {
	if req == nil || len(req.Messages) == 0 {
		return
	}
	root := projectRootFromRequest(req)
	if root == "" {
		return
	}
	files := extractTouchedFiles(req.Messages)
	if len(files) == 0 {
		return
	}
	_, _ = nav.NoteTouch(root, files, nil)
}

func projectRootFromRequest(req *types.ChatRequest) string {
	if req.Metadata == nil {
		return ""
	}
	for _, k := range []string{"project", "cwd", "root", "workspace"} {
		if v, ok := req.Metadata[k].(string); ok {
			if s := strings.TrimSpace(v); s != "" {
				return s
			}
		}
	}
	return ""
}

func extractTouchedFiles(msgs []types.ChatMessage) []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" || strings.HasPrefix(p, "http") || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, m := range msgs {
		for _, tc := range m.ToolCalls {
			name := strings.ToLower(tc.Name)
			if !strings.Contains(name, "read") && name != "grep" && name != "glob" &&
				!strings.Contains(name, "view_file") {
				continue
			}
			var args map[string]any
			if json.Unmarshal([]byte(tc.Arguments), &args) != nil {
				continue
			}
			for _, k := range []string{"file_path", "path", "AbsolutePath", "target_file", "filename", "pattern"} {
				if v, ok := args[k].(string); ok && (k != "pattern" || strings.Contains(v, "/")) {
					if k == "pattern" {
						continue
					}
					add(v)
				}
			}
		}
	}
	return out
}
