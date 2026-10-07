package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"amux-accounts/pkg/types"
)

// ExecutionResult represents the complete outcome of a native tool invocation.
type ExecutionResult struct {
	ToolCallID string        `json:"tool_call_id"`
	ToolName   string        `json:"tool_name"`
	Stdout     string        `json:"stdout,omitempty"`
	Stderr     string        `json:"stderr,omitempty"`
	Output     string        `json:"output,omitempty"`
	ExitCode   int           `json:"exit_code"`
	Duration   time.Duration `json:"duration"`
	Error      error         `json:"error,omitempty"`
}

// IsSuccess returns true if the tool completed with exit code 0 and no error.
func (r *ExecutionResult) IsSuccess() bool {
	if r == nil {
		return false
	}
	return r.ExitCode == 0 && r.Error == nil
}

// NativeExecutor defines the interface for running native runtime operations.
type NativeExecutor interface {
	CanExecute(toolName string) bool
	Execute(ctx context.Context, call types.ToolCall) (*ExecutionResult, error)
}

// ============================================================================
// 1. CommandExecutor - Native Terminal / CLI Execution
// ============================================================================

type CommandExecutor struct {
	DefaultCwd     string
	DefaultTimeout time.Duration
}

func NewCommandExecutor(defaultCwd string) *CommandExecutor {
	if defaultCwd == "" {
		if cwd, err := os.Getwd(); err == nil {
			defaultCwd = cwd
		} else {
			defaultCwd = "."
		}
	}
	return &CommandExecutor{
		DefaultCwd:     defaultCwd,
		DefaultTimeout: 60 * time.Second,
	}
}

func (e *CommandExecutor) CanExecute(toolName string) bool {
	n := strings.ToLower(strings.TrimSpace(toolName))
	return n == "run_command" || n == "bash" || n == "run_terminal_command" ||
		n == "exec_command" || n == "shell" || n == "terminal" ||
		n == "execute_command" || n == "cmd" || n == "sh"
}

func (e *CommandExecutor) Execute(ctx context.Context, call types.ToolCall) (*ExecutionResult, error) {
	start := time.Now()
	res := &ExecutionResult{
		ToolCallID: call.ID,
		ToolName:   call.Name,
	}

	var args map[string]any
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
		res.Duration = time.Since(start)
		res.ExitCode = 1
		res.Error = fmt.Errorf("invalid json arguments: %w", err)
		res.Output = res.Error.Error()
		return res, res.Error
	}

	// Extract command string from native property names
	cmdStr := ""
	for _, k := range []string{"CommandLine", "command", "cmd", "script", "input", "code"} {
		if v, ok := args[k].(string); ok && strings.TrimSpace(v) != "" {
			cmdStr = strings.TrimSpace(v)
			break
		}
	}

	if cmdStr == "" {
		res.Duration = time.Since(start)
		res.ExitCode = 1
		res.Error = errors.New("command string is required and cannot be empty")
		res.Output = res.Error.Error()
		return res, res.Error
	}

	// Extract working directory
	cwd := e.DefaultCwd
	for _, k := range []string{"Cwd", "cwd", "dir", "directory"} {
		if v, ok := args[k].(string); ok && strings.TrimSpace(v) != "" {
			cwd = strings.TrimSpace(v)
			break
		}
	}

	if !filepath.IsAbs(cwd) {
		cwd = filepath.Join(e.DefaultCwd, cwd)
	}
	if abs, err := filepath.Abs(cwd); err == nil {
		cwd = abs
	}

	// Determine timeout
	execTimeout := e.DefaultTimeout
	if v, ok := args["timeout_sec"].(float64); ok && v > 0 {
		execTimeout = time.Duration(v) * time.Second
	} else if v, ok := args["WaitMsBeforeAsync"].(float64); ok && v > 0 {
		execTimeout = time.Duration(v) * time.Millisecond
		if execTimeout < 5*time.Second {
			execTimeout = 5 * time.Second
		}
	}

	execCtx := ctx
	var cancel context.CancelFunc
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		execCtx, cancel = context.WithTimeout(ctx, execTimeout)
		defer cancel()
	}

	// Choose shell: zsh or bash
	shell := "bash"
	if _, err := exec.LookPath("zsh"); err == nil {
		shell = "zsh"
	}

	cmd := exec.CommandContext(execCtx, shell, "-c", cmdStr)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "TERM=dumb", "PAGER=cat")

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	execErr := cmd.Run()
	res.Duration = time.Since(start)
	res.Stdout = stdoutBuf.String()
	res.Stderr = stderrBuf.String()

	if stdoutBuf.Len() > 0 && stderrBuf.Len() > 0 {
		res.Output = stdoutBuf.String() + "\n[STDERR]\n" + stderrBuf.String()
	} else if stdoutBuf.Len() > 0 {
		res.Output = stdoutBuf.String()
	} else if stderrBuf.Len() > 0 {
		res.Output = stderrBuf.String()
	} else {
		res.Output = "(command produced no output)"
	}

	if execErr != nil {
		res.Error = execErr
		if exitErr, ok := execErr.(*exec.ExitError); ok {
			res.ExitCode = exitErr.ExitCode()
		} else if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
			res.ExitCode = 124 // Standard timeout exit code
			res.Output += fmt.Sprintf("\nCommand timed out after %v", execTimeout)
		} else if errors.Is(execCtx.Err(), context.Canceled) {
			res.ExitCode = 130 // Standard interrupted exit code
		} else {
			res.ExitCode = 1
		}
	} else {
		res.ExitCode = 0
	}

	return res, res.Error
}

