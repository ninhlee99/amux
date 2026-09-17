package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"

	"amux-accounts/pkg/tools"
	"amux-accounts/pkg/types"
)

// nilParentMessageID is the ChatGPT web convention for the first turn of a
// new conversation (no prior assistant message to reply to).
const nilParentMessageID = "00000000-0000-0000-0000-000000000000"

type ChatGPTWebAdapter struct {
	AdapterID    string
	PriorityLvl  int
	SessionToken string
	TargetModel  string
	PlanTier     string // "plus" | "pro" | "team" | "free" | …
	HTTPClient   *http.Client

	mu      sync.Mutex
	convMgr *ProjectConversationManager
}

func (a *ChatGPTWebAdapter) convs() *ProjectConversationManager {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.convMgr == nil {
		a.convMgr = NewProjectConversationManager(a.AdapterID, DefaultMaxTurnsPerConversation, DefaultConversationTTL)
	}
	return a.convMgr
}

const chatGPTConversationURL = "https://chatgpt.com/backend-api/conversation"

func (a *ChatGPTWebAdapter) ID() string    { return a.AdapterID }
func (a *ChatGPTWebAdapter) Priority() int { return a.PriorityLvl }
func (a *ChatGPTWebAdapter) Plan() string  { return a.PlanTier }

// SupportsTools is false: ChatGPT web flattens to one text prompt and
// cannot emit Claude/OpenAI tool_use. Pool Send skips this adapter when
// the client sent tools[].
func (a *ChatGPTWebAdapter) SupportsTools() bool { return false }

func (a *ChatGPTWebAdapter) client() *http.Client {
	if a.HTTPClient != nil {
		return a.HTTPClient
	}
	return defaultHTTPClient
}

// BuildConcatenatedPrompt flattens a multi-turn ChatRequest into the single
// text blob the web adapters (ChatGPT, Claude web) send as one message.
// Multiple system messages are merged into one "[System Instructions]"
// block up front (rather than repeating the header per message) so the
// instructions read as one coherent block and nothing is dropped.
//
// Edge case: if messages contains only system message(s) and no user/
// assistant turns (e.g. a client sends a bare system prompt with an empty
// history), the output is still well-formed: "[System Instructions]\n<...>"
// followed by a trailing "Assistant:" cue with no history in between. This
// is intentional — it still gives the web adapter's chat UI a prompt to
// respond to instead of an empty string. See TestBuildConcatenatedPrompt_
// SystemOnly in config_test.go for the locked-in behavior.
func BuildConcatenatedPrompt(messages []types.ChatMessage) string {
	if len(messages) == 0 {
		return ""
	}
	if len(messages) == 1 && strings.EqualFold(messages[0].Role, "user") {
		return messages[0].Content
	}

	var sys strings.Builder
	var sb strings.Builder
	for idx, m := range messages {
		role := strings.ToLower(m.Role)
		switch role {
		case "system":
			if sys.Len() > 0 {
				sys.WriteString("\n\n")
			}
			sys.WriteString(m.Content)
		case "user":
			sb.WriteString("User: ")
			sb.WriteString(m.Content)
			sb.WriteString("\n\n")
		case "assistant":
			sb.WriteString("Assistant: ")
			sb.WriteString(m.Content)
			for _, tc := range m.ToolCalls {
				sb.WriteString("\n[Tool call: ")
				sb.WriteString(tc.Name)
				if tc.ID != "" {
					sb.WriteString(" id=")
					sb.WriteString(tc.ID)
				}
				sb.WriteString("]\n")
				sb.WriteString(tc.Arguments)
			}
			sb.WriteString("\n\n")
		case "tool":
			sb.WriteString("[Tool result — CLI ran]")
			if m.ToolCallID != "" {
				sb.WriteString(" (")
				sb.WriteString(m.ToolCallID)
				sb.WriteString(")")
			}
			sb.WriteString(":\n")
			content := m.Content
			// Keep last 2 tool results full; older ones hard-cap (token save).
			if idx < len(messages)-2 && len([]rune(content)) > 800 {
				r := []rune(content)
				content = string(r[:500]) + "\n... [truncated] ...\n" + string(r[len(r)-150:])
			}
			sb.WriteString(content)
			sb.WriteString("\n\n")
		default:
			title := role
			if len(title) > 0 {
				title = strings.ToUpper(title[:1]) + strings.ToLower(title[1:])
			}
			sb.WriteString(fmt.Sprintf("%s: %s\n\n", title, m.Content))
		}
	}

	var out strings.Builder
	if sys.Len() > 0 {
		out.WriteString("[System Instructions]\n")
		out.WriteString(sys.String())
		out.WriteString("\n\n")
	}
	out.WriteString(sb.String())
	out.WriteString("Assistant: ")
	return strings.TrimSpace(out.String())
}

