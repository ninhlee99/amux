package runtime_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"amux-accounts/pkg/runtime"
	"amux-accounts/pkg/types"
)

// mockModelInvoker simulates iterative turns of model responses.
type mockModelInvoker struct {
	turnResponses []types.StreamChunk
	currentTurn   int
	capturedReqs  []*types.ChatRequest
}

func (m *mockModelInvoker) SendMessageStream(ctx context.Context, req *types.ChatRequest) (<-chan types.StreamChunk, error) {
	m.capturedReqs = append(m.capturedReqs, req)
	ch := make(chan types.StreamChunk, 2)

	idx := m.currentTurn
	m.currentTurn++

	go func() {
		defer close(ch)
		if idx < len(m.turnResponses) {
			ch <- m.turnResponses[idx]
		} else {
			ch <- types.StreamChunk{
				Done:    true,
				Content: "Default final answer: all tasks completed.",
			}
		}
	}()

	return ch, nil
}

// ============================================================================
// TestAgentLoop_FullFlow_NativeToolCall:
// User request -> Model tool call -> Registry lookup -> Executor -> Tool result -> Resume -> Final answer
// ============================================================================
func TestAgentLoop_FullFlow_NativeToolCall(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "sample.txt")
	if err := os.WriteFile(testFile, []byte("production agent test line 1\nline 2\n"), 0644); err != nil {
		t.Fatal(err)
	}

	invoker := &mockModelInvoker{
		turnResponses: []types.StreamChunk{
			// Turn 1: Model requests native ToolCall to read file
			{
				ToolCalls: []types.ToolCall{
					{
						ID:        "call_native_read_1",
						Name:      "Read",
						Arguments: fmt.Sprintf(`{"file_path": %q}`, testFile),
					},
				},
				FinishReason: "tool_calls",
			},
			// Turn 2: Model resumes from tool result and provides final answer
			{
				Content:      "The file contains 2 lines: production agent test line 1 and line 2.",
				FinishReason: "stop",
				Done:         true,
			},
		},
	}

	registry := runtime.NewDefaultRegistry(tmpDir)
	engine := runtime.NewExecutionEngine(nil, tmpDir)
	engine.SetRegistry(registry)

	cfg := runtime.AgentLoopConfig{
		MaxTurns:     5,
		WorkspaceDir: tmpDir,
		Model:        "claude-3-7-sonnet",
	}

	result, err := runtime.RunAgentLoop(
		context.Background(),
		invoker,
		engine,
		registry,
		"Please read the sample file and summarize it.",
		cfg,
	)

	if err != nil {
		t.Fatalf("unexpected agent loop error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got failure")
	}
	if result.Turns != 2 {
		t.Fatalf("expected 2 turns, got %d", result.Turns)
	}
	if !strings.Contains(result.FinalAnswer, "production agent test line 1") {
		t.Fatalf("unexpected final answer: %s", result.FinalAnswer)
	}
	if len(result.ExecutedCalls) != 1 {
		t.Fatalf("expected 1 executed call, got %d", len(result.ExecutedCalls))
	}
	if result.ExecutedCalls[0].ToolName != "Read" {
		t.Fatalf("expected tool Read, got %s", result.ExecutedCalls[0].ToolName)
	}

	// Verify conversation history captured
	if len(result.Messages) < 4 {
		t.Fatalf("expected at least 4 messages (system, user, assistant, tool, assistant), got %d", len(result.Messages))
	}
	lastToolMsg := result.Messages[3]
	if lastToolMsg.Role != "tool" || !strings.Contains(lastToolMsg.Content, "production agent test") {
		t.Fatalf("expected tool message with output, got %+v", lastToolMsg)
	}
}

// ============================================================================
// TestAgentLoop_FullFlow_ParsedMarkupToolCall:
// Model outputs <tool_call> markup -> Parser -> Registry -> Executor -> Tool result -> Resume -> Final answer
// ============================================================================
func TestAgentLoop_FullFlow_ParsedMarkupToolCall(t *testing.T) {
	tmpDir := t.TempDir()

	invoker := &mockModelInvoker{
		turnResponses: []types.StreamChunk{
			// Turn 1: Model outputs markup <tool_call> for Bash command
			{
				Content: `<thought>I need to check the current date</thought>
<tool_call>
{"name":"Bash","arguments":{"command":"echo 'AGENT_DATE_OK'"}}
</tool_call>`,
				FinishReason: "stop",
			},
			// Turn 2: Model resumes from tool result and outputs final answer
			{
				Content:      "The current execution succeeded: AGENT_DATE_OK.",
				FinishReason: "stop",
				Done:         true,
			},
		},
	}

	registry := runtime.NewDefaultRegistry(tmpDir)
	engine := runtime.NewExecutionEngine(nil, tmpDir)
	engine.SetRegistry(registry)

	result, err := runtime.RunAgentLoop(
		context.Background(),
		invoker,
		engine,
		registry,
		"Run the date check script",
		runtime.AgentLoopConfig{MaxTurns: 5, WorkspaceDir: tmpDir},
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success")
	}
	if result.Turns != 2 {
		t.Fatalf("expected 2 turns, got %d", result.Turns)
	}
	if !strings.Contains(result.FinalAnswer, "AGENT_DATE_OK") {
		t.Fatalf("unexpected final answer: %s", result.FinalAnswer)
	}
	if len(result.ExecutedCalls) != 1 {
		t.Fatalf("expected 1 executed call, got %d", len(result.ExecutedCalls))
	}
	if strings.TrimSpace(result.ExecutedCalls[0].Stdout) != "AGENT_DATE_OK" {
		t.Fatalf("expected stdout 'AGENT_DATE_OK', got %q", result.ExecutedCalls[0].Stdout)
	}
}