// ============================================================================
// 2. FileSystemExecutor - Native Filesystem Operations (Read, Write, Edit, List)
// ============================================================================

type FileSystemExecutor struct {
	BaseDir string
}

func NewFileSystemExecutor(baseDir string) *FileSystemExecutor {
	if baseDir == "" {
		if cwd, err := os.Getwd(); err == nil {
			baseDir = cwd
		} else {
			baseDir = "."
		}
	}
	return &FileSystemExecutor{BaseDir: baseDir}
}

func (e *FileSystemExecutor) CanExecute(toolName string) bool {
	n := strings.ToLower(strings.TrimSpace(toolName))
	return n == "view_file" || n == "read_file" || n == "fileread" || n == "read" || n == "view" || n == "cat" || n == "show_file" || n == "get_file_content" || n == "open_file" ||
		n == "write_to_file" || n == "write_file" || n == "filewrite" || n == "write" || n == "create_file" || n == "new_file" || n == "save_file" ||
		n == "replace_file_content" || n == "fileedit" || n == "edit" || n == "edit_file" || n == "str_replace_editor" || n == "patch" || n == "apply_patch" || n == "modify_file" || n == "update_file" ||
		n == "list_dir" || n == "list_directory" || n == "ls" || n == "dir" || n == "list_files"
}

