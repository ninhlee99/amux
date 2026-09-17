package types

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	// ErrRateLimitReached is returned by SendMessageStream when the
	// provider answered with a 429 (or an equivalent rate-limit signal).
	// The router treats it as "cool this adapter down and try the next
	// one" rather than a hard failure.
	ErrRateLimitReached = errors.New("rate limit reached or cooldown active")
	// ErrAuthentication is returned when the provider rejected the
	// request as unauthenticated/forbidden (bad API key, expired session
	// token, or an anti-bot challenge the adapter didn't try to solve).
	ErrAuthentication = errors.New("authentication failed")
)

// RateLimitError wraps ErrRateLimitReached with an optional Retry-After hint
// parsed from the upstream HTTP response. The router uses this to apply
// an adaptive cooldown duration instead of the fixed default.
type RateLimitError struct {
	RetryAfter time.Duration // 0 if not specified by upstream
	Msg        string
}

func (e *RateLimitError) Error() string {
	if e.Msg != "" {
		return e.Msg
	}
	if e.RetryAfter > 0 {
		return "rate limit reached, retry after " + e.RetryAfter.String()
	}
	return ErrRateLimitReached.Error()
}

func (e *RateLimitError) Is(target error) bool {
	return target == ErrRateLimitReached
}

// NewRateLimitError creates a RateLimitError. retryAfter=0 means no hint.
func NewRateLimitError(msg string, retryAfter time.Duration) *RateLimitError {
	return &RateLimitError{RetryAfter: retryAfter, Msg: msg}
}

// ExtractRetryAfter returns the RetryAfter hint from a RateLimitError, or 0.
func ExtractRetryAfter(err error) time.Duration {
	var rle *RateLimitError
	if errors.As(err, &rle) {
		return rle.RetryAfter
	}
	return 0
}

// ToolDef is a provider-agnostic tool/function declaration.
type ToolDef struct {
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"input_schema,omitempty"` // JSON Schema object
	CacheControl bool            `json:"cache_control,omitempty"`
}

// ToolCall is one model-requested tool invocation.
type ToolCall struct {
	ID               string `json:"id,omitempty"`
	Name             string `json:"name"`
	Arguments        string `json:"arguments,omitempty"` // JSON object as string
	ThoughtSignature string `json:"thought_signature,omitempty"`
}

// ChatMessage is one turn in a conversation.
//
// Roles:
//   - "system", "user", "assistant" — normal chat
//   - "tool" — tool result (OpenAI-shaped); set ToolCallID
//
// Assistant turns that invoke tools carry ToolCalls; Content may still hold
// any accompanying text.
type ChatMessage struct {
	Role         string     `json:"role"`
	Content      string     `json:"content,omitempty"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID   string     `json:"tool_call_id,omitempty"`
	Name         string     `json:"name,omitempty"`
	CacheControl bool       `json:"cache_control,omitempty"`
}

