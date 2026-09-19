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
	"amux-accounts/pkg/tools"
	"amux-accounts/pkg/types"
)

type geminiPart struct {
	Text             string                    `json:"text,omitempty"`
	Thought          bool                      `json:"thought,omitempty"`
	ThoughtSignature string                    `json:"thoughtSignature,omitempty"`
	ThoughtSigSnake  string                    `json:"thought_signature,omitempty"`
	FunctionCall     *tools.GeminiFunctionCall `json:"functionCall,omitempty"`
	FunctionResponse *geminiFuncResponse       `json:"functionResponse,omitempty"`
}

type geminiFuncResponse struct {
	Name     string          `json:"name"`
	Response json.RawMessage `json:"response"`
}

type geminiContent struct {
	Role  string       `json:"role"`
	Parts []geminiPart `json:"parts"`
}

type geminiToolDeclaration struct {
	FunctionDeclarations []tools.GeminiFunctionDeclaration `json:"functionDeclarations,omitempty"`
}

type geminiGenerateRequest struct {
	Contents          []geminiContent         `json:"contents"`
	SystemInstruction *geminiContent          `json:"systemInstruction,omitempty"`
	Tools             []geminiToolDeclaration `json:"tools,omitempty"`
	GenerationConfig  *struct {
		Temperature     float64 `json:"temperature,omitempty"`
		MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
		ThinkingConfig  *struct {
			ThinkingBudget int `json:"thinkingBudget,omitempty"`
		} `json:"thinkingConfig,omitempty"`
	} `json:"generationConfig,omitempty"`
}

type geminiCandidateContent struct {
	Role  string       `json:"role"`
	Parts []geminiPart `json:"parts"`
}

type geminiCandidate struct {
	Content      geminiCandidateContent `json:"content"`
	FinishReason string                 `json:"finishReason,omitempty"`
	Index        int                    `json:"index"`
}

type geminiGenerateResponse struct {
	Candidates    []geminiCandidate `json:"candidates"`
	UsageMetadata *struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata,omitempty"`
}

func parseGeminiModelAndStream(path, query string) (model string, stream bool) {
	stream = strings.Contains(path, ":streamGenerateContent") || strings.Contains(query, "alt=sse")
	idx := strings.Index(path, "/models/")
	if idx != -1 {
		rest := path[idx+len("/models/"):]
		colon := strings.Index(rest, ":")
		if colon != -1 {
			model = rest[:colon]
		} else {
			model = rest
		}
	}
	if model == "" {
		model = "gemini-2.5-flash"
	}
	return model, stream
}

// GeminiBodyToChatRequest parses a Gemini generateContent request body into canonical ChatRequest.
func GeminiBodyToChatRequest(model string, stream bool, body []byte) (*types.ChatRequest, error) {
	return geminiBodyToChatRequest(model, stream, body)
}