func (e *FileSystemExecutor) resolvePath(p string) string {
	if p == "" {
		return e.BaseDir
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(e.BaseDir, p)
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

func (e *FileSystemExecutor) Execute(ctx context.Context, call types.ToolCall) (*ExecutionResult, error) {
	start := time.Now()
	res := &ExecutionResult{
		ToolCallID: call.ID,
		ToolName:   call.Name,
	}

	var args map[string]any
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
		res.Duration = time.Since(start)
		res.ExitCode = 1
		res.Error = fmt.Errorf("invalid json arguments: %w", err)
		res.Output = res.Error.Error()
		return res, res.Error
	}

	name := strings.ToLower(strings.TrimSpace(call.Name))
	switch {
	// READ / VIEW
	case name == "view_file" || name == "read_file" || name == "fileread" || name == "read" || name == "view" || name == "cat" || name == "show_file" || name == "get_file_content" || name == "open_file":
		filePath := e.resolvePath(extractFilePath(args))
		if filePath == "" || filePath == e.BaseDir {
			res.Error = errors.New("missing required file path for read operation")
			res.ExitCode = 1
			res.Duration = time.Since(start)
			res.Output = res.Error.Error()
			return res, res.Error
		}

		data, err := os.ReadFile(filePath)
		res.Duration = time.Since(start)
		if err != nil {
			res.Error = fmt.Errorf("read file failed: %w", err)
			res.ExitCode = 1
			res.Output = res.Error.Error()
			return res, res.Error
		}

		content := string(data)
		startLine := extractInt(args, "StartLine", "start_line", "start", "offset")
		endLine := extractInt(args, "EndLine", "end_line", "end", "limit")

		// If line range specified, slice lines
		if startLine > 0 || endLine > 0 {
			lines := strings.Split(content, "\n")
			totalLines := len(lines)
			if startLine <= 0 {
				startLine = 1
			}
			if endLine <= 0 || endLine > totalLines {
				endLine = totalLines
			}
			if startLine > totalLines {
				res.Output = fmt.Sprintf("File %s has %d lines (requested startLine %d is beyond end of file)", filePath, totalLines, startLine)
				res.Stdout = res.Output
				res.ExitCode = 0
				return res, nil
			}
			selected := lines[startLine-1 : endLine]
			res.Output = strings.Join(selected, "\n")
		} else {
			res.Output = content
		}

		res.Stdout = res.Output
		res.ExitCode = 0
		return res, nil

	// WRITE / CREATE
	case name == "write_to_file" || name == "write_file" || name == "filewrite" || name == "write" || name == "create_file" || name == "new_file" || name == "save_file":
		filePath := e.resolvePath(extractFilePath(args))
		if filePath == "" || filePath == e.BaseDir {
			res.Error = errors.New("missing required file path for write operation")
			res.ExitCode = 1
			res.Duration = time.Since(start)
			res.Output = res.Error.Error()
			return res, res.Error
		}

		content := ""
		for _, k := range []string{"CodeContent", "content", "contents", "text", "body", "code"} {
			if v, ok := args[k].(string); ok {
				content = v
				break
			}
		}

		appendMode := extractBool(args, "Append", "append")

		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			res.Error = fmt.Errorf("failed to create parent directories: %w", err)
			res.ExitCode = 1
			res.Duration = time.Since(start)
			res.Output = res.Error.Error()
			return res, res.Error
		}

		var writeErr error
		if appendMode {
			f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				writeErr = err
			} else {
				_, writeErr = f.WriteString(content)
				f.Close()
			}
		} else {
			writeErr = os.WriteFile(filePath, []byte(content), 0644)
		}

		res.Duration = time.Since(start)
		if writeErr != nil {
			res.Error = fmt.Errorf("write to file failed: %w", writeErr)
			res.ExitCode = 1
			res.Output = res.Error.Error()
			return res, res.Error
		}

		action := "wrote"
		if appendMode {
			action = "appended"
		}
		res.Output = fmt.Sprintf("Successfully %s %d bytes to %s", action, len(content), filePath)
		res.Stdout = res.Output
		res.ExitCode = 0
		return res, nil

	// EDIT / REPLACE
	case name == "replace_file_content" || name == "fileedit" || name == "edit" || name == "edit_file" || name == "str_replace_editor" || name == "patch" || name == "apply_patch" || name == "modify_file" || name == "update_file":
		filePath := e.resolvePath(extractFilePath(args))
		if filePath == "" || filePath == e.BaseDir {
			res.Error = errors.New("missing required file path for edit operation")
			res.ExitCode = 1
			res.Duration = time.Since(start)
			res.Output = res.Error.Error()
			return res, res.Error
		}

		data, err := os.ReadFile(filePath)
		if err != nil {
			res.Error = fmt.Errorf("cannot edit file: %w", err)
			res.ExitCode = 1
			res.Duration = time.Since(start)
			res.Output = res.Error.Error()
			return res, res.Error
		}

		oldStr := ""
		for _, k := range []string{"TargetContent", "old_string", "old_str", "old", "original", "search"} {
			if v, ok := args[k].(string); ok {
				oldStr = v
				break
			}
		}

		newStr := ""
		for _, k := range []string{"ReplacementContent", "new_string", "new_str", "new", "replacement", "replace"} {
			if v, ok := args[k].(string); ok {
				newStr = v
				break
			}
		}

		allowMultiple := extractBool(args, "AllowMultiple", "allow_multiple")

		content := string(data)
		if oldStr == "" {
			if content == "" {
				// Empty file initial population
				err := os.WriteFile(filePath, []byte(newStr), 0644)
				res.Duration = time.Since(start)
				if err != nil {
					res.Error = err
					res.ExitCode = 1
					res.Output = err.Error()
					return res, err
				}
				res.Output = fmt.Sprintf("Populated empty file %s with %d bytes", filePath, len(newStr))
				res.Stdout = res.Output
				res.ExitCode = 0
				return res, nil
			}
			res.Error = errors.New("old_string cannot be empty for file edit")
			res.ExitCode = 1
			res.Duration = time.Since(start)
			res.Output = res.Error.Error()
			return res, res.Error
		}

		count := strings.Count(content, oldStr)
		if count == 0 {
			res.Error = fmt.Errorf("target content not found in %s (make sure whitespace and formatting match exactly)", filePath)
			res.ExitCode = 1
			res.Duration = time.Since(start)
			res.Output = res.Error.Error()
			return res, res.Error
		}

		if count > 1 && !allowMultiple {
			res.Error = fmt.Errorf("target content appears %d times in %s; provide more surrounding lines for a unique replacement or set AllowMultiple: true", count, filePath)
			res.ExitCode = 1
			res.Duration = time.Since(start)
			res.Output = res.Error.Error()
			return res, res.Error
		}

		var replaced string
		if allowMultiple {
			replaced = strings.ReplaceAll(content, oldStr, newStr)
		} else {
			replaced = strings.Replace(content, oldStr, newStr, 1)
		}

		if err := os.WriteFile(filePath, []byte(replaced), 0644); err != nil {
			res.Error = fmt.Errorf("failed to save edited file: %w", err)
			res.ExitCode = 1
			res.Duration = time.Since(start)
			res.Output = res.Error.Error()
			return res, res.Error
		}

		res.Duration = time.Since(start)
		res.Output = fmt.Sprintf("Successfully replaced %d occurrence(s) in %s", count, filePath)
		res.Stdout = res.Output
		res.ExitCode = 0
		return res, nil

	// LIST DIRECTORY
	case name == "list_dir" || name == "list_directory" || name == "ls" || name == "dir" || name == "list_files":
		dirPath := ""
		for _, k := range []string{"DirectoryPath", "path", "dir", "directory", "folder"} {
			if v, ok := args[k].(string); ok && strings.TrimSpace(v) != "" {
				dirPath = strings.TrimSpace(v)
				break
			}
		}
		dirPath = e.resolvePath(dirPath)

		entries, err := os.ReadDir(dirPath)
		res.Duration = time.Since(start)
		if err != nil {
			res.Error = fmt.Errorf("list directory failed: %w", err)
			res.ExitCode = 1
			res.Output = res.Error.Error()
			return res, res.Error
		}

		var lines []string
		for _, entry := range entries {
			kind := "file"
			if entry.IsDir() {
				kind = "dir"
			}
			lines = append(lines, fmt.Sprintf("%s\t[%s]", entry.Name(), kind))
		}
		if len(lines) == 0 {
			res.Output = "(directory is empty)"
		} else {
			res.Output = strings.Join(lines, "\n")
		}
		res.Stdout = res.Output
		res.ExitCode = 0
		return res, nil
	}

	res.Duration = time.Since(start)
	res.ExitCode = 1
	res.Error = fmt.Errorf("unsupported filesystem tool: %s", call.Name)
	res.Output = res.Error.Error()
	return res, res.Error
}