func isChatGPTRateLimit(statusCode int, body string) bool {
	if statusCode == http.StatusTooManyRequests {
		return true
	}
	lower := strings.ToLower(body)
	return strings.Contains(lower, "rate_limit") ||
		strings.Contains(lower, "too_many_requests") ||
		strings.Contains(lower, "usage_limit") ||
		strings.Contains(lower, "reached our limit") ||
		strings.Contains(lower, "try again later")
}

func (a *ChatGPTWebAdapter) SendMessageStream(ctx context.Context, req *types.ChatRequest) (<-chan types.StreamChunk, error) {
	if a.SessionToken == "" {
		return nil, fmt.Errorf("%s: %w: no session token configured", a.AdapterID, types.ErrAuthentication)
	}

	model := a.TargetModel
	if model == "" {
		model = "auto"
	}

	accountID := chatgptAccountIDFromJWT(a.SessionToken)
	deviceID := newUUIDv4()
	sentinel, err := fetchChatGPTSentinel(ctx, a.client(), a.SessionToken, accountID, deviceID)
	if err != nil {
		return nil, fmt.Errorf("%s: sentinel: %w", a.AdapterID, err)
	}

	project := req.Project()
	cm := a.convs()
	rotatedConv := false
	for {
		activeConv, hasActive := cm.GetActive(project)
		var convID, parentID string
		if hasActive && activeConv != nil && !rotatedConv {
			convID = activeConv.ID
			parentID = activeConv.ParentID
		}

		if rotatedConv {
			cm.ResetProject(project)
			convID = ""
			parentID = ""
		}

		// Continuing a server-side thread: send only the latest user turn.
		// Fresh thread / FullContext: flatten history once into the first message.
		prompt := WebBackendPrompt(req, convID != "" && parentID != "")
		if parentID == "" {
			parentID = nilParentMessageID
		}

		messageID := newUUIDv4()
		payloadMap := map[string]any{
			"action": "next",
			"messages": []map[string]any{
				{
					"id":     messageID,
					"author": map[string]string{"role": "user"},
					"content": map[string]any{
						"content_type": "text",
						"parts":        []string{prompt},
					},
					"metadata": map[string]any{},
				},
			},
			"parent_message_id":             parentID,
			"model":                         model,
			"timezone_offset_min":           -420,
			"history_and_training_disabled": true,
			"conversation_mode":             map[string]string{"kind": "primary_assistant"},
		}
		if convID != "" {
			payloadMap["conversation_id"] = convID
		}

		b, err := json.Marshal(payloadMap)
		if err != nil {
			return nil, fmt.Errorf("%s: encode: %w", a.AdapterID, err)
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, chatGPTConversationURL, bytes.NewReader(b))
		if err != nil {
			return nil, fmt.Errorf("%s: build request: %w", a.AdapterID, err)
		}

		setChatGPTWebHeaders(httpReq, a.SessionToken, accountID, deviceID, true)
		httpReq.Header.Set("openai-sentinel-chat-requirements-token", sentinel.Requirements)
		if sentinel.Proof != "" {
			httpReq.Header.Set("openai-sentinel-proof-token", sentinel.Proof)
		}

		resp, err := a.client().Do(httpReq)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", a.AdapterID, err)
		}

		if resp.StatusCode == http.StatusUnauthorized {
			resp.Body.Close()
			return nil, types.ErrAuthentication
		}

		if isChatGPTRateLimit(resp.StatusCode, "") {
			resp.Body.Close()
			if !rotatedConv {
				rotatedConv = true
				cm.ResetProject(project)
				log.Printf("%s: rate limited on current thread for project %s — starting a new ChatGPT conversation", a.AdapterID, project)
				continue
			}
			return nil, types.ErrRateLimitReached
		}

		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
			resp.Body.Close()
			msg := string(bytes.TrimSpace(body))
			if isChatGPTRateLimit(resp.StatusCode, msg) {
				if !rotatedConv {
					rotatedConv = true
					cm.ResetProject(project)
					log.Printf("%s: rate limited on current thread for project %s — starting a new ChatGPT conversation", a.AdapterID, project)
					continue
				}
				return nil, fmt.Errorf("%s: %w: %s", a.AdapterID, types.ErrRateLimitReached, msg)
			}
			if resp.StatusCode == http.StatusNotFound {
				if !rotatedConv {
					rotatedConv = true
					cm.ResetProject(project)
					log.Printf("%s: conversation not found for project %s — starting a new ChatGPT conversation", a.AdapterID, project)
					continue
				}
			}
			return nil, fmt.Errorf("%s: upstream status %d: %s", a.AdapterID, resp.StatusCode, msg)
		}

		out := make(chan types.StreamChunk)
		go streamChatGPTWeb(ctx, a, project, req.SessionID, resp, out)
		return tools.MaybeWrapWebStream(a.AdapterID, req, out), nil
	}
}

