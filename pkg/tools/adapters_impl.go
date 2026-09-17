package tools

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// ============================================================================
// 1. Anthropic Claude Adapter
// ============================================================================

type AnthropicToolAdapter struct{}

func NewAnthropicToolAdapter() *AnthropicToolAdapter {
	return &AnthropicToolAdapter{}
}

func (a *AnthropicToolAdapter) Name() string {
	return "anthropic"
}

func (a *AnthropicToolAdapter) ToUniversalTools(raw []byte) ([]UniversalTool, error) {
	var wrap struct {
		Tools []struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description,omitempty"`
			InputSchema map[string]interface{} `json:"input_schema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, err
	}

	var out []UniversalTool
	for _, t := range wrap.Tools {
		if strings.TrimSpace(t.Name) == "" {
			continue
		}
		out = append(out, UniversalTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
			Source:      "anthropic",
		})
	}
	return out, nil
}

func (a *AnthropicToolAdapter) FromUniversalTools(tools []UniversalTool) (any, error) {
	type anthropicTool struct {
		Name        string                 `json:"name"`
		Description string                 `json:"description,omitempty"`
		InputSchema map[string]interface{} `json:"input_schema"`
	}
	out := make([]anthropicTool, 0, len(tools))
	for _, t := range tools {
		out = append(out, anthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
	}
	return out, nil
}

func (a *AnthropicToolAdapter) ToUniversalToolCalls(raw any) ([]UniversalToolCall, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}

	var blocks []struct {
		Type  string                 `json:"type"`
		ID    string                 `json:"id"`
		Name  string                 `json:"name"`
		Input map[string]interface{} `json:"input"`
	}
	if err := json.Unmarshal(b, &blocks); err != nil {
		// Single block fallback
		var single struct {
			Type  string                 `json:"type"`
			ID    string                 `json:"id"`
			Name  string                 `json:"name"`
			Input map[string]interface{} `json:"input"`
		}
		if sErr := json.Unmarshal(b, &single); sErr == nil && single.Type == "tool_use" {
			blocks = append(blocks, single)
		} else {
			return nil, err
		}
	}

	var out []UniversalToolCall
	for _, blk := range blocks {
		if blk.Type == "tool_use" {
			out = append(out, UniversalToolCall{
				ID:        blk.ID,
				Name:      blk.Name,
				Arguments: blk.Input,
				RawJSON:   SerializeArguments(blk.Input),
			})
		}
	}
	return out, nil
}

func (a *AnthropicToolAdapter) FromUniversalToolCalls(calls []UniversalToolCall) (any, error) {
	type anthropicUseBlock struct {
		Type  string                 `json:"type"`
		ID    string                 `json:"id"`
		Name  string                 `json:"name"`
		Input map[string]interface{} `json:"input"`
	}
	out := make([]anthropicUseBlock, 0, len(calls))
	for _, c := range calls {
		out = append(out, anthropicUseBlock{
			Type:  "tool_use",
			ID:    c.ID,
			Name:  c.Name,
			Input: c.Arguments,
		})
	}
	return out, nil
}

func (a *AnthropicToolAdapter) ToUniversalToolResults(raw any) ([]UniversalToolResult, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}

	var blocks []struct {
		Type      string `json:"type"`
		ToolUseID string `json:"tool_use_id"`
		Content   string `json:"content"`
		IsError   bool   `json:"is_error"`
	}
	if err := json.Unmarshal(b, &blocks); err != nil {
		var single struct {
			Type      string `json:"type"`
			ToolUseID string `json:"tool_use_id"`
			Content   string `json:"content"`
			IsError   bool   `json:"is_error"`
		}
		if sErr := json.Unmarshal(b, &single); sErr == nil && single.Type == "tool_result" {
			blocks = append(blocks, single)
		} else {
			return nil, err
		}
	}

	var out []UniversalToolResult
	for _, blk := range blocks {
		if blk.Type == "tool_result" {
			var uErr *UniversalToolError
			if blk.IsError {
				uErr = &UniversalToolError{Code: "tool_execution_error", Message: blk.Content, Retryable: true, Provider: "anthropic"}
			}
			out = append(out, UniversalToolResult{
				ToolCallID: blk.ToolUseID,
				Success:    !blk.IsError,
				Output:     blk.Content,
				Error:      uErr,
			})
		}
	}
	return out, nil
}

func (a *AnthropicToolAdapter) FromUniversalToolResults(results []UniversalToolResult) (any, error) {
	type anthropicResultBlock struct {
		Type      string `json:"type"`
		ToolUseID string `json:"tool_use_id"`
		Content   string `json:"content"`
		IsError   bool   `json:"is_error,omitempty"`
	}
	out := make([]anthropicResultBlock, 0, len(results))
	for _, r := range results {
		out = append(out, anthropicResultBlock{
			Type:      "tool_result",
			ToolUseID: r.ToolCallID,
			Content:   r.Output,
			IsError:   !r.Success,
		})
	}
	return out, nil
}

// ============================================================================
// 2. OpenAI Function Calling Adapter
// ============================================================================

type OpenAIToolAdapter struct{}

func NewOpenAIToolAdapter() *OpenAIToolAdapter {
	return &OpenAIToolAdapter{}
}

func (o *OpenAIToolAdapter) Name() string {
	return "openai"
}

func (o *OpenAIToolAdapter) ToUniversalTools(raw []byte) ([]UniversalTool, error) {
	type toolEntry struct {
		Type     string `json:"type"`
		Function struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description,omitempty"`
			Parameters  map[string]interface{} `json:"parameters"`
		} `json:"function"`
	}

	var tools []toolEntry

	var wrap struct {
		Tools []toolEntry `json:"tools"`
	}
	if err := json.Unmarshal(raw, &wrap); err == nil && len(wrap.Tools) > 0 {
		tools = wrap.Tools
	} else {
		// Try bare array
		if err := json.Unmarshal(raw, &tools); err != nil {
			return nil, err
		}
	}

	var out []UniversalTool
	for _, t := range tools {
		if t.Type == "function" || t.Type == "" {
			out = append(out, UniversalTool{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				InputSchema: t.Function.Parameters,
				Source:      "openai",
			})
		}
	}
	return out, nil
}

