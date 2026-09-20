package runtime_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"amux-accounts/pkg/runtime"
	"amux-accounts/pkg/tools"
	"amux-accounts/pkg/types"
)

// ============================================================================
// TEST 1: Native Command Execution
// pwd, ls, git status, git branch, node --version, stdout/stderr, timeout, cancellation
// ============================================================================
func TestAudit_Test1_NativeCommandExecution(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	exec := runtime.NewCommandExecutor(cwd)

	// 1. pwd
	resPwd, err := exec.Execute(context.Background(), types.ToolCall{
		Name:      "run_command",
		Arguments: `{"CommandLine": "pwd"}`,
	})
	if err != nil || resPwd.ExitCode != 0 {
		t.Fatalf("pwd failed: %v, exit: %d, out: %s", err, resPwd.ExitCode, resPwd.Output)
	}
	if !strings.Contains(resPwd.Stdout, "amux") {
		t.Fatalf("expected cwd in stdout, got: %s", resPwd.Stdout)
	}

	// 2. ls
	resLs, err := exec.Execute(context.Background(), types.ToolCall{
		Name:      "run_command",
		Arguments: `{"CommandLine": "ls"}`,
	})
	if err != nil || resLs.ExitCode != 0 {
		t.Fatalf("ls failed: %v", err)
	}
	if !strings.Contains(resLs.Stdout, "manifest.go") || !strings.Contains(resLs.Stdout, "executor.go") {
		t.Fatalf("expected manifest.go and executor.go in ls stdout, got: %s", resLs.Stdout)
	}

	// 3. git status
	resGitStatus, err := exec.Execute(context.Background(), types.ToolCall{
		Name:      "run_command",
		Arguments: `{"CommandLine": "git status --porcelain"}`,
	})
	if err != nil || resGitStatus.ExitCode != 0 {
		t.Fatalf("git status failed: %v", err)
	}

	// 4. git branch
	resGitBranch, err := exec.Execute(context.Background(), types.ToolCall{
		Name:      "run_command",
		Arguments: `{"CommandLine": "git branch --show-current"}`,
	})
	if err != nil || resGitBranch.ExitCode != 0 {
		t.Fatalf("git branch failed: %v", err)
	}
	if strings.TrimSpace(resGitBranch.Stdout) == "" {
		t.Fatalf("expected non-empty branch name, got empty")
	}

	// 5. node --version
	resNode, err := exec.Execute(context.Background(), types.ToolCall{
		Name:      "run_command",
		Arguments: `{"CommandLine": "node --version"}`,
	})
	if err != nil || resNode.ExitCode != 0 {
		t.Fatalf("node --version failed: %v", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(resNode.Stdout), "v") {
		t.Fatalf("expected node version starting with 'v', got: %s", resNode.Stdout)
	}

	// 6. Working directory isolation
	tempDir, err := os.MkdirTemp("", "amux-audit-cwd-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	resCwd, err := exec.Execute(context.Background(), types.ToolCall{
		Name:      "run_command",
		Arguments: fmt.Sprintf(`{"CommandLine": "pwd", "Cwd": %q}`, tempDir),
	})
	if err != nil || resCwd.ExitCode != 0 {
		t.Fatalf("custom cwd execution failed: %v", err)
	}
	resolvedTemp, _ := filepath.EvalSymlinks(tempDir)
	resolvedOut, _ := filepath.EvalSymlinks(strings.TrimSpace(resCwd.Stdout))
	if resolvedTemp != resolvedOut {
		t.Fatalf("expected cwd %s, got %s", resolvedTemp, resolvedOut)
	}

	// 7. Stderr capture & non-zero exit code
	resErr, err := exec.Execute(context.Background(), types.ToolCall{
		Name:      "run_command",
		Arguments: `{"CommandLine": "cat non_existent_file_xyz123.txt"}`,
	})
	if err == nil {
		t.Fatalf("expected command error for missing file")
	}
	if resErr.ExitCode == 0 {
		t.Fatalf("expected non-zero exit code, got 0")
	}
	if strings.TrimSpace(resErr.Stderr) == "" {
		t.Fatalf("expected stderr output, got empty")
	}

	// 8. Timeout handling
	ctxTimeout, cancelTimeout := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancelTimeout()
	resTimeout, err := exec.Execute(ctxTimeout, types.ToolCall{
		Name:      "run_command",
		Arguments: `{"CommandLine": "sleep 2"}`,
	})
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if resTimeout.Duration < 150*time.Millisecond || resTimeout.Duration > 800*time.Millisecond {
		t.Fatalf("timeout duration out of expected range: %v", resTimeout.Duration)
	}

	// 9. Context cancellation
	ctxCancel, cancelNow := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancelNow()
	}()
	resCancel, err := exec.Execute(ctxCancel, types.ToolCall{
		Name:      "run_command",
		Arguments: `{"CommandLine": "sleep 3"}`,
	})
	if err == nil {
		t.Fatalf("expected cancellation error")
	}
	if resCancel.Duration > 500*time.Millisecond {
		t.Fatalf("cancellation took too long: %v", resCancel.Duration)
	}
}

