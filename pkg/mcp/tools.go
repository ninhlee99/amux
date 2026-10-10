package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ProviderInfo is one pool account as shown to MCP clients (no secrets).
type ProviderInfo struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Tier       string `json:"tier"`
	Priority   int    `json:"priority"`
	InPool     bool   `json:"inPool"`
	Configured bool   `json:"configured"`
	Account    string `json:"account,omitempty"`
	Model      string `json:"model,omitempty"`
}

// AskRequest is a one-shot question routed through amux's account pool.
type AskRequest struct {
	Prompt   string `json:"prompt"`
	System   string `json:"system,omitempty"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Timeout  time.Duration
}

// AskResult is the answer and which account produced it.
type AskResult struct {
	Provider  string `json:"provider"`
	Text      string `json:"text"`
	ElapsedMs int64  `json:"elapsedMs"`
}

// Backend is what the amux_* tools need from the rest of amux.
type Backend interface {
	Providers() ([]ProviderInfo, error)
	Ask(ctx context.Context, req AskRequest, onDelta func(string)) (*AskResult, error)
	Status(ctx context.Context) (map[string]any, error)
}

func obj(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func num(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }

func seconds(n int, def time.Duration) time.Duration {
	if n <= 0 {
		return def
	}
	return time.Duration(n) * time.Second
}

// RegisterAmuxTools adds the provider-agnostic tools backed by the pool.
func RegisterAmuxTools(s *Server, b Backend) {
	s.Register(Tool{
		Name:        "amux_providers",
		Title:       "List chat providers",
		Description: "List the AI accounts amux can route to (subscriptions, web chats such as ChatGPT/Claude/Gemini/Muse, and API keys), with tier, priority and whether each is in the rotation pool. Use an id with amux_ask's provider argument.",
		InputSchema: obj(map[string]any{}),
		ReadOnly:    true,
		Handler: func(context.Context, json.RawMessage, func(string)) (any, error) {
			ps, err := b.Providers()
			if err != nil {
				return nil, err
			}
			return map[string]any{"providers": ps, "count": len(ps)}, nil
		},
	})
	s.Register(Tool{
		Name:  "amux_ask",
		Title: "Ask another AI",
		Description: "Send a self-contained prompt to another AI through amux's account pool and return its answer. " +
			"Choose where it goes with provider (see amux_providers): an exact account id such as \"gemini:web:01\" pins that account; " +
			"a family such as \"gemini:web\", \"chatgpt\" or \"claude:web\" lets amux pick among those accounts with failover; " +
			"omit it to let amux pick from the whole pool. Good for second opinions, research and drafting. " +
			"The other AI cannot see your files or conversation — include all context it needs.",
		InputSchema: obj(map[string]any{
			"prompt":      str("The full, self-contained question or task."),
			"context":     str("Optional workspace context, active file content, or project structure."),
			"system":      str("Optional system instructions."),
			"provider":    str("Optional exact account id (pins it) or account family such as \"gemini:web\" (amux picks within it)."),
			"model":       str("Optional model override for the chosen account."),
			"timeout_sec": num("Give up after this many seconds (default 300)."),
		}, "prompt"),
		Handler: func(ctx context.Context, raw json.RawMessage, progress func(string)) (any, error) {
			var a struct {
				Prompt, Context, System, Provider, Model string
				TimeoutSec                               int `json:"timeout_sec"`
			}
			if err := DecodeArgs(raw, &a); err != nil {
				return nil, err
			}
			if strings.TrimSpace(a.Prompt) == "" {
				return nil, errors.New("prompt is required")
			}
			fullPrompt := a.Prompt
			if strings.TrimSpace(a.Context) != "" {
				fullPrompt = "[Workspace Context]\n" + strings.TrimSpace(a.Context) + "\n\n[Task / Question]\n" + a.Prompt
			}
			req := AskRequest{Prompt: fullPrompt, System: a.System, Provider: a.Provider, Model: a.Model, Timeout: seconds(a.TimeoutSec, 5*time.Minute)}
			ctx, cancel := context.WithTimeout(ctx, req.Timeout)
			defer cancel()
			return b.Ask(ctx, req, throttle(progress))
		},
	})
	s.Register(Tool{
		Name:        "amux_review",
		Title:       "Review code diff or snippet",
		Description: "Send a git diff, pull request patch, or code snippet to an AI in the pool for independent code review (bugs, security, architecture, edge cases).",
		InputSchema: obj(map[string]any{
			"diff":        str("The git diff, patch, or code snippet to review."),
			"focus":       str("Optional review focus (e.g., 'security', 'performance', 'style', 'bugs', 'architecture')."),
			"context":     str("Optional background context, requirements, or architecture notes."),
			"provider":    str("Optional exact account id or family (e.g., 'gemini:web', 'claude:web', 'chatgpt')."),
			"timeout_sec": num("Give up after this many seconds (default 300)."),
		}, "diff"),
		Handler: func(ctx context.Context, raw json.RawMessage, progress func(string)) (any, error) {
			var a struct {
				Diff, Focus, Context, Provider string
				TimeoutSec                     int `json:"timeout_sec"`
			}
			if err := DecodeArgs(raw, &a); err != nil {
				return nil, err
			}
			if strings.TrimSpace(a.Diff) == "" {
				return nil, errors.New("diff is required")
			}
			sysPrompt := "You are an expert senior code reviewer. Review the provided code changes thoroughly. " +
				"Identify bugs, security vulnerabilities, edge cases, regression risks, and architectural improvements. " +
				"Provide clear, actionable feedback with code suggestions where appropriate."
			if a.Focus != "" {
				sysPrompt += " Focus especially on: " + a.Focus + "."
			}
			userPrompt := "Please review the following code changes:\n\n```diff\n" + a.Diff + "\n```"
			if strings.TrimSpace(a.Context) != "" {
				userPrompt = "[Context]\n" + strings.TrimSpace(a.Context) + "\n\n" + userPrompt
			}
			req := AskRequest{
				Prompt:   userPrompt,
				System:   sysPrompt,
				Provider: a.Provider,
				Timeout:  seconds(a.TimeoutSec, 5*time.Minute),
			}
			ctx, cancel := context.WithTimeout(ctx, req.Timeout)
			defer cancel()
			return b.Ask(ctx, req, throttle(progress))
		},
	})
	s.Register(Tool{
		Name:        "amux_diagnose",
		Title:       "Diagnose error or crash",
		Description: "Investigate a bug, error message, failing test, or stack trace. Analyzes root cause, reproduction conditions, and outlines exact fixes.",
		InputSchema: obj(map[string]any{
			"error":       str("The error message, panic log, test failure, or stack trace."),
			"code":        str("Optional code snippet or function implementation where the error occurred."),
			"context":     str("Optional environment details, inputs, or steps that triggered the error."),
			"provider":    str("Optional exact account id or family (e.g., 'gemini:web', 'claude:web', 'chatgpt')."),
			"timeout_sec": num("Give up after this many seconds (default 300)."),
		}, "error"),
		Handler: func(ctx context.Context, raw json.RawMessage, progress func(string)) (any, error) {
			var a struct {
				Error, Code, Context, Provider string
				TimeoutSec                     int `json:"timeout_sec"`
			}
			if err := DecodeArgs(raw, &a); err != nil {
				return nil, err
			}
			if strings.TrimSpace(a.Error) == "" {
				return nil, errors.New("error is required")
			}
			sysPrompt := "You are a master software debugging specialist. Analyze the error carefully. " +
				"1. Identify the exact root cause of the failure.\n" +
				"2. Explain why it occurred given the code and context.\n" +
				"3. Provide step-by-step instructions and code snippets to fix the bug permanently.\n" +
				"4. Highlight edge cases or regression risks to verify."
			var userPrompt strings.Builder
			userPrompt.WriteString("[Error / Stack Trace]\n")
			userPrompt.WriteString(strings.TrimSpace(a.Error))
			if strings.TrimSpace(a.Code) != "" {
				userPrompt.WriteString("\n\n[Relevant Code]\n```\n")
				userPrompt.WriteString(strings.TrimSpace(a.Code))
				userPrompt.WriteString("\n```")
			}
			if strings.TrimSpace(a.Context) != "" {
				userPrompt.WriteString("\n\n[Context]\n")
				userPrompt.WriteString(strings.TrimSpace(a.Context))
			}
			req := AskRequest{
				Prompt:   userPrompt.String(),
				System:   sysPrompt,
				Provider: a.Provider,
				Timeout:  seconds(a.TimeoutSec, 5*time.Minute),
			}
			ctx, cancel := context.WithTimeout(ctx, req.Timeout)
			defer cancel()
			return b.Ask(ctx, req, throttle(progress))
		},
	})
	s.Register(Tool{
		Name:        "amux_fix",
		Title:       "Generate bug fix or code patch",
		Description: "Generate a precise code patch, refactor, or bug fix for a given file or function based on issue description.",
		InputSchema: obj(map[string]any{
			"file_content": str("The current code content of the file or function needing fixes."),
			"issue":        str("Description of the bug, test failure, or requirement to implement."),
			"instructions": str("Optional specific coding standards, constraints, or preferences."),
			"provider":     str("Optional exact account id or family (e.g., 'gemini:web', 'claude:web', 'chatgpt')."),
			"timeout_sec":  num("Give up after this many seconds (default 300)."),
		}, "file_content", "issue"),
		Handler: func(ctx context.Context, raw json.RawMessage, progress func(string)) (any, error) {
			var a struct {
				FileContent  string `json:"file_content"`
				Issue        string `json:"issue"`
				Instructions string `json:"instructions"`
				Provider     string `json:"provider"`
				TimeoutSec   int    `json:"timeout_sec"`
			}
			if err := DecodeArgs(raw, &a); err != nil {
				return nil, err
			}
			if strings.TrimSpace(a.FileContent) == "" || strings.TrimSpace(a.Issue) == "" {
				return nil, errors.New("file_content and issue are required")
			}
			sysPrompt := "You are an expert senior software engineer. Fix the issue in the provided code. " +
				"Return the complete corrected code or clean replacement chunk with explanation of what was changed and why."
			var userPrompt strings.Builder
			userPrompt.WriteString("[Issue to Fix]\n")
			userPrompt.WriteString(strings.TrimSpace(a.Issue))
			if strings.TrimSpace(a.Instructions) != "" {
				userPrompt.WriteString("\n\n[Instructions / Constraints]\n")
				userPrompt.WriteString(strings.TrimSpace(a.Instructions))
			}
			userPrompt.WriteString("\n\n[Current Code]\n```\n")
			userPrompt.WriteString(strings.TrimSpace(a.FileContent))
			userPrompt.WriteString("\n```")
			req := AskRequest{
				Prompt:   userPrompt.String(),
				System:   sysPrompt,
				Provider: a.Provider,
				Timeout:  seconds(a.TimeoutSec, 5*time.Minute),
			}
			ctx, cancel := context.WithTimeout(ctx, req.Timeout)
			defer cancel()
			return b.Ask(ctx, req, throttle(progress))
		},
	})
	s.Register(Tool{
		Name:        "amux_analyze",
		Title:       "Analyze project architecture or design",
		Description: "Analyze codebase structure, database schema, module relationships, or technical trade-offs for a project.",
		InputSchema: obj(map[string]any{
			"structure":   str("The file tree, module layout, API contracts, or schema to evaluate."),
			"objective":   str("What you want to achieve, refactor, or evaluate (e.g. scalability, modularity, security)."),
			"context":     str("Optional business requirements or tech stack constraints."),
			"provider":    str("Optional exact account id or family (e.g., 'gemini:web', 'claude:web', 'chatgpt')."),
			"timeout_sec": num("Give up after this many seconds (default 300)."),
		}, "structure", "objective"),
		Handler: func(ctx context.Context, raw json.RawMessage, progress func(string)) (any, error) {
			var a struct {
				Structure, Objective, Context, Provider string
				TimeoutSec                              int `json:"timeout_sec"`
			}
			if err := DecodeArgs(raw, &a); err != nil {
				return nil, err
			}
			if strings.TrimSpace(a.Structure) == "" || strings.TrimSpace(a.Objective) == "" {
				return nil, errors.New("structure and objective are required")
			}
			sysPrompt := "You are a Principal Software Architect. Evaluate the provided system structure and provide clear, actionable architectural guidance. " +
				"Identify bottlenecks, coupling issues, layering violations, scalability concerns, and recommend concrete improvements."
			var userPrompt strings.Builder
			userPrompt.WriteString("[Objective]\n")
			userPrompt.WriteString(strings.TrimSpace(a.Objective))
			if strings.TrimSpace(a.Context) != "" {
				userPrompt.WriteString("\n\n[Context]\n")
				userPrompt.WriteString(strings.TrimSpace(a.Context))
			}
			userPrompt.WriteString("\n\n[System Structure / Schema]\n```\n")
			userPrompt.WriteString(strings.TrimSpace(a.Structure))
			userPrompt.WriteString("\n```")
			req := AskRequest{
				Prompt:   userPrompt.String(),
				System:   sysPrompt,
				Provider: a.Provider,
				Timeout:  seconds(a.TimeoutSec, 5*time.Minute),
			}
			ctx, cancel := context.WithTimeout(ctx, req.Timeout)
			defer cancel()
			return b.Ask(ctx, req, throttle(progress))
		},
	})
	s.Register(Tool{
		Name:        "amux_status",
		Title:       "amux status",
		Description: "Report whether the amux gateway is running, where coding agents should point (base URLs), and how many accounts are usable.",
		InputSchema: obj(map[string]any{}),
		ReadOnly:    true,
		Handler: func(ctx context.Context, _ json.RawMessage, _ func(string)) (any, error) {
			return b.Status(ctx)
		},
	})
}

// throttle turns cumulative-text callbacks into at most ~1 progress message
// per second carrying the newest tail of the text.
func throttle(progress func(string)) func(string) {
	var last time.Time
	return func(full string) {
		if time.Since(last) < time.Second {
			return
		}
		last = time.Now()
		tail := full
		if r := []rune(tail); len(r) > 200 {
			tail = "…" + string(r[len(r)-200:])
		}
		progress(fmt.Sprintf("%d chars: %s", len(full), tail))
	}
}
