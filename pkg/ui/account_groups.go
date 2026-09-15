package ui

import (
	"sort"
	"strings"

	"amux-accounts/pkg/router"
	"amux-accounts/pkg/term"
	"amux-accounts/pkg/types"
)

// accountRow is one printable line for accounts / pool / ls tables.
type accountRow struct {
	Group    string
	Provider string // API Other only: gemini / groq / openrouter / …
	ID       string
	Kind     string
	Account  string
	Plan     string
	Pool     string
	Model    string
	Priority int
	Active   string
	Off      string
	Saved    string
}

func groupSectionTitle(group string) string {
	return router.GroupDisplayName(group)
}

func printGroupSection(group string) {
	term.Section(groupSectionTitle(group))
}

func printDisplaySection(title string) {
	term.Section(title)
}

// apiProviderBrand extracts the vendor from a unified API id (gemini:api:01 → gemini).
func apiProviderBrand(id, kind string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if pre, _, ok := types.ParseID(id); ok {
		if i := strings.IndexByte(pre, ':'); i > 0 {
			return pre[:i]
		}
		if pre != "" {
			return pre
		}
	}
	if i := strings.IndexByte(id, ':'); i > 0 {
		return id[:i]
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch {
	case strings.Contains(kind, "gemini"):
		return "gemini"
	case strings.Contains(kind, "groq"):
		return "groq"
	case strings.Contains(kind, "openrouter"):
		return "openrouter"
	case strings.Contains(kind, "github"):
		return "github"
	case strings.Contains(kind, "kimi") || strings.Contains(kind, "moonshot"):
		return "kimi"
	case strings.Contains(kind, "grok") || strings.Contains(kind, "xai"):
		return "grok"
	case kind == "" || kind == "api":
		if id != "" {
			return id
		}
		return "other"
	default:
		return kind
	}
}

func withAPIProvider(r accountRow) accountRow {
	if r.Group == router.GroupAPIOther && r.Provider == "" {
		r.Provider = apiProviderBrand(r.ID, r.Kind)
	}
	return r
}

func apiSectionTitle(brand string) string {
	brand = strings.TrimSpace(brand)
	if brand == "" {
		brand = "other"
	}
	return "API · " + strings.ToUpper(brand)
}

// sortAccountRowsByGroup orders by GroupPriority, then API provider, then priority, then id.
func sortAccountRowsByGroup(rows []accountRow) {
	for i := range rows {
		rows[i] = withAPIProvider(rows[i])
	}
	sort.SliceStable(rows, func(i, j int) bool {
		ii, jj := router.GroupIndex(rows[i].Group), router.GroupIndex(rows[j].Group)
		if ii != jj {
			return ii < jj
		}
		if rows[i].Group == router.GroupAPIOther && rows[i].Provider != rows[j].Provider {
			return rows[i].Provider < rows[j].Provider
		}
		if rows[i].Priority != rows[j].Priority {
			return rows[i].Priority < rows[j].Priority
		}
		return rows[i].ID < rows[j].ID
	})
}

// forEachAccountGroup walks rows by GroupPriority. API Other splits into
// one section per provider (API · GEMINI, API · GROQ, …).
func forEachAccountGroup(rows []accountRow, fn func(sectionTitle, group string, members []accountRow)) {
	if len(rows) == 0 {
		return
	}
	sortAccountRowsByGroup(rows)

	order := append([]string{}, router.GroupPriority...)
	seen := make(map[string]bool, len(order))
	for _, g := range order {
		seen[g] = true
	}
	for _, r := range rows {
		if !seen[r.Group] {
			order = append(order, r.Group)
			seen[r.Group] = true
		}
	}

	by := make(map[string][]accountRow, len(order))
	for _, r := range rows {
		by[r.Group] = append(by[r.Group], r)
	}
	for _, g := range order {
		members := by[g]
		if len(members) == 0 {
			continue
		}
		if g == router.GroupAPIOther {
			forEachAPIProvider(members, func(brand string, sub []accountRow) {
				fn(apiSectionTitle(brand), g, sub)
			})
			continue
		}
		fn(groupSectionTitle(g), g, members)
	}
}

func forEachAPIProvider(members []accountRow, fn func(brand string, sub []accountRow)) {
	var order []string
	seen := map[string]bool{}
	by := map[string][]accountRow{}
	for _, r := range members {
		r = withAPIProvider(r)
		b := r.Provider
		if b == "" {
			b = "other"
		}
		if !seen[b] {
			seen[b] = true
			order = append(order, b)
		}
		by[b] = append(by[b], r)
	}
	sort.Strings(order)
	for _, b := range order {
		fn(b, by[b])
	}
}

// matchesAccountRowFilter decides whether a row appears under am accounts/ls [filter].
func matchesAccountRowFilter(filter string, r accountRow) bool {
	filter = strings.ToLower(strings.TrimSpace(filter))
	if filter == "" {
		return true
	}
	r = withAPIProvider(r)
	group := strings.ToLower(r.Group)
	kind := strings.ToLower(r.Kind)
	id := strings.ToLower(r.ID)
	brand := strings.ToLower(r.Provider)

	switch filter {
	case "claude":
		return strings.HasPrefix(group, "claude") || kind == "claude" || strings.Contains(kind, "claude")
	case "codex":
		return strings.HasPrefix(group, "codex") || kind == "codex"
	case "agy", "antigravity":
		return strings.HasPrefix(group, "agy") || kind == "antigravity" || kind == "agy"
	case "api":
		return group == router.GroupAPIOther || kind == "api" || strings.Contains(kind, "api")
	case "web":
		return strings.HasSuffix(group, "_web") || strings.Contains(kind, "web")
	default:
		if brand == filter || strings.HasPrefix(id, filter+":") {
			return true
		}
		return group == filter || kind == filter ||
			strings.Contains(group, filter) || strings.Contains(kind, filter)
	}
}