// ResetConversation clears all server-side ChatGPT threads across all projects.
func (a *ChatGPTWebAdapter) ResetConversation() {
	a.convs().ResetAll()
}

// ResetConversationForScope clears the thread for a specific project.
func (a *ChatGPTWebAdapter) ResetConversationForScope(scopeKey string) {
	a.convs().ResetProject(scopeKey)
}

func streamChatGPTWeb(ctx context.Context, a *ChatGPTWebAdapter, project, sessionID string, resp *http.Response, out chan<- types.StreamChunk) {
	defer close(out)
	defer resp.Body.Close()

	id := a.AdapterID
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 2<<20)

	var lastText string
	var lastThought string
	var convID, msgID string
	doneSent := false
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			if convID != "" && msgID != "" {
				a.convs().Register(project, sessionID, convID, msgID, nil)
			}
			sendChunk(ctx, out, types.StreamChunk{ID: id, Done: true})
			doneSent = true
			return
		}

		var chunk struct {
			ConversationID string `json:"conversation_id"`
			Message        struct {
				ID     string `json:"id"`
				Author struct {
					Role string `json:"role"`
				} `json:"author"`
				Content struct {
					ContentType string   `json:"content_type"`
					Parts       []string `json:"parts"`
				} `json:"content"`
				Status string `json:"status"`
			} `json:"message"`
			Error any `json:"error"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if chunk.Error != nil {
			errStr := fmt.Sprintf("%v", chunk.Error)
			if isChatGPTRateLimit(0, errStr) {
				a.convs().ResetProject(project)
				sendChunk(ctx, out, types.StreamChunk{ID: id, Error: fmt.Errorf("%s: %w: %s", id, types.ErrRateLimitReached, errStr), Done: true})
			} else {
				sendChunk(ctx, out, types.StreamChunk{ID: id, Error: fmt.Errorf("%s: error: %v", id, chunk.Error), Done: true})
			}
			doneSent = true
			return
		}
		if chunk.ConversationID != "" {
			convID = chunk.ConversationID
		}

		if chunk.Message.Author.Role != "" && !strings.EqualFold(chunk.Message.Author.Role, "assistant") {
			continue
		}
		ctype := chunk.Message.Content.ContentType
		if ctype == "thought" {
			if len(chunk.Message.Content.Parts) > 0 {
				fullThought := chunk.Message.Content.Parts[0]
				if strings.HasPrefix(fullThought, lastThought) {
					delta := fullThought[len(lastThought):]
					lastThought = fullThought
					if delta != "" {
						if !sendChunk(ctx, out, types.StreamChunk{ID: id, Thinking: delta}) {
							return
						}
					}
				} else {
					lastThought = fullThought
					if !sendChunk(ctx, out, types.StreamChunk{ID: id, Thinking: fullThought}) {
						return
					}
				}
			}
			continue
		}
		if ctype != "" && ctype != "text" {
			continue
		}
		if chunk.Message.ID != "" {
			msgID = chunk.Message.ID
		}

		if len(chunk.Message.Content.Parts) > 0 {
			fullText := chunk.Message.Content.Parts[0]
			if strings.HasPrefix(fullText, lastText) {
				delta := fullText[len(lastText):]
				lastText = fullText
				if delta != "" {
					if !sendChunk(ctx, out, types.StreamChunk{ID: id, Content: delta}) {
						return
					}
				}
			} else {
				lastText = fullText
				if !sendChunk(ctx, out, types.StreamChunk{ID: id, Content: fullText}) {
					return
				}
			}
		}

		if chunk.Message.Status == "finished_successfully" && lastText != "" {
			if convID != "" && msgID != "" {
				a.convs().Register(project, sessionID, convID, msgID, nil)
			}
			sendChunk(ctx, out, types.StreamChunk{ID: id, Done: true})
			doneSent = true
			return
		}
	}
	if err := sc.Err(); err != nil && ctx.Err() == nil {
		sendChunk(ctx, out, types.StreamChunk{ID: id, Error: fmt.Errorf("%s: read stream: %w", id, err), Done: true})
		return
	}
	if convID != "" && msgID != "" {
		a.convs().Register(project, sessionID, convID, msgID, nil)
	}
	if !doneSent && ctx.Err() == nil {
		sendChunk(ctx, out, types.StreamChunk{ID: id, Done: true})
	}
}
