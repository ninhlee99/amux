package router

import (
	"strings"

	"amux-accounts/pkg/types"
)

// PreferredGroupsForTask returns soft-preference group order for a task kind.
// These groups are tried first; every other eligible group remains available.
// Empty kind / general → nil (keep default rotate order).
func PreferredGroupsForTask(kind string) []string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case TaskCoding, TaskFix:
		// Implement / fix → prefer native API + Codex; web still allowed later.
		return []string{
			GroupAPIOther,
			GroupCodexSub, GroupCodexFree,
			GroupAGYSub, GroupAGYFree,
			GroupClaudeWeb, GroupChatGPTWeb, GroupGeminiWeb,
		}
	case TaskAnalysis:
		// Analyze / explain → prefer web (+ AGY); API/Codex still failover.
		return []string{
			GroupClaudeWeb, GroupChatGPTWeb, GroupGeminiWeb,
			GroupAGYSub, GroupAGYFree,
			GroupAPIOther,
			GroupCodexSub, GroupCodexFree,
		}
	case TaskReview:
		return []string{
			GroupClaudeWeb, GroupGeminiWeb, GroupChatGPTWeb,
			GroupAGYSub, GroupAGYFree,
			GroupAPIOther,
			GroupCodexSub, GroupCodexFree,
		}
	case TaskCompact:
		return []string{
			GroupChatGPTWeb, GroupClaudeWeb, GroupGeminiWeb,
			GroupAPIOther,
			GroupAGYSub, GroupAGYFree,
			GroupCodexSub, GroupCodexFree,
		}
	case TaskQuality:
		return []string{
			GroupGeminiWeb, GroupClaudeWeb, GroupChatGPTWeb,
			GroupAGYSub, GroupAGYFree,
			GroupAPIOther,
			GroupCodexSub, GroupCodexFree,
		}
	default:
		return nil
	}
}

// SoftPreferGroups reorders base so prefer appears first (intersection only),
// then the remaining base groups. Never drops a group from base.
func SoftPreferGroups(base, prefer []string) []string {
	if len(prefer) == 0 || len(base) == 0 {
		return base
	}
	inBase := make(map[string]bool, len(base))
	for _, g := range base {
		inBase[g] = true
	}
	seen := make(map[string]bool, len(base))
	out := make([]string, 0, len(base))
	for _, g := range prefer {
		if inBase[g] && !seen[g] {
			out = append(out, g)
			seen[g] = true
		}
	}
	for _, g := range base {
		if !seen[g] {
			out = append(out, g)
		}
	}
	return out
}

// GroupOrderForRequest is the try-order for pool failover: IDE proxy rules,
// then soft task preference, then webPolicy lift. Manual pin / affinity still win.
func GroupOrderForRequest(req *types.ChatRequest) []string {
	ide := ""
	kind := ""
	if req != nil {
		ide = IDEFromClientDialect(req.ClientDialect)
		kind = req.TaskKind
	}
	base := ProxyGroupsForClient(ide)
	order := SoftPreferGroups(base, PreferredGroupsForTask(kind))
	switch EffectiveWebPolicy() {
	case WebPolicyForce:
		return SoftPreferGroups(order, []string{GroupClaudeWeb, GroupChatGPTWeb, GroupGeminiWeb})
	case WebPolicyPrefer:
		// Lift web ahead of free tiers for coding/fix too (still after native IDE).
		return SoftPreferGroups(order, []string{
			GroupClaudeWeb, GroupChatGPTWeb, GroupGeminiWeb,
			GroupAPIOther,
		})
	default:
		return order
	}
}

func groupRankIn(order []string, group string) int {
	for i, g := range order {
		if g == group {
			return i
		}
	}
	return len(order) + 1
}

// sortAdaptersByGroupOrder stable-sorts adapters by group rank in order.
func sortAdaptersByGroupOrder(adapters []types.ProviderAdapter, order []string) []types.ProviderAdapter {
	out := make([]types.ProviderAdapter, len(adapters))
	copy(out, adapters)
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && groupRankIn(order, DetermineAdapterGroup(out[j])) < groupRankIn(order, DetermineAdapterGroup(out[j-1])) {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out
}
