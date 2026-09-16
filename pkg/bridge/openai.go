package bridge

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"amux-accounts/pkg/ctxshrink"
	"amux-accounts/pkg/guard"
	"amux-accounts/pkg/privacy"
	"amux-accounts/pkg/router"
	"amux-accounts/pkg/term"
	"amux-accounts/pkg/tools"
	"amux-accounts/pkg/types"
	"amux-accounts/pkg/usage"
)

// HandleChatCompletions handles standard OpenAI /v1/chat/completions requests
// from Cursor, Codex, and other OpenAI-shaped clients. Tools are normalized
// through pkg/tools so the same pool adapters serve Claude Code and Cursor.
func HandleChatCompletions(w http.ResponseWriter, r *http.Request, pool *router.AccountPoolRouter) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	if privacy.Enabled {
		if redacted, res := privacy.RedactBytes(body); res.Len() > 0 {
			body = redacted
			privacy.LogHits(r, res, "openai")
		}
	}

	req, err := openAIBodyToChatRequest(body)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid json: %v", err), http.StatusBadRequest)
		return
	}
	req.ClientDialect = openaiClientDialect(r)

	// Session switch detection & compact for OpenAI clients (Cursor/Codex)
	targetAccount := pool.Preferred()
	if targetAccount == "" {
		targetAccount = "pool"
	}
	if switched, _ := guard.CheckSessionAccountSwitch(r, req, targetAccount); switched {
		if len(req.Messages) > 4 {
			req.Messages = ctxshrink.CompactForAccountSwitch(req.Messages, 6)
		}
	} else {
		// Run global deduplication on historical tool results
		req.Messages = ctxshrink.GlobalDeduplicator().DeduplicateMessages(req.Messages, 2)
	}

	replayKey, canReplay := ctxshrink.GlobalReplayCache().ComputeHash(req)
	if canReplay {
		if cached, found := ctxshrink.GlobalReplayCache().Get(replayKey); found {
			recordChatUsage(r, pool, req.Model, 0, cached.OutputTokens, cached.InputTokens)
			logChatRequest(r, pool, req, "[cached replay]", "stop", "", 0, cached.OutputTokens, time.Now(), nil)
			term.LogProxy("⚡ Deterministic Replay Cache HIT [key=%s] (0 upstream tokens, saved %d tokens, 0$)",
				replayKey[:8], cached.InputTokens)
			_ = cached.Serve(w, req.Stream)
			return
		}
	}

	rec := ctxshrink.NewRecordingWriter(w)
	w = rec

	var initialFlusher http.Flusher
	if req.Stream {
		var ok bool
		initialFlusher, ok = beginSSE(w)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
	}

	ctx := r.Context()
	started := time.Now()
	var stream <-chan types.StreamChunk
	if req.Stream {
		stream, err = poolSendStreaming(w, r, pool, req, initialFlusher, nil)
	} else {
		stream, err = poolSend(r, pool, req)
	}
	if err != nil {
		logChatRequest(r, pool, req, "", "", err.Error(), 0, 0, started, nil)
		if req.Stream {
			writeOpenAIStreamError(w, initialFlusher, err)
			return
		}
		http.Error(w, fmt.Sprintf("all providers failed: %v", err), http.StatusBadGateway)
		return
	}

	cmplID := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	now := time.Now().Unix()
	inputTokens := estimateInputTokens(req)

	if req.Stream {
		flusher := initialFlusher

		var fullContent strings.Builder
		var toolCalls []types.ToolCall
		var logText string
		var finalUsage *types.UsageStats
		finishReason := "stop"
		stopSent := false
		ping := time.NewTicker(streamKeepaliveInterval)
		defer ping.Stop()
		for {
			chunk, ok, recvErr := recvStreamChunk(ctx, stream, ping.C, commentKeepalive(w, flusher))
			if recvErr != nil {
				return
			}
			if !ok {
				break
			}
			if chunk.Error != nil {
				writeOpenAIStreamError(w, flusher, chunk.Error)
				return
			}

			if chunk.Usage != nil {
				finalUsage = chunk.Usage
			}
			if chunk.LogText != "" {
				logText = chunk.LogText
			}
			if chunk.Thinking != "" {
				chunkJSON, _ := json.Marshal(map[string]any{
					"id":      cmplID,
					"object":  "chat.completion.chunk",
					"created": now,
					"model":   req.Model,
					"choices": []map[string]any{{
						"index": 0,
						"delta": map[string]string{
							"reasoning_content": chunk.Thinking,
						},
						"finish_reason": nil,
					}},
				})
				fmt.Fprintf(w, "data: %s\n\n", chunkJSON)
				flusher.Flush()
			}
			if chunk.Content != "" {
				fullContent.WriteString(chunk.Content)
				chunkJSON, _ := json.Marshal(map[string]any{
					"id":      cmplID,
					"object":  "chat.completion.chunk",
					"created": now,
					"model":   req.Model,
					"choices": []map[string]any{{
						"index": 0,
						"delta": map[string]string{
							"content": chunk.Content,
						},
						"finish_reason": nil,
					}},
				})
				fmt.Fprintf(w, "data: %s\n\n", chunkJSON)
				flusher.Flush()
			}
			if len(chunk.ToolCalls) > 0 {
				toolCalls = append(toolCalls, chunk.ToolCalls...)
			}
			if chunk.FinishReason != "" {
				finishReason = chunk.FinishReason
			}
			if chunk.Done {
				if len(toolCalls) == 0 && fullContent.Len() > 0 && len(req.Tools) > 0 {
					if parsed, _ := tools.FinalizeWebToolCalls(fullContent.String(), req.Tools, req.Messages); len(parsed) > 0 {
						toolCalls = parsed
					}
				}
				if len(toolCalls) > 0 {
					finishReason = "tool_calls"
					oaCalls := tools.ToOpenAIToolCalls(toolCalls)
					for i, oc := range oaCalls {
						delta := map[string]any{
							"tool_calls": []map[string]any{{
								"index": i,
								"id":    oc.ID,
								"type":  "function",
								"function": map[string]string{
									"name":      oc.Function.Name,
									"arguments": oc.Function.Arguments,
								},
							}},
						}
						chunkJSON, _ := json.Marshal(map[string]any{
							"id":      cmplID,
							"object":  "chat.completion.chunk",
							"created": now,
							"model":   req.Model,
							"choices": []map[string]any{{
								"index":         0,
								"delta":         delta,
								"finish_reason": nil,
							}},
						})
						fmt.Fprintf(w, "data: %s\n\n", chunkJSON)
						flusher.Flush()
					}
				}
				stopJSON, _ := json.Marshal(map[string]any{
					"id":      cmplID,
					"object":  "chat.completion.chunk",
					"created": now,
					"model":   req.Model,
					"choices": []map[string]any{{
						"index":         0,
						"delta":         map[string]string{},
						"finish_reason": finishReason,
					}},
				})
				fmt.Fprintf(w, "data: %s\n\n", stopJSON)
				flusher.Flush()
				stopSent = true
				break
			}
		}

		if !stopSent && ctx.Err() == nil {
			stopJSON, _ := json.Marshal(map[string]any{
				"id":      cmplID,
				"object":  "chat.completion.chunk",
				"created": now,
				"model":   req.Model,
				"choices": []map[string]any{{
					"index":         0,
					"delta":         map[string]string{},
					"finish_reason": "stop",
				}},
			})
			fmt.Fprintf(w, "data: %s\n\n", stopJSON)
			flusher.Flush()
		}

		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
		outTok := estimateStringTokens(fullContent.String())
		cachedTokens := 0
		if finalUsage != nil {
			if finalUsage.InputTokens > 0 {
				inputTokens = finalUsage.InputTokens
			}
			if finalUsage.OutputTokens > 0 {
				outTok = finalUsage.OutputTokens
			}
			cachedTokens = finalUsage.CacheReadInputTokens
		}
		recordChatUsage(r, pool, req.Model, inputTokens, outTok, cachedTokens)
		if canReplay && len(rec.Events()) > 0 {
			ctxshrink.GlobalReplayCache().Put(replayKey, &ctxshrink.CachedReplay{
				ContentType:  "text/event-stream",
				SSEEvents:    rec.Events(),
				InputTokens:  inputTokens,
				OutputTokens: outTok,
			})
		}
		logChatRequest(r, pool, req, pickLogOutput(fullContent.String(), logText), finishReason, "", inputTokens, outTok, started, toolCalls)
		return
	}

	// Non-streaming
	var full strings.Builder
	var thinking strings.Builder
	var toolCalls []types.ToolCall
	var logText string
	var finalUsage *types.UsageStats
	finishReason := "stop"
	for chunk := range stream {
		if chunk.Error != nil {
			http.Error(w, chunk.Error.Error(), http.StatusBadGateway)
			return
		}
		if chunk.Usage != nil {
			finalUsage = chunk.Usage
		}
		if chunk.Thinking != "" {
			thinking.WriteString(chunk.Thinking)
		}
		full.WriteString(chunk.Content)
		if chunk.LogText != "" {
			logText = chunk.LogText
		}
		if len(chunk.ToolCalls) > 0 {
			toolCalls = append(toolCalls, chunk.ToolCalls...)
		}
		if chunk.FinishReason != "" {
			finishReason = chunk.FinishReason
		}
		if chunk.Done {
			break
		}
	}

	if len(toolCalls) == 0 && full.Len() > 0 && len(req.Tools) > 0 {
		if parsed, _ := tools.FinalizeWebToolCalls(full.String(), req.Tools, req.Messages); len(parsed) > 0 {
			toolCalls = parsed
		}
	}
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}

	completionTokens := estimateStringTokens(full.String()) + estimateStringTokens(thinking.String())
	cachedTokens := 0
	if finalUsage != nil {
		if finalUsage.InputTokens > 0 {
			inputTokens = finalUsage.InputTokens
		}
		if finalUsage.OutputTokens > 0 {
			completionTokens = finalUsage.OutputTokens
		}
		cachedTokens = finalUsage.CacheReadInputTokens
	}
	msg := map[string]any{
		"role":    "assistant",
		"content": full.String(),
	}
	if thinking.Len() > 0 {
		msg["reasoning_content"] = thinking.String()
	}
	if len(toolCalls) > 0 {
		msg["tool_calls"] = tools.ToOpenAIToolCalls(toolCalls)
		if full.Len() == 0 {
			msg["content"] = nil
		}
	}

	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{
		"id":      cmplID,
		"object":  "chat.completion",
		"created": now,
		"model":   req.Model,
		"choices": []map[string]any{{
			"index":         0,
			"message":       msg,
			"finish_reason": finishReason,
		}},
		"usage": map[string]int{
			"prompt_tokens":     inputTokens,
			"completion_tokens": completionTokens,
			"total_tokens":      inputTokens + completionTokens,
		},
	}
	_ = json.NewEncoder(w).Encode(resp)
	if canReplay && len(rec.Body()) > 0 {
		ctxshrink.GlobalReplayCache().Put(replayKey, &ctxshrink.CachedReplay{
			ContentType:  "application/json",
			Body:         rec.Body(),
			InputTokens:  inputTokens,
			OutputTokens: completionTokens,
		})
	}
	recordChatUsage(r, pool, req.Model, inputTokens, completionTokens, cachedTokens)
	logChatRequest(r, pool, req, pickLogOutput(full.String(), logText), finishReason, "", inputTokens, completionTokens, started, toolCalls)
}

