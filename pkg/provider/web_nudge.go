package provider

import (
	"context"
	"log"
	"strings"

	"amux-accounts/pkg/tools"
	"amux-accounts/pkg/types"
)

// webNudge is the user turn added when a web reply stopped without a
// <tool_call>: it restates the protocol and the task so the model acts.
const webNudge = "[next] Your last reply did not include a <tool_call>, so nothing ran. " +
	"You need no native tools: write the <tool_call> block as text and the client runs it. " +
	"Continue the task now with the next <tool_call>; give the final answer only when every requested step has a real [Tool result].\nTask: "

// sendWithWebNudge runs one web turn. When the client sent tools[] and the
// reply carries no real tool call but refuses the tools ("not available to me")
// or stalls ("I need to continue…"), the turn is re-sent once with webNudge
// instead of handing the client a dead end: Claude Code ends its turn on a
// text-only reply, so the user's remaining steps never run.
//
// Content is held back until the turn is decided; thinking streams through.
func sendWithWebNudge(ctx context.Context, req *types.ChatRequest, send func(context.Context, *types.ChatRequest) (<-chan types.StreamChunk, error)) (<-chan types.StreamChunk, error) {
	first, err := send(ctx, req)
	if err != nil || req == nil || len(req.Tools) == 0 {
		return first, err
	}
	out := make(chan types.StreamChunk, 8)
	go func() {
		defer close(out)
		var held []types.StreamChunk
		var text strings.Builder
		logText := ""
		realTools := false
		for ch := range first {
			if ch.Error != nil {
				for _, h := range held {
					out <- h
				}
				out <- ch
				for rest := range first {
					out <- rest
				}
				return
			}
			if ch.Thinking != "" && ch.Content == "" && len(ch.ToolCalls) == 0 && !ch.Done {
				out <- ch
				continue
			}
			if len(ch.ToolCalls) > 0 && !ch.ForcedTools {
				realTools = true
			}
			if ch.LogText != "" {
				logText = ch.LogText
			}
			text.WriteString(ch.Content)
			held = append(held, ch)
		}
		raw := logText
		if raw == "" {
			raw = text.String()
		}
		if !realTools && ctx.Err() == nil {
			kind := ""
			switch {
			case tools.IsToolRefusal(raw):
				kind = "refusal"
			case tools.IsToolStall(raw):
				kind = "stall"
			}
			if kind != "" {
				log.Printf("%s: web reply was a tool %s without <tool_call> — re-sending once with a nudge", req.ServingAccount, kind)
				if second, err := send(ctx, nudgedRequest(req, raw, kind)); err == nil {
					for ch := range second {
						out <- ch
					}
					return
				} else {
					log.Printf("%s: nudge retry failed: %v", req.ServingAccount, err)
				}
			}
		}
		for _, h := range held {
			out <- h
		}
	}()
	return out, nil
}

// nudgedRequest appends the nudge turn. A stall stays on the live thread (its
// text is kept as the assistant turn); a refusal's thread was reset by the
// adapter, and replaying the refusal would only argue for it, so it is dropped.
func nudgedRequest(req *types.ChatRequest, reply, kind string) *types.ChatRequest {
	cloned := *req
	n := len(req.Messages)
	if req.ClientMessages > 0 && req.ClientMessages < n {
		n = req.ClientMessages
	}
	msgs := make([]types.ChatMessage, 0, n+2)
	msgs = append(msgs, req.Messages[:n]...)
	if kind == "stall" {
		if visible := strings.TrimSpace(tools.StripWebToolMarkup(reply)); visible != "" {
			msgs = append(msgs, types.ChatMessage{Role: "assistant", Content: visible})
		}
	}
	msgs = append(msgs, types.ChatMessage{Role: "user", Content: webNudge + truncateRunes(currentUserTask(req.Messages[:n]), 800)})
	cloned.Messages = msgs
	cloned.ClientMessages = n
	return &cloned
}