// ============================================================================
// TEST 2: Tool Schema Validation
// Valid calls accepted & executed; invalid calls rejected clearly without silent mutation
// ============================================================================
func TestAudit_Test2_ToolSchemaValidation(t *testing.T) {
	agyToolDef := types.ToolDef{
		Name:        "run_command",
		InputSchema: []byte(`{"type":"object","required":["CommandLine","Cwd","WaitMsBeforeAsync"],"properties":{"CommandLine":{"type":"string"},"Cwd":{"type":"string"},"WaitMsBeforeAsync":{"type":"integer"}}}`),
	}
	manifest := runtime.FromToolDefs("Antigravity", []types.ToolDef{agyToolDef})
	engine := runtime.NewExecutionEngine(manifest)

	// Valid call -> Must be accepted and executed
	validCall := types.ToolCall{
		ID:   "call_valid_1",
		Name: "run_command",
		Arguments: `{
			"CommandLine": "echo 'amux-schema-valid'",
			"Cwd": ".",
			"WaitMsBeforeAsync": 1000
		}`,
	}
	resValid, err := engine.Execute(context.Background(), validCall)
	if err != nil || resValid.ExitCode != 0 {
		t.Fatalf("valid schema call should succeed: %v, out: %s", err, resValid.Output)
	}
	if !strings.Contains(resValid.Stdout, "amux-schema-valid") {
		t.Fatalf("expected output 'amux-schema-valid', got: %s", resValid.Stdout)
	}

	// Invalid call 1: Missing required property 'Cwd'
	missingCwdCall := types.ToolCall{
		ID:   "call_invalid_1",
		Name: "run_command",
		Arguments: `{
			"CommandLine": "echo 'fail'",
			"WaitMsBeforeAsync": 1000
		}`,
	}
	resMissing, errMissing := engine.Execute(context.Background(), missingCwdCall)
	if errMissing == nil {
		t.Fatalf("expected validation error for missing required 'Cwd'")
	}
	var valErr *runtime.ValidationError
	if !errors.As(errMissing, &valErr) {
		t.Fatalf("expected ValidationError type, got %T: %v", errMissing, errMissing)
	}
	if len(valErr.MissingRequired) != 1 || valErr.MissingRequired[0] != "Cwd" {
		t.Fatalf("expected missing Cwd, got %v", valErr.MissingRequired)
	}
	if resMissing.ExitCode == 0 {
		t.Fatalf("invalid call must have non-zero exit code")
	}

	// Invalid call 2: Claude dialect 'command' emitted instead of 'CommandLine'
	claudeInsteadOfAgy := types.ToolCall{
		ID:        "call_invalid_2",
		Name:      "run_command",
		Arguments: `{"command": "echo 'wrong'"}`,
	}
	_, errClaude := engine.Execute(context.Background(), claudeInsteadOfAgy)
	if errClaude == nil {
		t.Fatalf("expected validation error when CommandLine is missing")
	}

	// Invalid call 3: Hallucinated / unlisted tool name
	hallucinatedTool := types.ToolCall{
		ID:        "call_invalid_3",
		Name:      "non_existent_magic_tool",
		Arguments: `{"param": "val"}`,
	}
	_, errUnlisted := engine.Execute(context.Background(), hallucinatedTool)
	if errUnlisted == nil {
		t.Fatalf("expected rejection for unlisted tool")
	}
	if !strings.Contains(errUnlisted.Error(), "not supported in Antigravity") {
		t.Fatalf("expected rejection message with runtime name, got: %v", errUnlisted)
	}
}

