// Package mcp is amux's Model Context Protocol server: newline-delimited
// JSON-RPC 2.0 over stdio, so any MCP host (Claude Code, Claude Desktop,
// Cursor, Codex, Gemini CLI, Antigravity, VS Code, Windsurf, opencode, …) can
// call the chat platforms amux manages as tools.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"sort"
	"strings"
	"sync"
)

// SupportedProtocolVersions, newest first.
var SupportedProtocolVersions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

// Tool is one MCP tool.
type Tool struct {
	Name        string
	Title       string
	Description string
	InputSchema map[string]any
	ReadOnly    bool
	// Handler returns a string (sent as text) or any JSON-serialisable value.
	// progress may be called with human-readable updates (no-op when the
	// client did not ask for progress).
	Handler func(ctx context.Context, args json.RawMessage, progress func(msg string)) (any, error)
}

// Server dispatches JSON-RPC requests to registered tools.
type Server struct {
	Name         string
	Version      string
	Instructions string
	Logger       *log.Logger

	mu       sync.Mutex
	tools    map[string]Tool
	inflight map[string]context.CancelFunc

	writeMu sync.Mutex
	enc     *json.Encoder
}

// NewServer returns an empty server.
func NewServer(name, version string) *Server {
	return &Server{Name: name, Version: version, tools: map[string]Tool{}, inflight: map[string]context.CancelFunc{},
		Logger: log.New(io.Discard, "", 0)}
}

// Register adds (or replaces) a tool.
func (s *Server) Register(t Tool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools[t.Name] = t
}

// Tools returns the registered tools sorted by name.
func (s *Server) Tools() []Tool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Tool, 0, len(s.tools))
	for _, t := range s.tools {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

// Serve reads requests from r and writes responses to w until r hits EOF or
// ctx is cancelled. Requests run concurrently, so a long tool call never
// blocks ping or cancellation.
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	s.enc = json.NewEncoder(w)
	s.enc.SetEscapeHTML(false)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	var wg sync.WaitGroup
	defer wg.Wait()
	for sc.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			// JSON-RPC batches were dropped from MCP in 2025-06-18; answer each.
			var batch []json.RawMessage
			if err := json.Unmarshal([]byte(line), &batch); err != nil {
				s.reply(nil, nil, &rpcError{codeParseError, "parse error"})
				continue
			}
			for _, raw := range batch {
				s.dispatch(ctx, raw, &wg)
			}
			continue
		}
		s.dispatch(ctx, []byte(line), &wg)
	}
	return sc.Err()
}

func (s *Server) dispatch(ctx context.Context, raw []byte, wg *sync.WaitGroup) {
	var m rpcMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		s.reply(nil, nil, &rpcError{codeParseError, "parse error"})
		return
	}
	if m.Method == "" {
		return // a response to something we never send; ignore
	}
	isNotification := len(m.ID) == 0 || string(m.ID) == "null"
	if isNotification {
		s.handleNotification(m)
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		res, rerr := s.handle(ctx, m)
		s.reply(m.ID, res, rerr)
	}()
}

func (s *Server) handleNotification(m rpcMessage) {
	switch m.Method {
	case "notifications/cancelled":
		var p struct {
			RequestID json.RawMessage `json:"requestId"`
		}
		if json.Unmarshal(m.Params, &p) == nil {
			s.mu.Lock()
			if cancel := s.inflight[string(p.RequestID)]; cancel != nil {
				cancel()
			}
			s.mu.Unlock()
		}
	default:
		// notifications/initialized and friends need no action.
	}
}

func (s *Server) reply(id json.RawMessage, result any, rerr *rpcError) {
	msg := map[string]any{"jsonrpc": "2.0"}
	if len(id) == 0 {
		msg["id"] = nil
	} else {
		msg["id"] = id
	}
	if rerr != nil {
		msg["error"] = rerr
	} else {
		if result == nil {
			result = map[string]any{}
		}
		msg["result"] = result
	}
	s.send(msg)
}

