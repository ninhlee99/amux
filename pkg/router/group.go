package router

import (
	"strings"

	"amux-accounts/pkg/types"
)

const (
	GroupClaudeSub  = "claude_sub"  // 1. Claude subscription (Pro/Team)
	GroupCodexSub   = "codex_sub"   // 2. Codex subscription (ChatGPT Plus/Pro/Team)
	GroupAGYSub     = "agy_sub"     // 3. AGY subscription (Google Workspace/AI Premium)
	GroupClaudeFree = "claude_free" // 4. Claude free (if any)
	GroupCodexFree  = "codex_free"  // 5. Codex free (ChatGPT Free)
	GroupAGYFree    = "agy_free"    // 6. AGY free (Google Consumer Free)
	GroupAPIOther   = "api_other"   // 7. API other (Gemini API, Groq, Kimi, OpenRouter, etc.)
	GroupClaudeWeb  = "claude_web"  // 8. Claude web
	GroupChatGPTWeb = "chatgpt_web" // 9. ChatGPT web
	GroupGeminiWeb  = "gemini_web"  // 10. Gemini web
)

// GroupPriority defines the exact execution priority specified by the user:
// 1. Claude subscription
// 2. Codex subscription
// 3. AGY subscription
// 4. Claude free
// 5. Codex free
// 6. AGY free
// 7. API other
// 8. Claude web
// 9. ChatGPT web
// 10. Gemini web
var GroupPriority = []string{
	GroupClaudeSub,
	GroupCodexSub,
	GroupAGYSub,
	GroupClaudeFree,
	GroupCodexFree,
	GroupAGYFree,
	GroupAPIOther,
	GroupClaudeWeb,
	GroupChatGPTWeb,
	GroupGeminiWeb,
}

// Client IDE that owns the inbound request. Native-IDE accounts are tried
// first (main); every other group is failover proxy for that session.
const (
	IDEClaude = "claude"
	IDECodex  = "codex"
	IDEAGY    = "agy"
)

// IDEFromClientDialect maps a ChatRequest.ClientDialect (claude/codex/gemini/cursor)
// onto the IDE whose subscription accounts should be main.
func IDEFromClientDialect(dialect string) string {
	switch strings.ToLower(strings.TrimSpace(dialect)) {
	case "claude", "anthropic":
		return IDEClaude
	case "codex":
		return IDECodex
	case "gemini", "agy", "antigravity":
		return IDEAGY
	default:
		// Cursor / generic OpenAI: no native subscription group — keep
		// the global GroupPriority order.
		return ""
	}
}

// NativeGroups are the main (same-IDE) groups for an inbound client.
func NativeGroups(ide string) []string {
	switch ide {
	case IDEClaude:
		return []string{GroupClaudeSub, GroupClaudeFree}
	case IDECodex:
		return []string{GroupCodexSub, GroupCodexFree}
	case IDEAGY:
		return []string{GroupAGYSub, GroupAGYFree}
	default:
		return nil
	}
}

// GroupPriorityForIDE puts this IDE's subscription/free groups first, then
// every other group in the default order (those are proxy/failover).
func GroupPriorityForIDE(ide string) []string {
	native := NativeGroups(ide)
	if len(native) == 0 {
		return GroupPriority
	}
	seen := make(map[string]bool, len(native))
	out := make([]string, 0, len(GroupPriority))
	for _, g := range native {
		out = append(out, g)
		seen[g] = true
	}
	for _, g := range GroupPriority {
		if seen[g] {
			continue
		}
		out = append(out, g)
	}
	return out
}

// GroupDisplayName returns a clean human-readable name for each group.
func GroupDisplayName(group string) string {
	switch group {
	case GroupClaudeSub:
		return "Claude Subscription"
	case GroupCodexSub:
		return "Codex Subscription"
	case GroupAGYSub:
		return "AGY Subscription"
	case GroupClaudeFree:
		return "Claude Free"
	case GroupCodexFree:
		return "Codex Free"
	case GroupAGYFree:
		return "AGY Free"
	case GroupAPIOther:
		return "API Other"
	case GroupClaudeWeb:
		return "Claude Web"
	case GroupChatGPTWeb:
		return "ChatGPT Web"
	case GroupGeminiWeb:
		return "Gemini Web"
	default:
		return group
	}
}

// GroupAwareAdapter is optionally implemented by adapters that have explicit group info.
type GroupAwareAdapter interface {
	Group() string
}

// DetermineAdapterGroup resolves the group for any ProviderAdapter.
func DetermineAdapterGroup(a types.ProviderAdapter) string {
	if ga, ok := a.(GroupAwareAdapter); ok {
		if g := ga.Group(); g != "" {
			return g
		}
	}

	id := strings.ToLower(a.ID())

	// 8. Claude Web
	if strings.Contains(id, "claude:web") || strings.Contains(id, "claude_web") {
		return GroupClaudeWeb
	}

	// 9. ChatGPT Web
	if strings.Contains(id, "chatgpt") && !strings.Contains(id, "codex") {
		return GroupChatGPTWeb
	}

	// 10. Gemini Web
	if strings.Contains(id, "gemini:web") || strings.Contains(id, "gemini_web") {
		return GroupGeminiWeb
	}

	// 2 & 5. Codex CLI
	if strings.HasPrefix(id, "codex") {
		if strings.Contains(id, "free") {
			return GroupCodexFree
		}
		return GroupCodexSub
	}

	// 3 & 6. Antigravity / AGY
	if strings.HasPrefix(id, "agy") || strings.HasPrefix(id, "antigravity") {
		if strings.Contains(id, "free") {
			return GroupAGYFree
		}
		return GroupAGYSub
	}

	// 1 & 4. Claude OAuth profiles
	if strings.HasPrefix(id, "claude") {
		if strings.Contains(id, "free") {
			return GroupClaudeFree
		}
		return GroupClaudeSub
	}

	// 7. API Other (Gemini API, Groq, Kimi, OpenRouter, GitHub Models, etc.)
	return GroupAPIOther
}
