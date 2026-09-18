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

// NativeExecutor defines the interface for running native runtime operations.
type NativeExecutor interface {
	CanExecute(toolName string) bool
	Execute(ctx context.Context, call types.ToolCall) (*ExecutionResult, error)
}

// ============================================================================
// 1. CommandExecutor - Native Terminal / CLI Execution
// ============================================================================

type CommandExecutor struct {
	DefaultCwd string
}

func NewCommandExecutor(defaultCwd string) *CommandExecutor {
	if defaultCwd == "" {
		if cwd, err := os.Getwd(); err == nil {
			defaultCwd = cwd
		} else {
			defaultCwd = "."
		}
	}
	return &CommandExecutor{DefaultCwd: defaultCwd}
}

func (e *CommandExecutor) CanExecute(toolName string) bool {
	n := strings.ToLower(toolName)
	return n == "run_command" || n == "bash" || n == "run_terminal_command" || n == "exec_command"
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
		return res, res.Error
	}

	// Extract command string from native property names
	cmdStr := ""
	if v, ok := args["CommandLine"].(string); ok && v != "" {
		cmdStr = v
	} else if v, ok := args["command"].(string); ok && v != "" {
		cmdStr = v
	} else if v, ok := args["cmd"].(string); ok && v != "" {
		cmdStr = v
	}

	if strings.TrimSpace(cmdStr) == "" {
		res.Duration = time.Since(start)
		res.ExitCode = 1
		res.Error = errors.New("command string is required and cannot be empty")
		return res, res.Error
	}

	// Extract working directory
	cwd := e.DefaultCwd
	if v, ok := args["Cwd"].(string); ok && strings.TrimSpace(v) != "" {
		cwd = v
	} else if v, ok := args["cwd"].(string); ok && strings.TrimSpace(v) != "" {
		cwd = v
	}

	if !filepath.IsAbs(cwd) {
		if abs, err := filepath.Abs(cwd); err == nil {
			cwd = abs
		}
	}

	// Choose shell: zsh or bash
	shell := "bash"
	if _, err := exec.LookPath("zsh"); err == nil {
		shell = "zsh"
	}

	cmd := exec.CommandContext(ctx, shell, "-c", cmdStr)
	cmd.Dir = cwd

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
	} else {
		res.Output = stderrBuf.String()
	}

	if execErr != nil {
		res.Error = execErr
		if exitErr, ok := execErr.(*exec.ExitError); ok {
			res.ExitCode = exitErr.ExitCode()
		} else if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			res.ExitCode = 124 // Standard timeout exit code
		} else if errors.Is(ctx.Err(), context.Canceled) {
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
// 2. FileSystemExecutor - Native Filesystem Operations
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
	n := strings.ToLower(toolName)
	return n == "view_file" || n == "read_file" || n == "fileread" ||
		n == "write_to_file" || n == "write_file" || n == "filewrite" ||
		n == "list_dir"
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
		return res, res.Error
	}

	name := strings.ToLower(call.Name)
	switch {
	case name == "view_file" || name == "read_file" || name == "fileread":
		path := extractFilePath(args)
		if path == "" {
			res.Error = errors.New("missing file path")
			res.ExitCode = 1
			res.Duration = time.Since(start)
			return res, res.Error
		}
		data, err := os.ReadFile(path)
		res.Duration = time.Since(start)
		if err != nil {
			res.Error = err
			res.ExitCode = 1
			return res, err
		}
		res.Output = string(data)
		res.Stdout = res.Output
		res.ExitCode = 0
		return res, nil

	case name == "write_to_file" || name == "write_file" || name == "filewrite":
		path := extractFilePath(args)
		if path == "" {
			res.Error = errors.New("missing target file path")
			res.ExitCode = 1
			res.Duration = time.Since(start)
			return res, res.Error
		}
		content := ""
		if v, ok := args["CodeContent"].(string); ok {
			content = v
		} else if v, ok := args["content"].(string); ok {
			content = v
		}

		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			res.Error = err
			res.ExitCode = 1
			res.Duration = time.Since(start)
			return res, err
		}
		err := os.WriteFile(path, []byte(content), 0644)
		res.Duration = time.Since(start)
		if err != nil {
			res.Error = err
			res.ExitCode = 1
			return res, err
		}
		res.Output = fmt.Sprintf("Successfully wrote %d bytes to %s", len(content), path)
		res.Stdout = res.Output
		res.ExitCode = 0
		return res, nil

	case name == "list_dir":
		path := ""
		if v, ok := args["DirectoryPath"].(string); ok {
			path = v
		} else if v, ok := args["path"].(string); ok {
			path = v
		}
		if path == "" {
			path = e.BaseDir
		}
		entries, err := os.ReadDir(path)
		res.Duration = time.Since(start)
		if err != nil {
			res.Error = err
			res.ExitCode = 1
			return res, err
		}
		var lines []string
		for _, entry := range entries {
			kind := "file"
			if entry.IsDir() {
				kind = "dir"
			}
			lines = append(lines, fmt.Sprintf("%s\t[%s]", entry.Name(), kind))
		}
		res.Output = strings.Join(lines, "\n")
		res.Stdout = res.Output
		res.ExitCode = 0
		return res, nil
	}

	res.Duration = time.Since(start)
	res.ExitCode = 1
	res.Error = fmt.Errorf("unsupported filesystem tool: %s", call.Name)
	return res, res.Error
}

func extractFilePath(args map[string]any) string {
	for _, k := range []string{"AbsolutePath", "file_path", "path", "TargetFile"} {
		if v, ok := args[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// ============================================================================
// 3. ExecutionEngine - Unified Dispatcher & Validation Barrier
// ============================================================================

type ExecutionEngine struct {
	manifest  *RuntimeManifest
	executors []NativeExecutor
}

func NewExecutionEngine(m *RuntimeManifest) *ExecutionEngine {
	cwd, _ := os.Getwd()
	return &ExecutionEngine{
		manifest: m,
		executors: []NativeExecutor{
			NewCommandExecutor(cwd),
			NewFileSystemExecutor(cwd),
		},
	}
}

func (e *ExecutionEngine) RegisterExecutor(exec NativeExecutor) {
	e.executors = append(e.executors, exec)
}

// Execute validates and runs a native tool call.
func (e *ExecutionEngine) Execute(ctx context.Context, call types.ToolCall) (*ExecutionResult, error) {
	// 1. Validate call against schema if manifest is present
	if e.manifest != nil {
		errs := ValidateCallsAgainstManifest([]types.ToolCall{call}, e.manifest)
		if len(errs) > 0 {
			return &ExecutionResult{
				ToolCallID: call.ID,
				ToolName:   call.Name,
				ExitCode:   1,
				Error:      errs[0],
			}, errs[0]
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
		Error:      err,
	}, err
}
