package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"amux-accounts/pkg/tools"
	"amux-accounts/pkg/types"
)

// ModelInvoker sends requests to an LLM provider and receives streamed responses.
type ModelInvoker interface {
	SendMessageStream(ctx context.Context, req *types.ChatRequest) (<-chan types.StreamChunk, error)
}

// AgentLoopConfig configures the autonomous agent execution pipeline.
type AgentLoopConfig struct {
	MaxTurns      int           // Maximum iterative turns before giving up (default: 10)
	Timeout       time.Duration // Total timeout for the entire agent run (default: 5m)
	WorkspaceDir  string        // Working directory for tool executions
	Model         string        // Target model name or identifier
	SystemPrompt  string        // Optional custom system prompt
	ClientDialect string        // Dialect to emulate ("claude", "cursor", "codex", "gemini")
	Logger        func(format string, args ...any)
}

// AgentRunResult contains the consolidated outcome of an agent loop execution.
type AgentRunResult struct {
	FinalAnswer   string              `json:"final_answer"`
	Success       bool                `json:"success"`
	Turns         int                 `json:"turns"`
	Messages      []types.ChatMessage `json:"messages"`
	ExecutedCalls []ExecutionResult   `json:"executed_calls"`
	Duration      time.Duration       `json:"duration"`
	Error         error               `json:"error,omitempty"`
}

// RunAgentLoop executes the production-grade autonomous agent loop:
// User request -> Model tool call -> Parser -> Tool registry lookup -> Executor -> Tool result -> Conversation resume -> Final answer
func RunAgentLoop(
	ctx context.Context,
	invoker ModelInvoker,
	engine *ExecutionEngine,
	registry *Registry,
	userPrompt string,
	cfg AgentLoopConfig,
) (*AgentRunResult, error) {
	start := time.Now()

	if invoker == nil {
		return nil, errors.New("invoker cannot be nil")
	}

	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = 10
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Minute
	}
	if cfg.WorkspaceDir == "" {
		if cwd, err := os.Getwd(); err == nil {
			cfg.WorkspaceDir = cwd
		} else {
			cfg.WorkspaceDir = "."
		}
	}

	loopCtx := ctx
	var cancel context.CancelFunc
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		loopCtx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}

	if registry == nil {
		registry = NewDefaultRegistry(cfg.WorkspaceDir)
	}
	if engine == nil {
		manifest := FromToolDefs("NativeRuntime", registry.ToolDefs())
		engine = NewExecutionEngine(manifest, cfg.WorkspaceDir)
		engine.SetRegistry(registry)
	}

	// 1. Build initial conversation state
	toolDefs := registry.ToolDefs()
	manifest := FromToolDefs("NativeRuntime", toolDefs)
	var history []types.ChatMessage

	if strings.TrimSpace(cfg.SystemPrompt) != "" {
		history = append(history, types.ChatMessage{Role: "system", Content: cfg.SystemPrompt})
	} else {
		contract := BuildRuntimeContract(manifest)
		history = append(history, types.ChatMessage{Role: "system", Content: contract})
	}

	history = append(history, types.ChatMessage{Role: "user", Content: userPrompt})

	var executedCalls []ExecutionResult
	var lastAssistantText string

	// 2. Iterative execution loop
	for turn := 1; turn <= cfg.MaxTurns; turn++ {
		select {
		case <-loopCtx.Done():
			return &AgentRunResult{
				Turns:         turn,
				Messages:      history,
				ExecutedCalls: executedCalls,
				Duration:      time.Since(start),
				Error:         loopCtx.Err(),
			}, loopCtx.Err()
		default:
		}

		req := &types.ChatRequest{
			Model:         cfg.Model,
			Messages:      history,
			Tools:         toolDefs,
			Stream:        true,
			FullContext:   true,
			ClientDialect: cfg.ClientDialect,
		}

		ch, err := invoker.SendMessageStream(loopCtx, req)
		if err != nil {
			return &AgentRunResult{
				Turns:         turn,
				Messages:      history,
				ExecutedCalls: executedCalls,
				Duration:      time.Since(start),
				Error:         fmt.Errorf("turn %d send stream error: %w", turn, err),
			}, err
		}

		var textBuf strings.Builder
		var nativeToolCalls []types.ToolCall
		var streamErr error

		for chunk := range ch {
			if chunk.Error != nil {
				streamErr = chunk.Error
				break
			}
			if chunk.Content != "" {
				textBuf.WriteString(chunk.Content)
			}
			if len(chunk.ToolCalls) > 0 {
				nativeToolCalls = append(nativeToolCalls, chunk.ToolCalls...)
			}
		}

		if streamErr != nil {
			return &AgentRunResult{
				Turns:         turn,
				Messages:      history,
				ExecutedCalls: executedCalls,
				Duration:      time.Since(start),
				Error:         fmt.Errorf("turn %d chunk error: %w", turn, streamErr),
			}, streamErr
		}

		rawText := textBuf.String()
		lastAssistantText = rawText

		// 3. Extract tool calls (native first, then parse from markup)
		calls := nativeToolCalls
		if len(calls) == 0 {
			calls = tools.ParseWebTools(rawText, toolDefs, cfg.WorkspaceDir)
		}

		// 4. If no tool calls emitted, we have achieved the final answer!
		if len(calls) == 0 {
			cleanAnswer := strings.TrimSpace(tools.StripInternalThoughtAndToolTags(rawText))
			if cleanAnswer == "" {
				cleanAnswer = strings.TrimSpace(rawText)
			}
			history = append(history, types.ChatMessage{
				Role:    "assistant",
				Content: cleanAnswer,
			})
			return &AgentRunResult{
				FinalAnswer:   cleanAnswer,
				Success:       true,
				Turns:         turn,
				Messages:      history,
				ExecutedCalls: executedCalls,
				Duration:      time.Since(start),
			}, nil
		}

		// 5. Tool calls present: execute tools deterministically and resume conversation
		history = append(history, types.ChatMessage{
			Role:      "assistant",
			Content:   rawText,
			ToolCalls: calls,
		})

		for _, tc := range calls {
			if cfg.Logger != nil {
				cfg.Logger("Turn %d: Executing %s with args: %s", turn, tc.Name, tc.Arguments)
			}

			// Pre-lookup and normalize
			execRes, execErr := engine.Execute(loopCtx, tc)
			if execRes != nil {
				executedCalls = append(executedCalls, *execRes)
			}

			output := ""
			if execRes != nil && execRes.Output != "" {
				output = execRes.Output
			} else if execErr != nil {
				output = fmt.Sprintf("Error: %v", execErr)
			} else {
				output = "(tool completed with no output)"
			}

			// Format tool result message into conversation state
			toolCallID := tc.ID
			if toolCallID == "" {
				toolCallID = fmt.Sprintf("call_%d_%s", turn, tc.Name)
			}

			history = append(history, types.ChatMessage{
				Role:       "tool",
				ToolCallID: toolCallID,
				Content:    output,
			})
		}

		// Conversation automatically resumes in the next iteration of the loop!
	}

	cleanAnswer := strings.TrimSpace(tools.StripInternalThoughtAndToolTags(lastAssistantText))
	return &AgentRunResult{
		FinalAnswer:   cleanAnswer,
		Success:       false,
		Turns:         cfg.MaxTurns,
		Messages:      history,
		ExecutedCalls: executedCalls,
		Duration:      time.Since(start),
		Error:         fmt.Errorf("max turns reached (%d turns)", cfg.MaxTurns),
	}, nil
}