func (s *Server) send(msg any) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.enc.Encode(msg); err != nil {
		s.Logger.Printf("mcp: write: %v", err)
	}
}

func (s *Server) handle(ctx context.Context, m rpcMessage) (any, *rpcError) {
	switch m.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(m.Params, &p)
		version := SupportedProtocolVersions[0]
		for _, v := range SupportedProtocolVersions {
			if v == p.ProtocolVersion {
				version = v
				break
			}
		}
		res := map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": s.Name, "title": "amux — multi-platform AI gateway", "version": s.Version},
		}
		if s.Instructions != "" {
			res["instructions"] = s.Instructions
		}
		return res, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		var list []map[string]any
		for _, t := range s.Tools() {
			schema := t.InputSchema
			if schema == nil {
				schema = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			entry := map[string]any{"name": t.Name, "description": t.Description, "inputSchema": schema}
			if t.Title != "" {
				entry["title"] = t.Title
			}
			entry["annotations"] = map[string]any{"readOnlyHint": t.ReadOnly, "openWorldHint": true}
			list = append(list, entry)
		}
		return map[string]any{"tools": list}, nil
	case "tools/call":
		return s.callTool(ctx, m)
	case "resources/list":
		return map[string]any{"resources": []any{}}, nil
	case "prompts/list":
		return map[string]any{"prompts": []any{}}, nil
	}
	return nil, &rpcError{codeMethodNotFound, "method not found: " + m.Method}
}

func (s *Server) callTool(ctx context.Context, m rpcMessage) (any, *rpcError) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
		Meta      struct {
			ProgressToken json.RawMessage `json:"progressToken"`
		} `json:"_meta"`
	}
	if err := json.Unmarshal(m.Params, &p); err != nil || p.Name == "" {
		return nil, &rpcError{codeInvalidParams, "tools/call needs a tool name"}
	}
	s.mu.Lock()
	t, ok := s.tools[p.Name]
	s.mu.Unlock()
	if !ok {
		return nil, &rpcError{codeInvalidParams, "unknown tool: " + p.Name}
	}
	if len(p.Arguments) == 0 || string(p.Arguments) == "null" {
		p.Arguments = json.RawMessage("{}")
	}

	cctx, cancel := context.WithCancel(ctx)
	key := string(m.ID)
	s.mu.Lock()
	s.inflight[key] = cancel
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.inflight, key)
		s.mu.Unlock()
		cancel()
	}()

	progress := func(string) {}
	if len(p.Meta.ProgressToken) > 0 {
		var n int
		var pmu sync.Mutex
		progress = func(msg string) {
			pmu.Lock()
			n++
			cur := n
			pmu.Unlock()
			s.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/progress",
				"params": map[string]any{"progressToken": p.Meta.ProgressToken, "progress": cur, "message": msg}})
		}
	}

	out, err := safeCall(cctx, t, p.Arguments, progress)
	if err != nil {
		// Tool failures are results (isError) so the model can read and react.
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": err.Error()}}, "isError": true}, nil
	}
	return toolResult(out), nil
}

func safeCall(ctx context.Context, t Tool, args json.RawMessage, progress func(string)) (out any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%s panicked: %v", t.Name, r)
		}
	}()
	return t.Handler(ctx, args, progress)
}

func toolResult(out any) map[string]any {
	switch v := out.(type) {
	case string:
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": v}}}
	case nil:
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": "ok"}}}
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": fmt.Sprint(out)}}}
	}
	res := map[string]any{"content": []any{map[string]any{"type": "text", "text": string(b)}}}
	var obj map[string]any
	if json.Unmarshal(b, &obj) == nil {
		res["structuredContent"] = obj
	}
	return res
}

// DecodeArgs unmarshals tool arguments, reporting bad input clearly.
func DecodeArgs(raw json.RawMessage, v any) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("invalid arguments: %v", err)
	}
	return nil
}