// ============================================================================
// TestAgentLoop_MultiTurn_WriteEditRead:
// 3 turns of sequential tool operations before final answer
// ============================================================================
func TestAgentLoop_MultiTurn_WriteEditRead(t *testing.T) {
	tmpDir := t.TempDir()
	targetFile := filepath.Join(tmpDir, "workflow_doc.txt")

	invoker := &mockModelInvoker{
		turnResponses: []types.StreamChunk{
			// Turn 1: Write file
			{
				ToolCalls: []types.ToolCall{
					{
						ID:        "call_write",
						Name:      "Write",
						Arguments: fmt.Sprintf(`{"file_path":%q,"content":"Hello World\nVersion 1.0\n"}`, targetFile),
					},
				},
				FinishReason: "tool_calls",
			},
			// Turn 2: Edit file
			{
				ToolCalls: []types.ToolCall{
					{
						ID:        "call_edit",
						Name:      "Edit",
						Arguments: fmt.Sprintf(`{"file_path":%q,"old_string":"Version 1.0","new_string":"Version 2.0"}`, targetFile),
					},
				},
				FinishReason: "tool_calls",
			},
			// Turn 3: Read file
			{
				ToolCalls: []types.ToolCall{
					{
						ID:        "call_read",
						Name:      "Read",
						Arguments: fmt.Sprintf(`{"file_path":%q}`, targetFile),
					},
				},
				FinishReason: "tool_calls",
			},
			// Turn 4: Final answer
			{
				Content:      "File created and upgraded to Version 2.0 successfully.",
				FinishReason: "stop",
				Done:         true,
			},
		},
	}

	registry := runtime.NewDefaultRegistry(tmpDir)
	engine := runtime.NewExecutionEngine(nil, tmpDir)
	engine.SetRegistry(registry)

	result, err := runtime.RunAgentLoop(
		context.Background(),
		invoker,
		engine,
		registry,
		"Create workflow_doc.txt and update it to Version 2.0",
		runtime.AgentLoopConfig{MaxTurns: 6, WorkspaceDir: tmpDir},
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success")
	}
	if result.Turns != 4 {
		t.Fatalf("expected 4 turns, got %d", result.Turns)
	}
	if len(result.ExecutedCalls) != 3 {
		t.Fatalf("expected 3 executed calls, got %d", len(result.ExecutedCalls))
	}

	// Verify file content on disk
	data, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Version 2.0") {
		t.Fatalf("expected 'Version 2.0' in file, got: %s", string(data))
	}
}

// ============================================================================
// TestAgentLoop_ToolFailureAndModelRecovery:
// Tool fails -> error sent to model -> model retries with correct parameters -> success
// ============================================================================
func TestAgentLoop_ToolFailureAndModelRecovery(t *testing.T) {
	tmpDir := t.TempDir()

	invoker := &mockModelInvoker{
		turnResponses: []types.StreamChunk{
			// Turn 1: Model tries to read nonexistent file
			{
				ToolCalls: []types.ToolCall{
					{
						ID:        "call_fail_1",
						Name:      "Read",
						Arguments: `{"file_path":"does_not_exist_xyz.txt"}`,
					},
				},
				FinishReason: "tool_calls",
			},
			// Turn 2: Model sees error, recovers by creating file and reading it
			{
				ToolCalls: []types.ToolCall{
					{
						ID:        "call_recover_write",
						Name:      "Write",
						Arguments: fmt.Sprintf(`{"file_path":%q,"content":"recovered content"}`, filepath.Join(tmpDir, "recovered.txt")),
					},
				},
				FinishReason: "tool_calls",
			},
			// Turn 3: Final answer after recovery
			{
				Content:      "File was missing, so I created it with recovered content.",
				FinishReason: "stop",
				Done:         true,
			},
		},
	}

	registry := runtime.NewDefaultRegistry(tmpDir)
	engine := runtime.NewExecutionEngine(nil, tmpDir)
	engine.SetRegistry(registry)

	result, err := runtime.RunAgentLoop(
		context.Background(),
		invoker,
		engine,
		registry,
		"Check file or recover",
		runtime.AgentLoopConfig{MaxTurns: 5, WorkspaceDir: tmpDir},
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success")
	}
	if result.Turns != 3 {
		t.Fatalf("expected 3 turns, got %d", result.Turns)
	}
	if len(result.ExecutedCalls) != 2 {
		t.Fatalf("expected 2 calls (1 failed, 1 succeeded), got %d", len(result.ExecutedCalls))
	}

	// Verify first call failed with error output
	if result.ExecutedCalls[0].ExitCode == 0 {
		t.Fatalf("expected first call to fail with non-zero exit code")
	}
	if !strings.Contains(result.ExecutedCalls[0].Output, "cannot read file") && !strings.Contains(result.ExecutedCalls[0].Output, "no such file") {
		t.Fatalf("expected error output in first call, got %s", result.ExecutedCalls[0].Output)
	}

	// Verify second call succeeded
	if result.ExecutedCalls[1].ExitCode != 0 {
		t.Fatalf("expected recovery write to succeed")
	}
}