func (o *OpenAIToolAdapter) FromUniversalTools(tools []UniversalTool) (any, error) {
	type openAIFunc struct {
		Name        string                 `json:"name"`
		Description string                 `json:"description,omitempty"`
		Parameters  map[string]interface{} `json:"parameters"`
	}
	type openAITool struct {
		Type     string     `json:"type"`
		Function openAIFunc `json:"function"`
	}
	out := make([]openAITool, 0, len(tools))
	for _, t := range tools {
		out = append(out, openAITool{
			Type: "function",
			Function: openAIFunc{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}
	return out, nil
}

func (o *OpenAIToolAdapter) ToUniversalToolCalls(raw any) ([]UniversalToolCall, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}

	var calls []struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	if err := json.Unmarshal(b, &calls); err != nil {
		return nil, err
	}

	var out []UniversalToolCall
	for _, c := range calls {
		args, _ := ParseArguments(c.Function.Arguments)
		out = append(out, UniversalToolCall{
			ID:        c.ID,
			Name:      c.Function.Name,
			Arguments: args,
			RawJSON:   c.Function.Arguments,
		})
	}
	return out, nil
}

func (o *OpenAIToolAdapter) FromUniversalToolCalls(calls []UniversalToolCall) (any, error) {
	type funcCall struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	type openAICall struct {
		ID       string   `json:"id"`
		Type     string   `json:"type"`
		Function funcCall `json:"function"`
	}
	out := make([]openAICall, 0, len(calls))
	for _, c := range calls {
		raw := c.RawJSON
		if raw == "" {
			raw = SerializeArguments(c.Arguments)
		}
		out = append(out, openAICall{
			ID:   c.ID,
			Type: "function",
			Function: funcCall{
				Name:      c.Name,
				Arguments: raw,
			},
		})
	}
	return out, nil
}

func (o *OpenAIToolAdapter) ToUniversalToolResults(raw any) ([]UniversalToolResult, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var msg struct {
		Role       string `json:"role"`
		ToolCallID string `json:"tool_call_id"`
		Content    string `json:"content"`
	}
	if err := json.Unmarshal(b, &msg); err != nil {
		return nil, err
	}
	return []UniversalToolResult{
		{
			ToolCallID: msg.ToolCallID,
			Success:    true,
			Output:     msg.Content,
		},
	}, nil
}

func (o *OpenAIToolAdapter) FromUniversalToolResults(results []UniversalToolResult) (any, error) {
	type toolMessage struct {
		Role       string `json:"role"`
		ToolCallID string `json:"tool_call_id"`
		Content    string `json:"content"`
	}
	out := make([]toolMessage, 0, len(results))
	for _, r := range results {
		out = append(out, toolMessage{
			Role:       "tool",
			ToolCallID: r.ToolCallID,
			Content:    r.Output,
		})
	}
	return out, nil
}

// ============================================================================
// 3. Google Gemini Adapter
// ============================================================================

type GeminiToolAdapter struct{}

func NewGeminiToolAdapter() *GeminiToolAdapter {
	return &GeminiToolAdapter{}
}

func (g *GeminiToolAdapter) Name() string {
	return "gemini"
}

func (g *GeminiToolAdapter) ToUniversalTools(raw []byte) ([]UniversalTool, error) {
	var wrap struct {
		FunctionDeclarations []struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description,omitempty"`
			Parameters  map[string]interface{} `json:"parameters"`
		} `json:"functionDeclarations"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, err
	}
	var out []UniversalTool
	for _, f := range wrap.FunctionDeclarations {
		out = append(out, UniversalTool{
			Name:        f.Name,
			Description: f.Description,
			InputSchema: f.Parameters,
			Source:      "gemini",
		})
	}
	return out, nil
}

func (g *GeminiToolAdapter) FromUniversalTools(tools []UniversalTool) (any, error) {
	type geminiDecl struct {
		Name        string                 `json:"name"`
		Description string                 `json:"description,omitempty"`
		Parameters  map[string]interface{} `json:"parameters"`
	}
	type geminiToolWrap struct {
		FunctionDeclarations []geminiDecl `json:"functionDeclarations"`
	}
	var decls []geminiDecl
	for _, t := range tools {
		decls = append(decls, geminiDecl{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.InputSchema,
		})
	}
	return geminiToolWrap{FunctionDeclarations: decls}, nil
}

func (g *GeminiToolAdapter) ToUniversalToolCalls(raw any) ([]UniversalToolCall, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var call struct {
		FunctionCall struct {
			Name string                 `json:"name"`
			Args map[string]interface{} `json:"args"`
		} `json:"functionCall"`
	}
	if err := json.Unmarshal(b, &call); err != nil {
		return nil, err
	}
	return []UniversalToolCall{
		{
			ID:        fmt.Sprintf("gemini_call_%s", call.FunctionCall.Name),
			Name:      call.FunctionCall.Name,
			Arguments: call.FunctionCall.Args,
			RawJSON:   SerializeArguments(call.FunctionCall.Args),
		},
	}, nil
}

func (g *GeminiToolAdapter) FromUniversalToolCalls(calls []UniversalToolCall) (any, error) {
	type geminiCall struct {
		Name string                 `json:"name"`
		Args map[string]interface{} `json:"args"`
	}
	type geminiPart struct {
		FunctionCall geminiCall `json:"functionCall"`
	}
	out := make([]geminiPart, 0, len(calls))
	for _, c := range calls {
		out = append(out, geminiPart{
			FunctionCall: geminiCall{
				Name: c.Name,
				Args: c.Arguments,
			},
		})
	}
	return out, nil
}

func (g *GeminiToolAdapter) ToUniversalToolResults(raw any) ([]UniversalToolResult, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var resp struct {
		FunctionResponse struct {
			Name     string                 `json:"name"`
			Response map[string]interface{} `json:"response"`
		} `json:"functionResponse"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, err
	}
	outStr := SerializeArguments(resp.FunctionResponse.Response)
	return []UniversalToolResult{
		{
			ToolCallID: resp.FunctionResponse.Name,
			Success:    true,
			Output:     outStr,
		},
	}, nil
}

func (g *GeminiToolAdapter) FromUniversalToolResults(results []UniversalToolResult) (any, error) {
	type geminiResp struct {
		Name     string                 `json:"name"`
		Response map[string]interface{} `json:"response"`
	}
	type geminiPart struct {
		FunctionResponse geminiResp `json:"functionResponse"`
	}
	out := make([]geminiPart, 0, len(results))
	for _, r := range results {
		args, _ := ParseArguments(r.Output)
		if args == nil {
			args = map[string]interface{}{"result": r.Output}
		}
		out = append(out, geminiPart{
			FunctionResponse: geminiResp{
				Name:     r.ToolCallID,
				Response: args,
			},
		})
	}
	return out, nil
}

// ============================================================================
// 4. Model Context Protocol (MCP) Adapter
// ============================================================================

type MCPToolAdapter struct{}

func NewMCPToolAdapter() *MCPToolAdapter {
	return &MCPToolAdapter{}
}

func (m *MCPToolAdapter) Name() string {
	return "mcp"
}

func (m *MCPToolAdapter) ToUniversalTools(raw []byte) ([]UniversalTool, error) {
	var manifest struct {
		Tools []struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description,omitempty"`
			InputSchema map[string]interface{} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, err
	}
	var out []UniversalTool
	for _, t := range manifest.Tools {
		out = append(out, UniversalTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
			Source:      "mcp",
		})
	}
	return out, nil
}

