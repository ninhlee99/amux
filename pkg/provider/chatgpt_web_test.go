package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"amux-accounts/pkg/tools"
	"amux-accounts/pkg/types"
)

func TestChatGPTContainerCommand(t *testing.T) {
	cases := map[string]string{
		`bash -lc pwd && ls -la`:                       "pwd && ls -la",
		`bash -lc "git status --short"`:                "git status --short",
		`{"cmd":["bash","-lc","cat go.mod"]}`:          "cat go.mod",
		`{"cmd":["/bin/sh","-c","ls"],"timeout":1000}`: "ls",
		`{"cmd":["ls","-la"]}`:                         "ls -la",
		`{"cmd":"git log -1"}`:                         "git log -1",
		`ls -la`:                                       "ls -la",
		`   `:                                          "",
	}
	for in, want := range cases {
		if got := chatgptContainerCommand(in); got != want {
			t.Errorf("chatgptContainerCommand(%q) = %q, want %q", in, got, want)
		}
	}
}

func chatgptSSE(t *testing.T, msgs ...map[string]any) *http.Response {
	t.Helper()
	var sb strings.Builder
	for _, m := range msgs {
		b, err := json.Marshal(map[string]any{"conversation_id": "conv-1", "message": m})
		if err != nil {
			t.Fatal(err)
		}
		sb.WriteString("data: " + string(b) + "\n\n")
	}
	sb.WriteString("data: [DONE]\n\n")
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(sb.String()))}
}

// ChatGPT reaching for its own sandbox must become a client shell tool call,
// not a "the repo is not accessible / I have no tools" reply.
func TestStreamChatGPTWeb_ContainerExecBecomesClientToolCall(t *testing.T) {
	resp := chatgptSSE(t,
		map[string]any{
			"id": "m1", "author": map[string]string{"role": "assistant"}, "recipient": "container.exec",
			"status":  "finished_successfully",
			"content": map[string]any{"content_type": "code", "text": "bash -lc pwd && ls -la"},
		},
		map[string]any{
			"id": "m2", "author": map[string]string{"role": "tool"}, "recipient": "all",
			"status":  "finished_successfully",
			"content": map[string]any{"content_type": "execution_output", "text": "Command failed due to container ServerError."},
		},
		map[string]any{
			"id": "m3", "author": map[string]string{"role": "assistant"}, "recipient": "all",
			"status":  "finished_successfully",
			"content": map[string]any{"content_type": "text", "parts": []string{"I cannot access the repo in this session."}},
		},
	)
	a := &ChatGPTWebAdapter{AdapterID: "chatgpt:test"}
	req := &types.ChatRequest{
		Messages: []types.ChatMessage{{Role: "user", Content: "review the repo"}},
		Tools:    []types.ToolDef{{Name: "Bash", InputSchema: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`)}},
	}
	raw := make(chan types.StreamChunk)
	go streamChatGPTWeb(context.Background(), a, "/proj", "s1", HistoryMark{}, true, resp, raw)

	var text strings.Builder
	var calls []types.ToolCall
	for c := range tools.MaybeWrapWebStream(a.AdapterID, req, raw) {
		if c.Error != nil {
			t.Fatal(c.Error)
		}
		text.WriteString(c.Content)
		calls = append(calls, c.ToolCalls...)
	}
	if len(calls) != 1 || calls[0].Name != "Bash" {
		t.Fatalf("want one Bash tool call, got %+v (text %q)", calls, text.String())
	}
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(calls[0].Arguments), &args); err != nil || args.Command != "pwd && ls -la" {
		t.Fatalf("bad args %q: %v", calls[0].Arguments, err)
	}
	if strings.Contains(text.String(), "cannot access") {
		t.Fatalf("sandbox refusal leaked to client: %q", text.String())
	}
	if _, ok := a.convs().GetActive("/proj"); ok {
		t.Fatal("thread holding the sandbox error must not be reused")
	}
}

// Without client tools the sandbox call is ChatGPT's business; stream text as before.
func TestStreamChatGPTWeb_ContainerExecIgnoredWithoutClientTools(t *testing.T) {
	resp := chatgptSSE(t,
		map[string]any{
			"id": "m1", "author": map[string]string{"role": "assistant"}, "recipient": "container.exec",
			"status":  "finished_successfully",
			"content": map[string]any{"content_type": "code", "text": "bash -lc ls"},
		},
		map[string]any{
			"id": "m2", "author": map[string]string{"role": "assistant"}, "recipient": "all",
			"status":  "finished_successfully",
			"content": map[string]any{"content_type": "text", "parts": []string{"done"}},
		},
	)
	a := &ChatGPTWebAdapter{AdapterID: "chatgpt:test"}
	out := make(chan types.StreamChunk)
	go streamChatGPTWeb(context.Background(), a, "/proj", "s1", HistoryMark{}, false, resp, out)
	var text strings.Builder
	for c := range out {
		text.WriteString(c.Content)
	}
	if text.String() != "done" {
		t.Fatalf("got %q, want plain text reply", text.String())
	}
}