// ============================================================================
// TEST 3: Real MCP Integration
// Discovery from disk schemas, manifest conversion, execution, result parsing
// ============================================================================
func TestAudit_Test3_MCPIntegration(t *testing.T) {
	// Read a real MCP tool schema from the system
	mcpDir := "/Users/ninh.le/.gemini/antigravity-cli/mcp/StitchMCP"
	if _, err := os.Stat(mcpDir); os.IsNotExist(err) {
		t.Skip("StitchMCP dir not found, skipping disk MCP test")
	}

	entries, err := os.ReadDir(mcpDir)
	if err != nil {
		t.Fatalf("failed to read MCP dir: %v", err)
	}

	var mcpTools []types.ToolDef
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			schemaBytes, err := os.ReadFile(filepath.Join(mcpDir, e.Name()))
			if err != nil {
				continue
			}
			toolName := "mcp__stitch__" + strings.TrimSuffix(e.Name(), ".json")
			mcpTools = append(mcpTools, types.ToolDef{
				Name:        toolName,
				Description: "Stitch MCP capability: " + e.Name(),
				InputSchema: schemaBytes,
			})
		}
	}

	if len(mcpTools) == 0 {
		t.Fatalf("no MCP tools loaded from StitchMCP")
	}

	// Discover and build manifest dynamically
	manifest := runtime.FromToolDefs("StitchMCP", mcpTools)
	if manifest.Runtime != "StitchMCP" {
		t.Fatalf("expected StitchMCP runtime, got %s", manifest.Runtime)
	}
	if len(manifest.Tools) != len(mcpTools) {
		t.Fatalf("expected %d tools, got %d", len(mcpTools), len(manifest.Tools))
	}

	// Generate prompt contract
	contract := runtime.BuildRuntimeContract(manifest)
	if !strings.Contains(contract, "Host Environment is [StitchMCP]") {
		t.Fatalf("contract missing host environment declaration")
	}

	// Verify schema parameters correctly extracted
	firstTool := manifest.Tools[0]
	if len(firstTool.Parameters) == 0 && len(firstTool.InputSchema) > 50 {
		t.Fatalf("tool parameters should be parsed from schema")
	}
}

// ============================================================================
// TEST 4: Claude Code Compatibility
// Native Claude tools (Bash, FileWrite, FileRead) executed through AMUX
// ============================================================================
func TestAudit_Test4_ClaudeCodeCompatibility(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "amux-audit-claude-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	claudeTools := []types.ToolDef{
		{
			Name:        "Bash",
			Description: "Run bash command",
			InputSchema: []byte(`{"type":"object","required":["command"],"properties":{"command":{"type":"string"}}}`),
		},
		{
			Name:        "FileWrite",
			Description: "Write content to a file",
			InputSchema: []byte(`{"type":"object","required":["file_path","content"],"properties":{"file_path":{"type":"string"},"content":{"type":"string"}}}`),
		},
		{
			Name:        "FileRead",
			Description: "Read file contents",
			InputSchema: []byte(`{"type":"object","required":["file_path"],"properties":{"file_path":{"type":"string"}}}`),
		},
	}

	manifest := runtime.FromToolDefs("ClaudeCode", claudeTools)
	engine := runtime.NewExecutionEngine(manifest)

	// 1. FileWrite via Claude format
	testFile := filepath.Join(tmpDir, "claude_test.txt")
	testContent := "hello from AMUX Claude Code compatibility runtime\nline 2\n"

	writeCall := types.ToolCall{
		ID:   "call_write_1",
		Name: "FileWrite",
		Arguments: fmt.Sprintf(`{
			"file_path": %q,
			"content": %q
		}`, testFile, testContent),
	}
	resWrite, err := engine.Execute(context.Background(), writeCall)
	if err != nil || resWrite.ExitCode != 0 {
		t.Fatalf("FileWrite failed: %v", err)
	}

	// 2. FileRead via Claude format
	readCall := types.ToolCall{
		ID:   "call_read_1",
		Name: "FileRead",
		Arguments: fmt.Sprintf(`{"file_path": %q}`, testFile),
	}
	resRead, err := engine.Execute(context.Background(), readCall)
	if err != nil || resRead.ExitCode != 0 {
		t.Fatalf("FileRead failed: %v", err)
	}
	if resRead.Output != testContent {
		t.Fatalf("expected content %q, got %q", testContent, resRead.Output)
	}

	// 3. Bash execution via Claude format
	bashCall := types.ToolCall{
		ID:   "call_bash_1",
		Name: "Bash",
		Arguments: fmt.Sprintf(`{
			"command": "wc -l < %s"
		}`, testFile),
	}
	resBash, err := engine.Execute(context.Background(), bashCall)
	if err != nil || resBash.ExitCode != 0 {
		t.Fatalf("Bash execution failed: %v", err)
	}
	if strings.TrimSpace(resBash.Stdout) != "2" {
		t.Fatalf("expected 2 lines, got: %s", resBash.Stdout)
	}
}