func (m *MCPToolAdapter) FromUniversalTools(tools []UniversalTool) (any, error) {
	type mcpTool struct {
		Name        string                 `json:"name"`
		Description string                 `json:"description,omitempty"`
		InputSchema map[string]interface{} `json:"inputSchema"`
	}
	type mcpListResponse struct {
		Tools []mcpTool `json:"tools"`
	}
	var list []mcpTool
	for _, t := range tools {
		list = append(list, mcpTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
	}
	return mcpListResponse{Tools: list}, nil
}

func (m *MCPToolAdapter) ToUniversalToolCalls(raw any) ([]UniversalToolCall, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var invocation struct {
		Params struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments"`
		} `json:"params"`
	}
	if err := json.Unmarshal(b, &invocation); err != nil {
		return nil, err
	}
	return []UniversalToolCall{
		{
			ID:        fmt.Sprintf("mcp_call_%s", invocation.Params.Name),
			Name:      invocation.Params.Name,
			Arguments: invocation.Params.Arguments,
			RawJSON:   SerializeArguments(invocation.Params.Arguments),
		},
	}, nil
}

func (m *MCPToolAdapter) FromUniversalToolCalls(calls []UniversalToolCall) (any, error) {
	type mcpParams struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	type mcpCall struct {
		Method string    `json:"method"`
		Params mcpParams `json:"params"`
	}
	out := make([]mcpCall, 0, len(calls))
	for _, c := range calls {
		out = append(out, mcpCall{
			Method: "tools/call",
			Params: mcpParams{
				Name:      c.Name,
				Arguments: c.Arguments,
			},
		})
	}
	return out, nil
}