func geminiBodyToChatRequest(model string, stream bool, body []byte) (*types.ChatRequest, error) {
	var gReq geminiGenerateRequest
	if err := json.Unmarshal(body, &gReq); err != nil {
		return nil, fmt.Errorf("unmarshal gemini request: %w", err)
	}

	req := &types.ChatRequest{
		Model:         model,
		Stream:        stream,
		ClientDialect: tools.DialectGemini,
		FullContext:   true,
		Messages:      make([]types.ChatMessage, 0, len(gReq.Contents)+1),
	}
	if gReq.GenerationConfig != nil {
		req.Temperature = gReq.GenerationConfig.Temperature
		if gReq.GenerationConfig.ThinkingConfig != nil {
			req.Thinking = true
			req.ThinkingBudget = gReq.GenerationConfig.ThinkingConfig.ThinkingBudget
		}
	}

	if gReq.SystemInstruction != nil {
		var sb strings.Builder
		for _, p := range gReq.SystemInstruction.Parts {
			if p.Text != "" {
				if sb.Len() > 0 {
					sb.WriteString("\n")
				}
				sb.WriteString(p.Text)
			}
		}
		if sb.Len() > 0 {
			req.Messages = append(req.Messages, types.ChatMessage{
				Role:    "system",
				Content: sb.String(),
			})
		}
	}

	for _, t := range gReq.Tools {
		if len(t.FunctionDeclarations) > 0 {
			req.Tools = append(req.Tools, tools.FromGeminiFunctions(t.FunctionDeclarations)...)
		}
	}

	claimedToolCallIDs := make(map[string]bool)
	for _, m := range req.Messages {
		if strings.EqualFold(m.Role, "tool") && m.ToolCallID != "" {
			claimedToolCallIDs[m.ToolCallID] = true
		}
	}

	for _, c := range gReq.Contents {
		role := strings.ToLower(c.Role)
		switch role {
		case "model":
			role = "assistant"
		case "user":
			role = "user"
		default:
			if role == "" {
				role = "user"
			}
		}

		var text strings.Builder
		var calls []types.ToolCall
		for _, p := range c.Parts {
			if p.Text != "" {
				if text.Len() > 0 {
					text.WriteString("\n")
				}
				text.WriteString(p.Text)
			}
			if p.FunctionCall != nil {
				args := "{}"
				if len(p.FunctionCall.Args) > 0 && string(p.FunctionCall.Args) != "null" {
					args = string(p.FunctionCall.Args)
				}
				sig := p.ThoughtSignature
				if sig == "" {
					sig = p.ThoughtSigSnake
				}
				if sig == "" {
					sig = p.FunctionCall.ThoughtSignature
				}
				if sig == "" {
					sig = p.FunctionCall.ThoughtSigSnake
				}
				callID := fmt.Sprintf("call_%s_%d_%d", p.FunctionCall.Name, time.Now().UnixNano(), len(calls)+1)
				if sig != "" {
					tools.RecordThoughtSignature(callID, sig)
				}
				calls = append(calls, types.ToolCall{
					ID:               callID,
					Name:             p.FunctionCall.Name,
					Arguments:        args,
					ThoughtSignature: sig,
				})
			}
			if p.FunctionResponse != nil {
				respStr := string(p.FunctionResponse.Response)
				toolCallID := p.FunctionResponse.Name
				for i := len(req.Messages) - 1; i >= 0; i-- {
					if strings.EqualFold(req.Messages[i].Role, "assistant") {
						for _, tc := range req.Messages[i].ToolCalls {
							if tc.Name == p.FunctionResponse.Name && tc.ID != "" && !claimedToolCallIDs[tc.ID] {
								toolCallID = tc.ID
								claimedToolCallIDs[tc.ID] = true
								break
							}
						}
						if toolCallID != p.FunctionResponse.Name {
							break
						}
					}
				}
				req.Messages = append(req.Messages, types.ChatMessage{
					Role:       "tool",
					Name:       p.FunctionResponse.Name,
					ToolCallID: toolCallID,
					Content:    respStr,
				})
			}
		}

		if text.Len() > 0 || len(calls) > 0 {
			req.Messages = append(req.Messages, types.ChatMessage{
				Role:      role,
				Content:   text.String(),
				ToolCalls: calls,
			})
		}
	}

	return req, nil
}

