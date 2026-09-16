package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"amux-accounts/pkg/auth"
	"amux-accounts/pkg/guard"
	"amux-accounts/pkg/profile"
	"amux-accounts/pkg/tools"
	"amux-accounts/pkg/types"
)

var (
	claudeMessagesURL  = "https://api.anthropic.com/v1/messages"
	claudeDefaultModel = "claude-3-7-sonnet-20250219"
	anthropicVersion   = "2023-06-01"
	anthropicOAuthBeta = "oauth-2025-04-20"
)

// ClaudeAdapter connects amux to Anthropic's /v1/messages API using either
// an Anthropic API Key or an existing Claude OAuth token (from Claude Code CLI / Keychain).
// This enables non-Claude clients (Codex CLI, AGY, Cursor) to use Claude as a backend proxy
// with full native tool-calling support.
type ClaudeAdapter struct {
	AdapterID   string
	PriorityLvl int
	TargetModel string
	GroupLabel  string
	APIKey      string
	HTTPClient  *http.Client

	mu       sync.Mutex
	token    string
	refToken string
	expAt    time.Time
}

func (a *ClaudeAdapter) ID() string    { return a.AdapterID }
func (a *ClaudeAdapter) Priority() int { return a.PriorityLvl }
func (a *ClaudeAdapter) Group() string {
	if a.GroupLabel != "" {
		return a.GroupLabel
	}
	return "claude_sub"
}

// SupportsTools is true: Anthropic /v1/messages natively supports tool_use / tool_result.
func (a *ClaudeAdapter) SupportsTools() bool { return true }

func (a *ClaudeAdapter) client() *http.Client {
	if a.HTTPClient != nil {
		return a.HTTPClient
	}
	return defaultHTTPClient
}

// ClaudeAuthAvailable reports whether live Claude credentials (Keychain OAuth, active profile,
// or ANTHROPIC_API_KEY) are available on the machine.
func ClaudeAuthAvailable() bool {
	if tok := auth.LiveKeychainToken(); tok != nil && tok.Access != "" {
		return true
	}
	if active := profile.ReadActivePointer("claude"); active != "" {
		return true
	}
	if len(profile.ListProfiles("claude")) > 0 {
		return true
	}
	return strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")) != ""
}

func (a *ClaudeAdapter) ensureAuth(ctx context.Context) (token string, isOAuth bool, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// 1. Explicit adapter APIKey
	if a.APIKey != "" {
		return a.APIKey, false, nil
	}

	// 2. Cached live token
	if a.token != "" && (a.expAt.IsZero() || time.Now().Add(auth.RefreshLead).Before(a.expAt)) {
		return a.token, true, nil
	}

	// 3. Live Keychain token
	if tok := auth.LiveKeychainToken(); tok != nil && tok.Access != "" {
		if !tok.ExpiresAt.IsZero() && time.Now().Add(auth.RefreshLead).After(tok.ExpiresAt) && tok.Refresh != "" {
			resp, rerr := auth.RefreshClaudeToken(tok.Refresh)
			if rerr == nil && resp != nil && resp.AccessToken != "" {
				a.token = resp.AccessToken
				a.refToken = resp.RefreshToken
				if resp.ExpiresIn > 0 {
					a.expAt = time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
				}
				return a.token, true, nil
			}
		}
		a.token = tok.Access
		a.refToken = tok.Refresh
		a.expAt = tok.ExpiresAt
		return a.token, true, nil
	}

	// 4. Active profile snapshot
	active := profile.ReadActivePointer("claude")
	if active != "" {
		if tok := profile.LoadClaudeToken("claude", active); tok != nil && tok.Access != "" {
			a.token = tok.Access
			a.refToken = tok.Refresh
			a.expAt = tok.ExpiresAt
			return a.token, true, nil
		}
	}

	// 5. ANTHROPIC_API_KEY env fallback
	if envKey := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")); envKey != "" {
		return envKey, false, nil
	}

	return "", false, fmt.Errorf("%s: %w: no Claude credentials found", a.AdapterID, types.ErrAuthentication)
}

