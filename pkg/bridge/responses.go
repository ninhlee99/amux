package bridge

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"amux-accounts/pkg/privacy"
	"amux-accounts/pkg/router"
	"amux-accounts/pkg/tools"
	"amux-accounts/pkg/types"
)

// HandleOpenAIResponses handles OpenAI Responses API (/v1/responses) requests
// used by modern Codex CLI and agent environments.
func HandleOpenAIResponses(w http.ResponseWriter, r *http.Request, pool *router.AccountPoolRouter) {
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

	req, err := responsesBodyToChatRequest(body)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid json: %v", err), http.StatusBadRequest)
		return
	}

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
			writeResponsesStreamError(w, initialFlusher, err, "")
			return
		}
		http.Error(w, fmt.Sprintf("all providers failed: %v", err), http.StatusBadGateway)
		return
	}

	respID := fmt.Sprintf("resp_%d", time.Now().UnixNano())
	now := time.Now().Unix()
	inputTokens := estimateInputTokens(req)

	if req.Stream {
		flusher := initialFlusher

		// Initial response.created event
		createdJSON, _ := json.Marshal(map[string]any{
			"type": "response.created",
			"response": map[string]any{
				"id":         respID,
				"object":     "response",
				"created_at": now,
				"status":     "in_progress",
				"model":      req.Model,
			},
		})
		fmt.Fprintf(w, "event: response.created\ndata: %s\n\n", createdJSON)
		flusher.Flush()

		var fullContent strings.Builder
		var toolCalls []types.ToolCall
		var logText string
		finishReason := "stop"
		textPartStarted := false
		itemID := fmt.Sprintf("item_%d", time.Now().UnixNano())
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
				writeResponsesStreamError(w, flusher, chunk.Error, respID)
				return
			}

			if chunk.LogText != "" {
				logText = chunk.LogText
			}

			if chunk.Content != "" {
				fullContent.WriteString(chunk.Content)

				if !textPartStarted {
					textPartStarted = true
					// Emit output_item.added and content_part.added
					itemAddJSON, _ := json.Marshal(map[string]any{
						"type":         "response.output_item.added",
						"output_index": 0,
						"item": map[string]any{
							"id":      itemID,
							"type":    "message",
							"status":  "in_progress",
							"role":    "assistant",
							"content": []any{},
						},
					})
					fmt.Fprintf(w, "event: response.output_item.added\ndata: %s\n\n", itemAddJSON)

					partAddJSON, _ := json.Marshal(map[string]any{
						"type":          "response.content_part.added",
						"output_index":  0,
						"content_index": 0,
						"part": map[string]string{
							"type": "text",
							"text": "",
						},
					})
					fmt.Fprintf(w, "event: response.content_part.added\ndata: %s\n\n", partAddJSON)
				}

				deltaJSON, _ := json.Marshal(map[string]any{
					"type":          "response.output_text.delta",
					"output_index":  0,
					"content_index": 0,
					"delta":         chunk.Content,
				})
				fmt.Fprintf(w, "event: response.output_text.delta\ndata: %s\n\n", deltaJSON)
				flusher.Flush()
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

		// If tool calls were accumulated (or extracted from text prompt for web loops)
		if len(toolCalls) == 0 && fullContent.Len() > 0 && len(req.Tools) > 0 {
			if parsed := tools.ParseWebTools(fullContent.String(), req.Tools); len(parsed) > 0 {
				toolCalls = parsed
			}
		}

		outputIndex := 0
		if textPartStarted {
			// Finish text part
			textDoneJSON, _ := json.Marshal(map[string]any{
				"type":          "response.output_text.done",
				"output_index":  0,
				"content_index": 0,
				"text":          fullContent.String(),
			})
			fmt.Fprintf(w, "event: response.output_text.done\ndata: %s\n\n", textDoneJSON)

			itemDoneJSON, _ := json.Marshal(map[string]any{
				"type":         "response.output_item.done",
				"output_index": 0,
				"item": map[string]any{
					"id":     itemID,
					"type":   "message",
					"status": "completed",
					"role":   "assistant",
					"content": []map[string]string{
						{"type": "text", "text": fullContent.String()},
					},
				},
			})
			fmt.Fprintf(w, "event: response.output_item.done\ndata: %s\n\n", itemDoneJSON)
			outputIndex++
		}

		// Emit tool call output items
		for _, tc := range toolCalls {
			finishReason = "tool_calls"
			callID := tc.ID
			if callID == "" {
				callID = fmt.Sprintf("call_%d", time.Now().UnixNano())
			}

			toolItemJSON, _ := json.Marshal(map[string]any{
				"type":         "response.output_item.added",
				"output_index": outputIndex,
				"item": map[string]any{
					"id":        callID,
					"type":      "function_call",
					"status":    "in_progress",
					"name":      tc.Name,
					"call_id":   callID,
					"arguments": "",
				},
			})
			fmt.Fprintf(w, "event: response.output_item.added\ndata: %s\n\n", toolItemJSON)

			argsDeltaJSON, _ := json.Marshal(map[string]any{
				"type":         "response.function_call_arguments.delta",
				"output_index": outputIndex,
				"call_id":      callID,
				"delta":        tc.Arguments,
			})
			fmt.Fprintf(w, "event: response.function_call_arguments.delta\ndata: %s\n\n", argsDeltaJSON)

			argsDoneJSON, _ := json.Marshal(map[string]any{
				"type":         "response.function_call_arguments.done",
				"output_index": outputIndex,
				"call_id":      callID,
				"arguments":    tc.Arguments,
			})
			fmt.Fprintf(w, "event: response.function_call_arguments.done\ndata: %s\n\n", argsDoneJSON)

			toolItemDoneJSON, _ := json.Marshal(map[string]any{
				"type":         "response.output_item.done",
				"output_index": outputIndex,
				"item": map[string]any{
					"id":        callID,
					"type":      "function_call",
					"status":    "completed",
					"name":      tc.Name,
					"call_id":   callID,
					"arguments": tc.Arguments,
				},
			})
			fmt.Fprintf(w, "event: response.output_item.done\ndata: %s\n\n", toolItemDoneJSON)
			outputIndex++
		}

		// Completed event
		completedJSON, _ := json.Marshal(map[string]any{
			"type": "response.completed",
			"response": map[string]any{
				"id":         respID,
				"object":     "response",
				"created_at": now,
				"status":     "completed",
				"model":      req.Model,
			},
		})
		fmt.Fprintf(w, "event: response.completed\ndata: %s\n\n", completedJSON)
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()

		completionTokens := estimateStringTokens(fullContent.String())
		recordChatUsage(r, pool, req.Model, inputTokens, completionTokens)
		logChatRequest(r, pool, req, pickLogOutput(fullContent.String(), logText), finishReason, "", inputTokens, completionTokens, started, toolCalls)
		return
	}

	// Non-streaming Responses API
	var fullContent strings.Builder
	var toolCalls []types.ToolCall
	var logText string
	finishReason := "stop"

	for chunk := range stream {
		if chunk.Error != nil {
			logChatRequest(r, pool, req, "", "", chunk.Error.Error(), 0, 0, started, nil)
			http.Error(w, chunk.Error.Error(), http.StatusBadGateway)
			return
		}
		if chunk.LogText != "" {
			logText = chunk.LogText
		}
		if chunk.Content != "" {
			fullContent.WriteString(chunk.Content)
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

	if len(toolCalls) == 0 && fullContent.Len() > 0 && len(req.Tools) > 0 {
		if parsed := tools.ParseWebTools(fullContent.String(), req.Tools); len(parsed) > 0 {
			toolCalls = parsed
		}
	}

	var outputItems []map[string]any
	if fullContent.Len() > 0 {
		outputItems = append(outputItems, map[string]any{
			"id":     fmt.Sprintf("item_%d", time.Now().UnixNano()),
			"type":   "message",
			"status": "completed",
			"role":   "assistant",
			"content": []map[string]string{
				{"type": "text", "text": fullContent.String()},
			},
		})
	}
	for _, tc := range toolCalls {
		finishReason = "tool_calls"
		callID := tc.ID
		if callID == "" {
			callID = fmt.Sprintf("call_%d", time.Now().UnixNano())
		}
		outputItems = append(outputItems, map[string]any{
			"id":        callID,
			"type":      "function_call",
			"status":    "completed",
			"name":      tc.Name,
			"call_id":   callID,
			"arguments": tc.Arguments,
		})
	}

	completionTokens := estimateStringTokens(fullContent.String())
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{
		"id":         respID,
		"object":     "response",
		"created_at": now,
		"status":     "completed",
		"model":      req.Model,
		"output":     outputItems,
		"usage": map[string]int{
			"input_tokens":  inputTokens,
			"output_tokens": completionTokens,
			"total_tokens":  inputTokens + completionTokens,
		},
	}
	_ = json.NewEncoder(w).Encode(resp)
	recordChatUsage(r, pool, req.Model, inputTokens, completionTokens)
	logChatRequest(r, pool, req, pickLogOutput(fullContent.String(), logText), finishReason, "", inputTokens, completionTokens, started, toolCalls)
}

// responsesBodyToChatRequest parses an OpenAI Responses API body into a canonical ChatRequest.
func responsesBodyToChatRequest(body []byte) (*types.ChatRequest, error) {
	var wrap struct {
		Model        string          `json:"model"`
		Input        json.RawMessage `json:"input"`
		Messages     json.RawMessage `json:"messages"`
		Instructions string          `json:"instructions"`
		Stream       bool            `json:"stream"`
		Temperature  float64         `json:"temperature"`
		ToolChoice   any             `json:"tool_choice"`
		Tools        json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(body, &wrap); err != nil {
		return nil, err
	}

	model := wrap.Model
	if model == "" {
		model = "gpt-5-codex"
	}

	req := &types.ChatRequest{
		Model:         model,
		Stream:        wrap.Stream,
		Temperature:   wrap.Temperature,
		ToolChoice:    wrap.ToolChoice,
		FullContext:   true,
		ClientDialect: tools.DialectCodex,
	}

	if wrap.Instructions != "" {
		req.Messages = append(req.Messages, types.ChatMessage{
			Role:    "system",
			Content: wrap.Instructions,
		})
	}

	// 1. Parse Tools
	if len(wrap.Tools) > 0 && string(wrap.Tools) != "null" {
		req.Tools = parseResponsesTools(wrap.Tools)
	}

	// 2. Parse Input or Messages
	if len(wrap.Input) > 0 && string(wrap.Input) != "null" {
		msgs, err := parseResponsesInput(wrap.Input)
		if err == nil {
			req.Messages = append(req.Messages, msgs...)
		}
	} else if len(wrap.Messages) > 0 && string(wrap.Messages) != "null" {
		// Fallback to standard messages array if input was omitted
		var rawMsgs []struct {
			Role       string                 `json:"role"`
			Content    json.RawMessage        `json:"content"`
			Name       string                 `json:"name"`
			ToolCallID string                 `json:"tool_call_id"`
			ToolCalls  []tools.OpenAIToolCall `json:"tool_calls"`
		}
		if err := json.Unmarshal(wrap.Messages, &rawMsgs); err == nil {
			for _, m := range rawMsgs {
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
		}
	}

	return req, nil
}

func parseResponsesInput(raw json.RawMessage) ([]types.ChatMessage, error) {
	// Case 1: Simple string prompt
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return []types.ChatMessage{{Role: "user", Content: s}}, nil
	}

	// Case 2: Array of input items / conversation turns
	var items []map[string]any
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}

	var msgs []types.ChatMessage
	for _, item := range items {
		itemType, _ := item["type"].(string)

		switch itemType {
		case "function_call_output":
			callID, _ := item["call_id"].(string)
			output, _ := item["output"].(string)
			msgs = append(msgs, types.ChatMessage{
				Role:       "tool",
				ToolCallID: callID,
				Content:    output,
			})

		case "function_call":
			callID, _ := item["call_id"].(string)
			name, _ := item["name"].(string)
			args, _ := item["arguments"].(string)
			msgs = append(msgs, types.ChatMessage{
				Role: "assistant",
				ToolCalls: []types.ToolCall{
					{ID: callID, Name: name, Arguments: args},
				},
			})

		default:
			// Regular message item
			role, _ := item["role"].(string)
			if role == "" {
				role = "user"
			}
			contentStr := ""
			if c, ok := item["content"].(string); ok {
				contentStr = c
			} else if parts, ok := item["content"].([]any); ok {
				var sb strings.Builder
				for _, p := range parts {
					if pm, ok := p.(map[string]any); ok {
						if t, _ := pm["type"].(string); t == "text" || t == "input_text" {
							if text, _ := pm["text"].(string); text != "" {
								if sb.Len() > 0 {
									sb.WriteByte('\n')
								}
								sb.WriteString(text)
							}
						}
					}
				}
				contentStr = sb.String()
			}

			msgs = append(msgs, types.ChatMessage{
				Role:    role,
				Content: contentStr,
			})
		}
	}

	return msgs, nil
}

func parseResponsesTools(raw json.RawMessage) []types.ToolDef {
	var list []map[string]any
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil
	}

	var out []types.ToolDef
	for _, t := range list {
		// Responses API format: {"type": "function", "name": "...", "description": "...", "parameters": {...}}
		// OR Chat Completions format: {"type": "function", "function": {"name": "...", ...}}
		name, _ := t["name"].(string)
		desc, _ := t["description"].(string)
		var params json.RawMessage

		if fn, ok := t["function"].(map[string]any); ok {
			if fnName, ok := fn["name"].(string); ok && fnName != "" {
				name = fnName
			}
			if fnDesc, ok := fn["description"].(string); ok && fnDesc != "" {
				desc = fnDesc
			}
			if p, ok := fn["parameters"]; ok {
				params, _ = json.Marshal(p)
			}
		} else if p, ok := t["parameters"]; ok {
			params, _ = json.Marshal(p)
		}

		if name != "" {
			out = append(out, types.ToolDef{
				Name:        name,
				Description: desc,
				InputSchema: params,
			})
		}
	}
	return out
}

func writeResponsesStreamError(w http.ResponseWriter, flusher http.Flusher, err error, respID string) {
	resp := map[string]any{
		"status": "failed",
		"error":  map[string]string{"message": err.Error()},
	}
	if respID != "" {
		resp["id"] = respID
	}
	errJSON, _ := json.Marshal(map[string]any{"type": "response.failed", "response": resp})
	fmt.Fprintf(w, "event: response.failed\ndata: %s\n\n", errJSON)
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}