// HandleGeminiGenerateContent handles Antigravity and Gemini SDK requests.
func HandleGeminiGenerateContent(w http.ResponseWriter, r *http.Request, pool *router.AccountPoolRouter) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	model, stream := parseGeminiModelAndStream(r.URL.Path, r.URL.RawQuery)
	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	if privacy.Enabled {
		if redacted, res := privacy.RedactBytes(body); res.Len() > 0 {
			body = redacted
			privacy.LogHits(r, res, tools.DialectGemini)
		}
	}

	req, err := geminiBodyToChatRequest(model, stream, body)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid json: %v", err), http.StatusBadRequest)
		return
	}

	// Session switch detection & compact for Gemini / Antigravity clients
	targetAccount := pool.Preferred()
	if targetAccount == "" {
		targetAccount = "pool"
	}
	if switched, _ := guard.CheckSessionAccountSwitch(r, req, targetAccount); switched {
		if len(req.Messages) > 4 {
			req.Messages = ctxshrink.CompactForAccountSwitchProject(req.Project(), req.Messages, 6)
		}
	} else {
		// Run global deduplication on historical tool results
		req.Messages = ctxshrink.GlobalDeduplicator().DeduplicateMessages(req.Project(), req.Messages, 2)
	}

	var initialFlusher http.Flusher
	if req.Stream {
		var ok bool
		initialFlusher, ok = beginGeminiSSE(w)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
	}

	ctx := r.Context()
	started := time.Now()
	var streamChan <-chan types.StreamChunk
	if req.Stream {
		streamChan, err = poolSendStreaming(w, r, pool, req, initialFlusher, func() {})
	} else {
		streamChan, err = poolSend(r, pool, req)
	}
	if err != nil {
		logChatRequest(r, pool, req, "", "", err.Error(), 0, 0, started, nil)
		if req.Stream {
			writeGeminiStreamError(w, initialFlusher, err)
			return
		}
		http.Error(w, fmt.Sprintf("all providers failed: %v", err), http.StatusBadGateway)
		return
	}

	if req.Stream {
		flusher := initialFlusher

		var fullContent strings.Builder
		var toolCalls []types.ToolCall
		var logText string
		finishReason := "STOP"
		ping := time.NewTicker(streamKeepaliveInterval)
		defer ping.Stop()

		for {
			chunk, ok, recvErr := recvStreamChunk(ctx, streamChan, ping.C, nil)
			if recvErr != nil {
				return
			}
			if !ok {
				break
			}
			if chunk.Error != nil {
				writeGeminiStreamError(w, flusher, chunk.Error)
				return
			}
			if chunk.LogText != "" {
				logText = chunk.LogText
			}
			if chunk.Thinking != "" {
				chunkResp := geminiGenerateResponse{
					Candidates: []geminiCandidate{{
						Index: 0,
						Content: geminiCandidateContent{
							Role:  "model",
							Parts: []geminiPart{{Text: chunk.Thinking, Thought: true}},
						},
					}},
				}
				b, _ := json.Marshal(chunkResp)
				fmt.Fprintf(w, "data: %s\n\n", b)
				flusher.Flush()
			}
			if chunk.Content != "" {
				fullContent.WriteString(chunk.Content)
				chunkResp := geminiGenerateResponse{
					Candidates: []geminiCandidate{{
						Index: 0,
						Content: geminiCandidateContent{
							Role:  "model",
							Parts: []geminiPart{{Text: chunk.Content}},
						},
					}},
				}
				b, _ := json.Marshal(chunkResp)
				fmt.Fprintf(w, "data: %s\n\n", b)
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
					toolCalls = tools.NormalizeToolCalls(toolCalls, req.Tools, tools.DialectGemini)
					geminiCalls := tools.ToGeminiFunctionCalls(toolCalls)
					parts := make([]geminiPart, 0, len(geminiCalls))
					for _, gc := range geminiCalls {
						cCopy := gc
						parts = append(parts, geminiPart{
							FunctionCall:     &cCopy,
							ThoughtSignature: gc.ThoughtSignature,
						})
					}
					chunkResp := geminiGenerateResponse{
						Candidates: []geminiCandidate{{
							Index: 0,
							Content: geminiCandidateContent{
								Role:  "model",
								Parts: parts,
							},
							FinishReason: "STOP",
						}},
					}
					b, _ := json.Marshal(chunkResp)
					fmt.Fprintf(w, "data: %s\n\n", b)
					flusher.Flush()
				}
				break
			}
		}

		inTokens := estimateInputTokens(req)
		outTokens := estimateStringTokens(fullContent.String())
		logChatRequest(r, pool, req, pickLogOutput(fullContent.String(), logText), finishReason, "", inTokens, outTokens, started, toolCalls)
		return
	}

	var fullContent strings.Builder
	var thinkingContent strings.Builder
	var toolCalls []types.ToolCall
	var logText string
	finishReason := "STOP"

	for chunk := range streamChan {
		if chunk.Error != nil {
			http.Error(w, chunk.Error.Error(), http.StatusBadGateway)
			return
		}
		if chunk.Thinking != "" {
			thinkingContent.WriteString(chunk.Thinking)
		}
		fullContent.WriteString(chunk.Content)
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

	parts := make([]geminiPart, 0)
	if thinkingContent.Len() > 0 {
		parts = append(parts, geminiPart{Text: thinkingContent.String(), Thought: true})
	}
	if len(toolCalls) == 0 && fullContent.Len() > 0 && len(req.Tools) > 0 {
		if parsed, _ := tools.FinalizeWebToolCalls(fullContent.String(), req.Tools, req.Messages); len(parsed) > 0 {
			toolCalls = parsed
		}
	}
	if len(toolCalls) > 0 {
		toolCalls = tools.NormalizeToolCalls(toolCalls, req.Tools, tools.DialectGemini)
	}
	if fullContent.Len() > 0 {
		text := fullContent.String()
		if len(toolCalls) > 0 {
			text = tools.StripWebToolMarkup(text)
		}
		if strings.TrimSpace(text) != "" {
			parts = append(parts, geminiPart{Text: text})
		}
	}
	for _, tc := range toolCalls {
		rawArgs := json.RawMessage(tc.Arguments)
		if len(rawArgs) == 0 {
			rawArgs = json.RawMessage(`{}`)
		}
		sig := tc.ThoughtSignature
		if sig == "" && tc.ID != "" {
			sig = tools.LookupThoughtSignature(tc.ID)
		}
		parts = append(parts, geminiPart{
			FunctionCall: &tools.GeminiFunctionCall{
				Name:             tc.Name,
				Args:             rawArgs,
				ThoughtSignature: sig,
			},
			ThoughtSignature: sig,
		})
	}
	if len(parts) == 0 {
		parts = append(parts, geminiPart{Text: ""})
	}

	inTokens := estimateInputTokens(req)
	outTokens := estimateStringTokens(fullContent.String()) + estimateStringTokens(thinkingContent.String())
	respObj := geminiGenerateResponse{
		Candidates: []geminiCandidate{
			{
				Index: 0,
				Content: geminiCandidateContent{
					Role:  "model",
					Parts: parts,
				},
				FinishReason: finishReason,
			},
		},
		UsageMetadata: &struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
			TotalTokenCount      int `json:"totalTokenCount"`
		}{
			PromptTokenCount:     inTokens,
			CandidatesTokenCount: outTokens,
			TotalTokenCount:      inTokens + outTokens,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(respObj)
	logChatRequest(r, pool, req, pickLogOutput(fullContent.String(), logText), finishReason, "", inTokens, outTokens, started, toolCalls)
}

// HandleGeminiCountTokens handles /models/...:countTokens requests for Antigravity & Gemini SDKs.
func HandleGeminiCountTokens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	model, _ := parseGeminiModelAndStream(r.URL.Path, r.URL.RawQuery)
	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	req, err := geminiBodyToChatRequest(model, false, body)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid json: %v", err), http.StatusBadRequest)
		return
	}
	tokens := estimateInputTokens(req)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{
		"totalTokens": tokens,
	})
}