// ============================================================================
// TestAgentLoop_WorkflowExecution:
// Model invokes Workflow tool -> runs test or build pipeline -> final answer
// ============================================================================
func TestAgentLoop_WorkflowExecution(t *testing.T) {
	tmpDir := t.TempDir()

	invoker := &mockModelInvoker{
		turnResponses: []types.StreamChunk{
			// Turn 1: Model calls Workflow tool
			{
				ToolCalls: []types.ToolCall{
					{
						ID:        "call_wf_1",
						Name:      "Workflow",
						Arguments: `{"name":"status"}`,
					},
				},
				FinishReason: "tool_calls",
			},
			// Turn 2: Final answer
			{
				Content:      "Workspace git status check complete.",
				FinishReason: "stop",
				Done:         true,
			},
		},
	}

	registry := runtime.NewDefaultRegistry(tmpDir)
	engine := runtime.NewExecutionEngine(nil, tmpDir)
	engine.SetRegistry(registry)

	result, err := runtime.RunAgentLoop(
		context.Background(),
		invoker,
		engine,
		registry,
		"Run workspace status workflow",
		runtime.AgentLoopConfig{MaxTurns: 4, WorkspaceDir: tmpDir},
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success")
	}
	if len(result.ExecutedCalls) != 1 {
		t.Fatalf("expected 1 workflow call, got %d", len(result.ExecutedCalls))
	}
}

// ============================================================================
// TestAgentLoop_MCPExecution:
// Model invokes MCP tool -> dispatches to MCPExecutor -> final answer
// ============================================================================
func TestAgentLoop_MCPExecution(t *testing.T) {
	tmpDir := t.TempDir()

	invoker := &mockModelInvoker{
		turnResponses: []types.StreamChunk{
			// Turn 1: Model calls call_mcp_tool
			{
				ToolCalls: []types.ToolCall{
					{
						ID:   "call_mcp_1",
						Name: "call_mcp_tool",
						Arguments: `{
							"ServerName": "amux_test",
							"ToolName": "lookup_docs",
							"Arguments": {"topic": "tool_execution"}
						}`,
					},
				},
				FinishReason: "tool_calls",
			},
			// Turn 2: Final answer
			{
				Content:      "Found documentation: Tool execution pipeline is production grade.",
				FinishReason: "stop",
				Done:         true,
			},
		},
	}

	registry := runtime.NewDefaultRegistry(tmpDir)
	engine := runtime.NewExecutionEngine(nil, tmpDir)

	// Register custom MCP executor
	mcpExec := runtime.NewMCPExecutor(func(ctx context.Context, serverName, toolName string, arguments json.RawMessage) (any, error) {
		if serverName == "amux_test" && toolName == "lookup_docs" {
			return map[string]string{
				"status":  "found",
				"summary": "Tool execution pipeline is production grade",
			}, nil
		}
		return nil, fmt.Errorf("unknown tool %s/%s", serverName, toolName)
	})
	engine.RegisterExecutor(mcpExec)
	engine.SetRegistry(registry)

	result, err := runtime.RunAgentLoop(
		context.Background(),
		invoker,
		engine,
		registry,
		"Look up documentation via MCP",
		runtime.AgentLoopConfig{MaxTurns: 4, WorkspaceDir: tmpDir},
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success")
	}
	if len(result.ExecutedCalls) != 1 {
		t.Fatalf("expected 1 MCP call, got %d", len(result.ExecutedCalls))
	}
	if !strings.Contains(result.ExecutedCalls[0].Output, "Tool execution pipeline is production grade") {
		t.Fatalf("expected MCP output in result, got: %s", result.ExecutedCalls[0].Output)
	}
}