func extractFilePath(args map[string]any) string {
	for _, k := range []string{"AbsolutePath", "file_path", "path", "TargetFile", "filename", "file"} {
		if v, ok := args[k].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func extractInt(args map[string]any, keys ...string) int {
	for _, k := range keys {
		if v, ok := args[k]; ok && v != nil {
			switch val := v.(type) {
			case int:
				return val
			case float64:
				return int(val)
			case string:
				var n int
				if _, err := fmt.Sscanf(strings.TrimSpace(val), "%d", &n); err == nil {
					return n
				}
			}
		}
	}
	return 0
}

func extractBool(args map[string]any, keys ...string) bool {
	for _, k := range keys {
		if v, ok := args[k]; ok && v != nil {
			switch val := v.(type) {
			case bool:
				return val
			case string:
				s := strings.ToLower(strings.TrimSpace(val))
				return s == "true" || s == "1"
			case int, float64:
				return val != 0
			}
		}
	}
	return false
}

// ============================================================================
// 3. WorkflowExecutor - Predefined and Dynamic Multi-step Pipelines
// ============================================================================

type WorkflowExecutor struct {
	WorkspaceDir string
	cmdExecutor  *CommandExecutor
}

func NewWorkflowExecutor(workspaceDir string) *WorkflowExecutor {
	if workspaceDir == "" {
		if cwd, err := os.Getwd(); err == nil {
			workspaceDir = cwd
		} else {
			workspaceDir = "."
		}
	}
	return &WorkflowExecutor{
		WorkspaceDir: workspaceDir,
		cmdExecutor:  NewCommandExecutor(workspaceDir),
	}
}

func (e *WorkflowExecutor) CanExecute(toolName string) bool {
	n := strings.ToLower(strings.TrimSpace(toolName))
	return n == "workflow" || n == "run_workflow" || n == "execute_workflow" || n == "workflow_run" || n == "apply_workflow"
}

func (e *WorkflowExecutor) Execute(ctx context.Context, call types.ToolCall) (*ExecutionResult, error) {
	start := time.Now()
	res := &ExecutionResult{
		ToolCallID: call.ID,
		ToolName:   call.Name,
	}

	var args map[string]any
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
		res.Duration = time.Since(start)
		res.ExitCode = 1
		res.Error = fmt.Errorf("invalid workflow arguments: %w", err)
		res.Output = res.Error.Error()
		return res, res.Error
	}

	name := ""
	for _, k := range []string{"name", "workflow_name", "workflow", "id"} {
		if v, ok := args[k].(string); ok && strings.TrimSpace(v) != "" {
			name = strings.TrimSpace(v)
			break
		}
	}

	if name == "" {
		res.Duration = time.Since(start)
		res.ExitCode = 1
		res.Error = errors.New("workflow name is required")
		res.Output = res.Error.Error()
		return res, res.Error
	}

	inputs := ""
	for _, k := range []string{"inputs", "arguments", "args", "params", "parameters"} {
		if v, ok := args[k].(string); ok {
			inputs = v
			break
		}
	}

	// 1. Built-in workflow shortcuts
	var cmdStr string
	switch strings.ToLower(name) {
	case "test":
		cmdStr = "go test ./... -v"
	case "build":
		cmdStr = "go build ./..."
	case "status":
		cmdStr = "git status"
	case "diff":
		cmdStr = "git diff"
	case "lint":
		cmdStr = "go vet ./..."
	default:
		// Check for workflow files in standard locations (.windsurf/workflows/, .agent/skills/)
		candidates := []string{
			filepath.Join(e.WorkspaceDir, ".windsurf", "workflows", name+".md"),
			filepath.Join(e.WorkspaceDir, ".windsurf", "workflows", name),
			filepath.Join(e.WorkspaceDir, ".agent", "skills", name, "SKILL.md"),
		}
		foundWorkflowFile := ""
		for _, cand := range candidates {
			if _, err := os.Stat(cand); err == nil {
				foundWorkflowFile = cand
				break
			}
		}

		if foundWorkflowFile != "" {
			data, err := os.ReadFile(foundWorkflowFile)
			if err == nil {
				res.Duration = time.Since(start)
				res.ExitCode = 0
				res.Output = fmt.Sprintf("Workflow '%s' loaded from %s:\n%s", name, foundWorkflowFile, string(data))
				res.Stdout = res.Output
				return res, nil
			}
		}

		if inputs != "" {
			cmdStr = inputs
		} else {
			res.Duration = time.Since(start)
			res.ExitCode = 1
			res.Error = fmt.Errorf("unknown workflow '%s' and no commands provided", name)
			res.Output = res.Error.Error()
			return res, res.Error
		}
	}

	// Execute through CommandExecutor
	b, _ := json.Marshal(map[string]string{
		"CommandLine": cmdStr,
		"Cwd":         e.WorkspaceDir,
	})
	cmdCall := types.ToolCall{
		ID:        call.ID,
		Name:      "run_command",
		Arguments: string(b),
	}
	execRes, err := e.cmdExecutor.Execute(ctx, cmdCall)
	execRes.ToolName = call.Name
	execRes.ToolCallID = call.ID
	return execRes, err
}

// ============================================================================
// 4. MCPExecutor - Model Context Protocol Dispatcher
// ============================================================================

type MCPHandler func(ctx context.Context, serverName, toolName string, arguments json.RawMessage) (any, error)

type MCPExecutor struct {
	handler MCPHandler
}

func NewMCPExecutor(handler MCPHandler) *MCPExecutor {
	return &MCPExecutor{handler: handler}
}

func (e *MCPExecutor) CanExecute(toolName string) bool {
	n := strings.ToLower(strings.TrimSpace(toolName))
	return n == "call_mcp_tool" || strings.HasPrefix(n, "mcp__") || strings.HasPrefix(n, "mcp_") || strings.HasPrefix(n, "amux_")
}

func (e *MCPExecutor) Execute(ctx context.Context, call types.ToolCall) (*ExecutionResult, error) {
	start := time.Now()
	res := &ExecutionResult{
		ToolCallID: call.ID,
		ToolName:   call.Name,
	}

	if e.handler == nil {
		res.Duration = time.Since(start)
		res.ExitCode = 1
		res.Error = errors.New("no MCP handler registered")
		res.Output = res.Error.Error()
		return res, res.Error
	}

	var serverName, toolName string
	var arguments json.RawMessage

	lower := strings.ToLower(strings.TrimSpace(call.Name))
	if lower == "call_mcp_tool" {
		var args map[string]any
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
			res.Duration = time.Since(start)
			res.ExitCode = 1
			res.Error = fmt.Errorf("invalid MCP arguments: %w", err)
			res.Output = res.Error.Error()
			return res, res.Error
		}
		if s, ok := args["ServerName"].(string); ok {
			serverName = s
		}
		if t, ok := args["ToolName"].(string); ok {
			toolName = t
		}
		if raw, ok := args["Arguments"]; ok {
			arguments, _ = json.Marshal(raw)
		} else {
			arguments = json.RawMessage("{}")
		}
	} else if strings.HasPrefix(lower, "mcp__") {
		parts := strings.SplitN(strings.TrimPrefix(lower, "mcp__"), "__", 2)
		if len(parts) == 2 {
			serverName = parts[0]
			toolName = parts[1]
		} else {
			toolName = parts[0]
		}
		arguments = json.RawMessage(call.Arguments)
	} else {
		toolName = call.Name
		arguments = json.RawMessage(call.Arguments)
	}

	out, err := e.handler(ctx, serverName, toolName, arguments)
	res.Duration = time.Since(start)
	if err != nil {
		res.ExitCode = 1
		res.Error = err
		res.Output = fmt.Sprintf("MCP execution error: %v", err)
		return res, err
	}

	res.ExitCode = 0
	switch v := out.(type) {
	case string:
		res.Output = v
	default:
		b, _ := json.MarshalIndent(v, "", "  ")
		res.Output = string(b)
	}
	res.Stdout = res.Output
	return res, nil
}