// HandleGeminiModels lists available models in Google Gemini API format.
func HandleGeminiModels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	models := []string{
		"gemini-3.8-flash",
		"gemini-3.8-flash-high", "gemini-3.8-flash-medium", "gemini-3.8-flash-low",
		"gemini-3.7-flash-high", "gemini-3.7-flash-medium", "gemini-3.7-flash-low",
		"gemini-3.6-flash", "gemini-2.5-pro", "gemini-2.5-flash", "gemini-2.5-flash-lite",
		"gemini-3.1-pro-preview",
	}
	type geminiModelItem struct {
		Name                       string   `json:"name"`
		DisplayName                string   `json:"displayName,omitempty"`
		InputTokenLimit            int      `json:"inputTokenLimit,omitempty"`
		OutputTokenLimit           int      `json:"outputTokenLimit,omitempty"`
		SupportedGenerationMethods []string `json:"supportedGenerationMethods,omitempty"`
	}
	items := make([]geminiModelItem, 0, len(models))
	for _, m := range models {
		items = append(items, geminiModelItem{
			Name:                       "models/" + m,
			DisplayName:                m,
			InputTokenLimit:            1048576,
			OutputTokenLimit:           8192,
			SupportedGenerationMethods: []string{"generateContent", "countTokens"},
		})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"models": items,
	})
}

func writeGeminiStreamError(w http.ResponseWriter, flusher http.Flusher, err error) {
	payload, _ := json.Marshal(map[string]any{
		"error": map[string]any{
			"code":    502,
			"message": err.Error(),
			"status":  "UNAVAILABLE",
		},
	})
	fmt.Fprintf(w, "data: %s\n\n", payload)
	flusher.Flush()
}

// beginGeminiSSE commits the SSE response headers without emitting SSE comments.
// google.golang.org/genai (used by agy) does not support SSE comment lines (e.g. ": ...")
// and fails with "iterateResponseStream: invalid stream chunk: : <comment>".
func beginGeminiSSE(w http.ResponseWriter) (http.Flusher, bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	flusher.Flush()
	return flusher, true
}