func (m *MCPToolAdapter) ToUniversalToolResults(raw any) ([]UniversalToolResult, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var res struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(b, &res); err != nil {
		return nil, err
	}
	var textParts []string
	for _, c := range res.Content {
		if c.Type == "text" {
			textParts = append(textParts, c.Text)
		}
	}
	outText := strings.Join(textParts, "\n")
	var uErr *UniversalToolError
	if res.IsError {
		uErr = &UniversalToolError{Code: "mcp_error", Message: outText, Retryable: true, Provider: "mcp"}
	}
	return []UniversalToolResult{
		{
			Success: !res.IsError,
			Output:  outText,
			Error:   uErr,
		},
	}, nil
}

func (m *MCPToolAdapter) FromUniversalToolResults(results []UniversalToolResult) (any, error) {
	type mcpContentItem struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type mcpResult struct {
		Content []mcpContentItem `json:"content"`
		IsError bool             `json:"isError,omitempty"`
	}
	var items []mcpContentItem
	isError := false
	for _, r := range results {
		items = append(items, mcpContentItem{Type: "text", Text: r.Output})
		if !r.Success {
			isError = true
		}
	}
	return mcpResult{Content: items, IsError: isError}, nil
}

// ============================================================================
// 5. Agent Text-Loop Adapter (<tool_call> blocks)
// ============================================================================

type TextLoopToolAdapter struct{}

var reToolCallXML = regexp.MustCompile(`(?s)<tool_call>\s*({.*?})\s*</tool_call>`)
var reToolCallFenced = regexp.MustCompile("(?s)```tool_call\\s*({.*?})\\s*```")

func NewTextLoopToolAdapter() *TextLoopToolAdapter {
	return &TextLoopToolAdapter{}
}

func (t *TextLoopToolAdapter) ParseTextCalls(text string) []UniversalToolCall {
	var out []UniversalToolCall

	// 1. Check <tool_call>...</tool_call>
	matches := reToolCallXML.FindAllStringSubmatch(text, -1)
	for _, m := range matches {
		if len(m) > 1 {
			var parsed struct {
				Name      string                 `json:"name"`
				Arguments map[string]interface{} `json:"arguments"`
			}
			if err := json.Unmarshal([]byte(m[1]), &parsed); err == nil && parsed.Name != "" {
				out = append(out, UniversalToolCall{
					ID:        fmt.Sprintf("text_call_%s", parsed.Name),
					Name:      parsed.Name,
					Arguments: parsed.Arguments,
					RawJSON:   m[1],
				})
			}
		}
	}

	// 2. Check ```tool_call ... ```
	fenced := reToolCallFenced.FindAllStringSubmatch(text, -1)
	for _, m := range fenced {
		if len(m) > 1 {
			var parsed struct {
				Name      string                 `json:"name"`
				Arguments map[string]interface{} `json:"arguments"`
			}
			if err := json.Unmarshal([]byte(m[1]), &parsed); err == nil && parsed.Name != "" {
				out = append(out, UniversalToolCall{
					ID:        fmt.Sprintf("fenced_call_%s", parsed.Name),
					Name:      parsed.Name,
					Arguments: parsed.Arguments,
					RawJSON:   m[1],
				})
			}
		}
	}

	return out
}

func (t *TextLoopToolAdapter) FormatTextCall(call UniversalToolCall) string {
	payload := map[string]interface{}{
		"name":      call.Name,
		"arguments": call.Arguments,
	}
	b, _ := json.Marshal(payload)
	return fmt.Sprintf("<tool_call>%s</tool_call>", string(b))
}