// ============================================================================
// 5. ExecutionEngine - Unified Dispatcher & Validation Barrier
// ============================================================================

type ExecutionEngine struct {
	manifest  *RuntimeManifest
	executors []NativeExecutor
	registry  *Registry
}

func NewExecutionEngine(m *RuntimeManifest, baseDir ...string) *ExecutionEngine {
	cwd := ""
	if len(baseDir) > 0 && baseDir[0] != "" {
		cwd = baseDir[0]
	} else if dir, err := os.Getwd(); err == nil {
		cwd = dir
	} else {
		cwd = "."
	}

	return &ExecutionEngine{
		manifest: m,
		executors: []NativeExecutor{
			NewCommandExecutor(cwd),
			NewFileSystemExecutor(cwd),
			NewWorkflowExecutor(cwd),
		},
		registry: GlobalRegistry,
	}
}

func (e *ExecutionEngine) RegisterExecutor(exec NativeExecutor) {
	// Prepend to give custom executors higher precedence
	e.executors = append([]NativeExecutor{exec}, e.executors...)
}

func (e *ExecutionEngine) SetRegistry(r *Registry) {
	e.registry = r
}

// Execute validates and runs a native tool call against manifest or registry schemas.
func (e *ExecutionEngine) Execute(ctx context.Context, call types.ToolCall) (*ExecutionResult, error) {
	start := time.Now()

	// 1. Validate call against schema if manifest is present
	if e.manifest != nil {
		errs := ValidateCallsAgainstManifest([]types.ToolCall{call}, e.manifest)
		if len(errs) > 0 {
			return &ExecutionResult{
				ToolCallID: call.ID,
				ToolName:   call.Name,
				ExitCode:   1,
				Duration:   time.Since(start),
				Error:      errs[0],
				Output:     errs[0].Error(),
			}, errs[0]
		}
	} else if e.registry != nil {
		if toolDef, ok := e.registry.Lookup(call.Name); ok {
			if vErr := ValidateToolCall(call, toolDef); vErr != nil {
				return &ExecutionResult{
					ToolCallID: call.ID,
					ToolName:   call.Name,
					ExitCode:   1,
					Duration:   time.Since(start),
					Error:      vErr,
					Output:     vErr.Error(),
				}, vErr
			}
		}
	}

	// 2. Dispatch to matching executor
	for _, exec := range e.executors {
		if exec.CanExecute(call.Name) {
			res, err := exec.Execute(ctx, call)
			runtimeName := "NativeRuntime"
			if e.manifest != nil && e.manifest.Runtime != "" {
				runtimeName = e.manifest.Runtime
			}
			LogExecution("req_local", "local", runtimeName, call.Name, call.Arguments, res.Duration, res.ExitCode, err)
			return res, err
		}
	}

	err := fmt.Errorf("no native executor registered for tool '%s'", call.Name)
	return &ExecutionResult{
		ToolCallID: call.ID,
		ToolName:   call.Name,
		ExitCode:   1,
		Duration:   time.Since(start),
		Error:      err,
		Output:     err.Error(),
	}, err
}
