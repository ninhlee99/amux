package guard

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"amux-accounts/pkg/term"
	"amux-accounts/pkg/types"
)

// DetectAndPruneLoop checks if the request is stuck in a repetitive loop (e.g. 3+ identical consecutive tool calls
// or repeated empty/identical turns) and prunes redundant duplicate turns to save tokens and recover from the loop.
func DetectAndPruneLoop(req *types.ChatRequest) (bool, string) {
	if req == nil || len(req.Messages) < 6 {
		return false, ""
	}

	msgs := req.Messages
	type turnSig struct {
		role      string
		toolNames string
		argsHash  string
		content   string
	}

	sigs := make([]turnSig, len(msgs))
	for i, m := range msgs {
		role := strings.ToLower(m.Role)
		var tNames []string
		var argHashes []string
		for _, tc := range m.ToolCalls {
			tNames = append(tNames, tc.Name)
			h := sha256.Sum256([]byte(strings.TrimSpace(tc.Arguments)))
			argHashes = append(argHashes, hex.EncodeToString(h[:8]))
		}
		sigs[i] = turnSig{
			role:      role,
			toolNames: strings.Join(tNames, ","),
			argsHash:  strings.Join(argHashes, ","),
			content:   strings.TrimSpace(m.Content),
		}
	}

	n := len(sigs)
	for period := 2; period <= 4; period += 2 {
		if n < period*3 {
			continue
		}
		matchCount := 0
		pLast := n - period
		allMatch := true
		for rep := 1; rep <= 2; rep++ {
			pPrev := pLast - (rep * period)
			for offset := 0; offset < period; offset++ {
				s1 := sigs[pLast+offset]
				s2 := sigs[pPrev+offset]
				if s1.role != s2.role || s1.toolNames != s2.toolNames || s1.argsHash != s2.argsHash {
					allMatch = false
					break
				}
				if s1.role == "tool" || s1.toolNames != "" {
					// Tool call repetition matches
				} else if s1.content != "" && s1.content == s2.content {
					// Content repetition matches
				} else {
					allMatch = false
					break
				}
			}
			if !allMatch {
				break
			}
			matchCount++
		}

		if allMatch && matchCount >= 2 {
			toolDesc := sigs[pLast].toolNames
			if toolDesc == "" {
				toolDesc = "dialogue turn"
			}
			warningMsg := fmt.Sprintf("task loop detected: repeated %s (%d times)", toolDesc, matchCount+1)
			term.LogWarn("amux: %s! Pruning redundant loop context to prevent token burn.", warningMsg)

			keepBefore := n - (period * (matchCount + 1))
			keepAfter := n - period
			prunedMsgs := make([]types.ChatMessage, 0, keepBefore+1+period)
			prunedMsgs = append(prunedMsgs, msgs[:keepBefore+period]...)
			prunedMsgs = append(prunedMsgs, types.ChatMessage{
				Role:    "user",
				Content: fmt.Sprintf("[amux loop guard: %d repetitive identical turns pruned to save tokens and break execution loop. Please choose a different approach or proceed with alternative tools.]", period*matchCount),
			})
			prunedMsgs = append(prunedMsgs, msgs[keepAfter:]...)
			req.Messages = prunedMsgs
			return true, warningMsg
		}
	}

	return false, ""
}
