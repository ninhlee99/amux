package context

import (
	"amux-accounts/pkg/ctxshrink"
	"amux-accounts/pkg/types"
)

// SafeCompactionOption configures conversation memory compaction.
type SafeCompactionOption struct {
	MidSessionRotation bool // true only when gateway rotated accounts mid-session on a non-native client
	MaxBudgetTokens    int
}

// CompactConversation safely compresses conversation history.
// Rules:
// 1. Completely disabled during normal IDE operation (native IDE compaction handles limits).
// 2. Invoked ONLY when gateway rotates accounts mid-session on clients that lack native compaction.
// 3. Compaction MUST NEVER touch or trim tools schemas.
// 4. Compaction MUST NEVER remove the most recent conversation turn containing an active tool_use / tool_result pair.
func CompactConversation(req *types.ChatRequest, opt SafeCompactionOption) bool {
	if !opt.MidSessionRotation {
		// Completely disabled during normal IDE operation
		return false
	}
	if req == nil || len(req.Messages) <= 2 {
		return false
	}

	// Never touch or trim req.Tools
	// Preserve the most recent conversation turn containing active tool_use / tool_result
	lastIdx := len(req.Messages) - 1
	lastMsg := req.Messages[lastIdx]

	hasActiveToolPair := false
	if len(lastMsg.ToolCalls) > 0 || lastMsg.ToolCallID != "" {
		hasActiveToolPair = true
	}

	// Delegate older turns to ctxshrink
	var prefixMessages []types.ChatMessage
	if hasActiveToolPair && len(req.Messages) > 2 {
		prefixMessages = req.Messages[:lastIdx]
	} else {
		prefixMessages = req.Messages
	}

	shrinkReq := &types.ChatRequest{
		Messages: prefixMessages,
	}
	ctxshrink.CompactConversation(shrinkReq)

	if hasActiveToolPair {
		req.Messages = append(shrinkReq.Messages, lastMsg)
	} else {
		req.Messages = shrinkReq.Messages
	}

	return true
}