// openaiClientDialect maps OpenAI-shaped clients onto pool IDE order.
// /v1/chat/completions is shared; Cursor stays Cursor, Codex UA/originator
// must not inherit Claude-first GroupPriority.
func openaiClientDialect(r *http.Request) string {
	if r == nil {
		return tools.DialectCursor
	}
	ua := strings.ToLower(r.Header.Get("User-Agent"))
	if strings.Contains(ua, "codex") {
		return tools.DialectCodex
	}
	if strings.Contains(strings.ToLower(r.Header.Get("originator")), "codex") {
		return tools.DialectCodex
	}
	return tools.DialectCursor
}

// openAIBodyToChatRequest parses a Cursor/Codex OpenAI chat.completions body
// into the canonical ChatRequest (tools use function.parameters on the wire).
func openAIBodyToChatRequest(body []byte) (*types.ChatRequest, error) {
	var wrap struct {
		Model               string   `json:"model"`
		Stream              bool     `json:"stream"`
		Temperature         *float64 `json:"temperature"`
		MaxTokens           int      `json:"max_tokens"`
		MaxCompletionTokens int      `json:"max_completion_tokens"`
		ToolChoice          any      `json:"tool_choice"`
		Messages            []struct {
			Role       string                 `json:"role"`
			Content    json.RawMessage        `json:"content"`
			Name       string                 `json:"name"`
			ToolCallID string                 `json:"tool_call_id"`
			ToolCalls  []tools.OpenAIToolCall `json:"tool_calls"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &wrap); err != nil {
		return nil, err
	}

	temp := 0.0
	explicitTemp := false
	if wrap.Temperature != nil {
		temp = *wrap.Temperature
		explicitTemp = true
	}

	maxTok := wrap.MaxTokens
	if wrap.MaxCompletionTokens > 0 {
		maxTok = wrap.MaxCompletionTokens
	}

	req := &types.ChatRequest{
		Model:               wrap.Model,
		Stream:              wrap.Stream,
		Temperature:         temp,
		ExplicitTemperature: explicitTemp,
		MaxTokens:           maxTok,
		ToolChoice:          wrap.ToolChoice,
		FullContext:         true,
		ClientDialect:       tools.DialectCursor,
	}
	if tools, err := tools.ParseCursorTools(body); err == nil {
		req.Tools = tools
	}

	for _, m := range wrap.Messages {
		msg := types.ChatMessage{
			Role:       m.Role,
			Name:       m.Name,
			ToolCallID: m.ToolCallID,
			Content:    openAIContentString(m.Content),
		}
		if len(m.ToolCalls) > 0 {
			msg.ToolCalls = tools.FromOpenAIToolCalls(m.ToolCalls)
		}
		req.Messages = append(req.Messages, msg)
	}
	return req, nil
}

func openAIContentString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	// Multimodal content array — keep text parts only.
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err == nil {
		var sb strings.Builder
		for _, p := range parts {
			if p.Type == "text" && p.Text != "" {
				if sb.Len() > 0 {
					sb.WriteByte('\n')
				}
				sb.WriteString(p.Text)
			}
		}
		return sb.String()
	}
	return strings.TrimSpace(string(raw))
}

func recordChatUsage(r *http.Request, pool *router.AccountPoolRouter, model string, input, output, cacheRead int) {
	if input == 0 && output == 0 && cacheRead == 0 {
		return
	}
	usage.AppendUsageEntry(types.UsageEntry{
		Time:      time.Now(),
		Account:   poolAccountLabel(pool),
		Model:     model,
		Project:   usage.ProjectForRemoteAddr(r.RemoteAddr),
		Session:   r.Header.Get("X-Claude-Code-Session-Id"),
		Input:     input,
		Output:    output,
		CacheRead: cacheRead,
	})
}

// HandleModels returns standard models list (Anthropic format if anthropic-version header present, else OpenAI format).
func HandleModels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Header.Get("anthropic-version") != "" || strings.Contains(r.Header.Get("User-Agent"), "Claude") {
		anthropicModels := []map[string]any{
			{"type": "model", "id": "claude-3-7-sonnet-20250219", "display_name": "Claude 3.7 Sonnet", "created_at": "2025-02-19T00:00:00Z"},
			{"type": "model", "id": "claude-3-5-sonnet-20241022", "display_name": "Claude 3.5 Sonnet", "created_at": "2024-10-22T00:00:00Z"},
			{"type": "model", "id": "claude-3-5-haiku-20241022", "display_name": "Claude 3.5 Haiku", "created_at": "2024-10-22T00:00:00Z"},
			{"type": "model", "id": "claude-3-opus-20240229", "display_name": "Claude 3 Opus", "created_at": "2024-02-29T00:00:00Z"},
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":     anthropicModels,
			"has_more": false,
			"first_id": "claude-3-7-sonnet-20250219",
			"last_id":  "claude-3-opus-20240229",
		})
		return
	}

	models := []string{
		"gpt-4o", "gpt-4o-mini", "o1", "o1-mini",
		"gemini-3.8-flash", "gemini-3.6-flash", "gemini-2.5-pro", "gemini-2.5-flash",
		"claude-3-5-sonnet-20241022", "claude-3-haiku-20240307",
		"llama-3.3-70b-versatile", "mixtral-8x7b-32768",
	}
	data := make([]map[string]any, 0, len(models))
	for _, m := range models {
		data = append(data, map[string]any{
			"id":       m,
			"object":   "model",
			"created":  1700000000,
			"owned_by": "amux-accounts",
		})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"object": "list",
		"data":   data,
	})
}

func writeOpenAIStreamError(w http.ResponseWriter, flusher http.Flusher, err error) {
	fmt.Fprintf(w, "data: {\"error\":{\"message\":%q}}\n\n", err.Error())
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}