func (a *ClaudeAdapter) SendMessageStream(ctx context.Context, req *types.ChatRequest) (<-chan types.StreamChunk, error) {
	token, isOAuth, err := a.ensureAuth(ctx)
	if err != nil {
		return nil, err
	}

	model := claudeDefaultModel
	if a.TargetModel != "" {
		model = a.TargetModel
	}
	if req.Model != "" && req.Model != "default" && (strings.Contains(req.Model, "claude") || strings.Contains(req.Model, "sonnet") || strings.Contains(req.Model, "haiku") || strings.Contains(req.Model, "opus")) {
		model = req.Model
	}

	payload, err := tools.MarshalClaudeMessagesRequest(req, model)
	if err != nil {
		return nil, fmt.Errorf("%s: encode request: %w", a.AdapterID, err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, claudeMessagesURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("%s: build request: %w", a.AdapterID, err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	if isOAuth {
		httpReq.Header.Set("Authorization", "Bearer "+token)
		httpReq.Header.Set("anthropic-beta", "prompt-caching-2024-07-31,"+anthropicOAuthBeta)
	} else {
		httpReq.Header.Set("x-api-key", token)
		httpReq.Header.Set("anthropic-beta", "prompt-caching-2024-07-31")
	}

	resp, err := a.client().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", a.AdapterID, err)
	}

	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		retryAfter := guard.ParseRetryAfter(resp.Header)
		resp.Body.Close()
		return nil, types.NewRateLimitError(a.AdapterID+": rate limit", retryAfter)
	case http.StatusUnauthorized, http.StatusForbidden:
		resp.Body.Close()
		return nil, types.ErrAuthentication
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		resp.Body.Close()
		return nil, fmt.Errorf("%s: upstream status %d: %s", a.AdapterID, resp.StatusCode, bytes.TrimSpace(b))
	}

	out := make(chan types.StreamChunk)
	go streamClaudeSSE(ctx, a.AdapterID, resp, out)
	return out, nil
}

type anthropicEvent struct {
	Type         string `json:"type"`
	Index        int    `json:"index"`
	Message      *struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
	ContentBlock *struct {
		Type string `json:"type"` // "text", "thinking", "tool_use"
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`
	Delta *struct {
		Type        string `json:"type"` // "text_delta", "thinking_delta", "input_json_delta"
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Usage *struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func streamClaudeSSE(ctx context.Context, id string, resp *http.Response, out chan<- types.StreamChunk) {
	defer close(out)
	defer resp.Body.Close()

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 2<<20)

	type toolCallBuilder struct {
		id   string
		name string
		args strings.Builder
	}
	activeTools := map[int]*toolCallBuilder{}
	var completedCalls []types.ToolCall
	var usageStats types.UsageStats
	finishReason := "stop"
	doneSent := false

	finish := func() {
		if doneSent {
			return
		}
		// Flush any remaining active tool builders
		for _, tb := range activeTools {
			args := tb.args.String()
			if strings.TrimSpace(args) == "" {
				args = "{}"
			}
			completedCalls = append(completedCalls, types.ToolCall{
				ID:        tb.id,
				Name:      tb.name,
				Arguments: args,
			})
		}
		activeTools = map[int]*toolCallBuilder{}

		fr := finishReason
		if len(completedCalls) > 0 {
			fr = "tool_calls"
		}
		sendChunk(ctx, out, types.StreamChunk{
			ID:           id,
			ToolCalls:    completedCalls,
			FinishReason: fr,
			Usage:        &usageStats,
			Done:         true,
		})
		doneSent = true
	}

	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			if payload == "[DONE]" {
				finish()
				return
			}
			continue
		}

		var evt anthropicEvent
		if err := json.Unmarshal([]byte(payload), &evt); err != nil {
			continue
		}

		if evt.Error != nil {
			sendChunk(ctx, out, types.StreamChunk{
				ID:    id,
				Error: fmt.Errorf("%s: %s", evt.Error.Type, evt.Error.Message),
				Done:  true,
			})
			doneSent = true
			return
		}

		switch evt.Type {
		case "message_start":
			if evt.Message != nil && evt.Message.Usage != nil {
				usageStats.InputTokens = evt.Message.Usage.InputTokens
				usageStats.OutputTokens = evt.Message.Usage.OutputTokens
				usageStats.CacheCreationInputTokens = evt.Message.Usage.CacheCreationInputTokens
				usageStats.CacheReadInputTokens = evt.Message.Usage.CacheReadInputTokens
			}
		case "content_block_start":
			if evt.ContentBlock != nil && evt.ContentBlock.Type == "tool_use" {
				activeTools[evt.Index] = &toolCallBuilder{
					id:   evt.ContentBlock.ID,
					name: evt.ContentBlock.Name,
				}
			}
		case "content_block_delta":
			if evt.Delta != nil {
				switch evt.Delta.Type {
				case "text_delta":
					if evt.Delta.Text != "" {
						if !sendChunk(ctx, out, types.StreamChunk{ID: id, Content: evt.Delta.Text}) {
							return
						}
					}
				case "thinking_delta":
					if evt.Delta.Thinking != "" {
						if !sendChunk(ctx, out, types.StreamChunk{ID: id, Thinking: evt.Delta.Thinking}) {
							return
						}
					}
				case "input_json_delta":
					if tb, ok := activeTools[evt.Index]; ok && evt.Delta.PartialJSON != "" {
						tb.args.WriteString(evt.Delta.PartialJSON)
					}
				}
			}
		case "content_block_stop":
			if tb, ok := activeTools[evt.Index]; ok {
				args := tb.args.String()
				if strings.TrimSpace(args) == "" {
					args = "{}"
				}
				completedCalls = append(completedCalls, types.ToolCall{
					ID:        tb.id,
					Name:      tb.name,
					Arguments: args,
				})
				delete(activeTools, evt.Index)
			}
		case "message_delta":
			if evt.Delta != nil && evt.Delta.StopReason != "" {
				finishReason = evt.Delta.StopReason
			}
			if evt.Usage != nil && evt.Usage.OutputTokens > 0 {
				usageStats.OutputTokens = evt.Usage.OutputTokens
			}
		case "message_stop":
			finish()
			return
		}
	}

	if err := sc.Err(); err != nil && ctx.Err() == nil {
		sendChunk(ctx, out, types.StreamChunk{ID: id, Error: fmt.Errorf("%s: read stream: %w", id, err), Done: true})
		return
	}
	if !doneSent && ctx.Err() == nil {
		finish()
	}
}