// ChatRequest is provider-agnostic; each adapter translates it into
// whatever wire format its backend expects.
type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Stream              bool          `json:"stream"`
	Temperature         float64       `json:"temperature,omitempty"`
	ExplicitTemperature bool          `json:"-"`
	MaxTokens           int           `json:"max_tokens,omitempty"`
	Tools       []ToolDef     `json:"tools,omitempty"`
	// ToolChoice mirrors OpenAI/Anthropic tool_choice when set ("auto",
	// "none", or a named tool). Adapters that don't support it ignore it.
	ToolChoice any `json:"tool_choice,omitempty"`
	// FullContext tells web backends to flatten the entire client history
	// into one prompt (Claude Code / Codex send full turns every request)
	// instead of only the last user message on a server-side thread.
	// Not serialized on the wire — set by the Anthropic/OpenAI bridges.
	FullContext bool `json:"-"`
	// ClientDialect hints which wire format the HTTP client spoke
	// ("anthropic", "openai", "gemini"). Used by bridges when emitting.
	ClientDialect string `json:"-"`
	// Thinking enables reasoning / extended thinking mode.
	Thinking bool `json:"thinking,omitempty"`
	// ThinkingBudget specifies max reasoning tokens (e.g. 1024, 2048).
	ThinkingBudget int `json:"thinking_budget,omitempty"`
	// ReasoningEffort mirrors OpenAI reasoning_effort ("low", "medium", "high").
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	// TargetTier requests a specific model class: "flash" (fast), "pro" (heavy/reasoning), or "" (auto).
	TargetTier string `json:"target_tier,omitempty"`
	// SessionID optionally identifies the multi-turn conversational session or thread.
	SessionID string `json:"session_id,omitempty"`
	// Metadata carries arbitrary client metadata passed along with the request.
	Metadata map[string]any `json:"metadata,omitempty"`
	// TaskKind is set by the router (not the client): coding | analysis | review |
	// compact | quality | fix | general. Soft-prefers account groups; never excludes.
	TaskKind string `json:"-"`
	// SystemCacheControl signals whether the system prompt should have an ephemeral cache breakpoint.
	SystemCacheControl bool `json:"system_cache_control,omitempty"`
	// ServingAccount tracks the actual account/adapter that served the request.
	ServingAccount string `json:"-"`
	// ServingAPI tracks the underlying API used (e.g. anthropic, gemini, openai, web).
	ServingAPI string `json:"-"`
	// ServingModel tracks the underlying model invoked.
	ServingModel string `json:"-"`
}

// Project returns the project directory / root if present in Metadata.
func (req *ChatRequest) Project() string {
	if req == nil || req.Metadata == nil {
		return ""
	}
	for _, k := range []string{"project", "cwd", "root", "workspace", "project_root"} {
		if v, ok := req.Metadata[k].(string); ok {
			if s := strings.TrimSpace(v); s != "" {
				return s
			}
		}
	}
	return ""
}

// ScopeKey returns a distinct key identifying the project + session combination
// to ensure complete isolation between different projects and sessions.
func (req *ChatRequest) ScopeKey() string {
	if req == nil {
		return "global"
	}
	proj := req.Project()
	sess := strings.TrimSpace(req.SessionID)
	if sess == "" && req.Metadata != nil {
		if s, ok := req.Metadata["session_id"].(string); ok {
			sess = strings.TrimSpace(s)
		} else if s, ok := req.Metadata["conversation_id"].(string); ok {
			sess = strings.TrimSpace(s)
		}
	}
	if proj != "" && sess != "" {
		return proj + "::" + sess
	}
	if proj != "" {
		return proj + "::default"
	}
	if sess != "" {
		return "unknown::" + sess
	}
	if len(req.Messages) > 0 {
		h := sha256.New()
		h.Write([]byte(req.Messages[0].Role))
		h.Write([]byte(":"))
		h.Write([]byte(req.Messages[0].Content))
		return "thread::" + hex.EncodeToString(h.Sum(nil))[:16]
	}
	return "global"
}

// UsageStats holds token usage and prompt caching metrics.
type UsageStats struct {
	InputTokens              int `json:"input_tokens,omitempty"`
	OutputTokens             int `json:"output_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
}

// StreamChunk is one piece of a streamed reply. The producer closes the
// channel after sending a chunk with Done set (or one carrying Error).
type StreamChunk struct {
	ID           string
	Content      string
	Thinking     string     // reasoning/thinking tokens emitted by the model
	ToolCalls    []ToolCall // set when the model requests tool use
	FinishReason string     // "stop", "tool_calls", "end_turn", ...
	Done         bool
	Error        error
	// LogText is the raw provider reply for request logging and error diagnosis (not sent to the client).
	LogText string
	// Usage carries token counts including cache read/creation stats from the upstream provider.
	Usage *UsageStats
}

// ProviderAdapter is implemented by every chat backend the router can
// dispatch to.
type ProviderAdapter interface {
	ID() string
	Priority() int // 1 = highest, 2, 3, ...
	SendMessageStream(ctx context.Context, req *ChatRequest) (<-chan StreamChunk, error)
}