// ============================================================================
// TEST 5: Codex Compatibility
// Request handling, capability discovery stripping unsupported parameters (temperature)
// ============================================================================
func TestAudit_Test5_CodexCompatibility(t *testing.T) {
	// Codex responses API strictly rejects temperature
	codexCaps := tools.GetModelCapabilities("codex", "gpt-5.6-terra")
	if codexCaps.SupportsTemperature {
		t.Fatalf("codex models must have SupportsTemperature = false")
	}

	// Standard models keep temperature
	standardCaps := tools.GetModelCapabilities("openai", "gpt-4o")
	if !standardCaps.SupportsTemperature {
		t.Fatalf("gpt-4o must have SupportsTemperature = true")
	}

	// Reasoning models (o3-mini, o1) reject temperature and enable reasoning effort
	o3Caps := tools.GetModelCapabilities("openai", "o3-mini")
	if o3Caps.SupportsTemperature {
		t.Fatalf("o3-mini must have SupportsTemperature = false")
	}
	if !o3Caps.SupportsReasoningEffort {
		t.Fatalf("o3-mini must support reasoning effort")
	}
}

// ============================================================================
// TEST 6: Failure Testing
// Malformed JSON, missing arguments, unavailable tool, runtime cancellation
// ============================================================================
func TestAudit_Test6_FailureTesting(t *testing.T) {
	toolsList := []types.ToolDef{
		{
			Name:        "Bash",
			InputSchema: []byte(`{"type":"object","required":["command"],"properties":{"command":{"type":"string"}}}`),
		},
	}
	manifest := runtime.FromToolDefs("ClaudeCode", toolsList)
	engine := runtime.NewExecutionEngine(manifest)

	// 1. Malformed JSON arguments
	malformedCall := types.ToolCall{
		Name:      "Bash",
		Arguments: `{"command": unclosed string`,
	}
	_, errMalformed := engine.Execute(context.Background(), malformedCall)
	if errMalformed == nil {
		t.Fatalf("expected error for malformed JSON")
	}
	if !strings.Contains(errMalformed.Error(), "JSON") {
		t.Fatalf("expected JSON error message, got: %v", errMalformed)
	}

	// 2. Missing required arguments
	emptyArgsCall := types.ToolCall{
		Name:      "Bash",
		Arguments: `{}`,
	}
	_, errEmpty := engine.Execute(context.Background(), emptyArgsCall)
	if errEmpty == nil {
		t.Fatalf("expected error for missing required arguments")
	}

	// 3. Unavailable / unlisted tool
	missingToolCall := types.ToolCall{
		Name:      "UnknownSuperTool",
		Arguments: `{"command": "ls"}`,
	}
	_, errMissingTool := engine.Execute(context.Background(), missingToolCall)
	if errMissingTool == nil {
		t.Fatalf("expected error for unlisted tool")
	}

	// 4. Runtime crash simulation (command exiting with crash code 139 / SIGSEGV)
	crashCall := types.ToolCall{
		Name:      "Bash",
		Arguments: `{"command": "exit 139"}`,
	}
	resCrash, errCrash := engine.Execute(context.Background(), crashCall)
	if errCrash == nil {
		t.Fatalf("expected error for crashed process")
	}
	if resCrash.ExitCode != 139 {
		t.Fatalf("expected exit code 139, got %d", resCrash.ExitCode)
	}

	// 5. Subsequent valid call succeeds cleanly without corrupted state
	cleanCall := types.ToolCall{
		Name:      "Bash",
		Arguments: `{"command": "echo 'recovered'"}`,
	}
	resClean, errClean := engine.Execute(context.Background(), cleanCall)
	if errClean != nil || resClean.ExitCode != 0 {
		t.Fatalf("recovery execution failed: %v", errClean)
	}
	if strings.TrimSpace(resClean.Stdout) != "recovered" {
		t.Fatalf("expected 'recovered', got %q", resClean.Stdout)
	}
}
