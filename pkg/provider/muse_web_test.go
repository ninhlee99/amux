package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"amux-accounts/pkg/muse"
	"amux-accounts/pkg/types"
)

func collectMuse(t *testing.T, ch <-chan types.StreamChunk) (string, []types.ToolCall, error) {
	t.Helper()
	var sb strings.Builder
	var calls []types.ToolCall
	for c := range ch {
		if c.Error != nil {
			return sb.String(), calls, c.Error
		}
		sb.WriteString(c.Content)
		calls = append(calls, c.ToolCalls...)
	}
	return sb.String(), calls, nil
}

func TestMuseWeb_StreamsDeltasAsAppendOnlyChunks(t *testing.T) {
	var got muse.ChatOptions
	a := &MuseWebAdapter{AdapterID: "muse:web:01"}
	a.chat = func(_ context.Context, prompt string, o muse.ChatOptions) (*muse.ChatResult, error) {
		got = o
		if strings.HasSuffix(prompt, "Assistant:") {
			t.Errorf("role-play cue leaked into Muse prompt: %q", prompt)
		}
		o.OnDelta("Hello")
		o.OnDelta("Hello, world")
		return &muse.ChatResult{Reply: "Hello, world!", ThreadURL: "https://muse.ai/thread/x"}, nil
	}
	ch, err := a.SendMessageStream(context.Background(), &types.ChatRequest{
		Messages:    []types.ChatMessage{{Role: "user", Content: "hi"}},
		FullContext: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	text, _, err := collectMuse(t, ch)
	if err != nil {
		t.Fatal(err)
	}
	if text != "Hello, world!" {
		t.Fatalf("text = %q", text)
	}
	if !got.NewThread {
		t.Fatal("FullContext requests must run on a fresh thread")
	}
}

func TestMuseWeb_FailureBeforeOutputIsSynchronousForFailover(t *testing.T) {
	a := &MuseWebAdapter{AdapterID: "muse:web:01"}
	a.chat = func(context.Context, string, muse.ChatOptions) (*muse.ChatResult, error) {
		return nil, errors.New("muse: composer not ready (missing) — run `amux login muse` if signed out")
	}
	_, err := a.SendMessageStream(context.Background(), &types.ChatRequest{
		Messages: []types.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if !errors.Is(err, types.ErrAuthentication) {
		t.Fatalf("err = %v, want ErrAuthentication", err)
	}
}

func TestMuseWeb_ContinuesProjectThreadWhenNotFullContext(t *testing.T) {
	var calls []muse.ChatOptions
	a := &MuseWebAdapter{AdapterID: "muse:web:test-thread"}
	a.chat = func(_ context.Context, _ string, o muse.ChatOptions) (*muse.ChatResult, error) {
		calls = append(calls, o)
		return &muse.ChatResult{Reply: "ok", ThreadURL: "https://muse.ai/thread/abc"}, nil
	}
	req := &types.ChatRequest{Messages: []types.ChatMessage{{Role: "user", Content: "one"}}}
	for i := 0; i < 2; i++ {
		ch, err := a.SendMessageStream(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := collectMuse(t, ch); err != nil {
			t.Fatal(err)
		}
	}
	if !calls[0].NewThread {
		t.Fatal("first turn should open a new thread")
	}
	if calls[1].NewThread || calls[1].Chat != "https://muse.ai/thread/abc" {
		t.Fatalf("second turn should continue the thread, got %+v", calls[1])
	}
}

func TestMuseWeb_ToolCallEmulation(t *testing.T) {
	a := &MuseWebAdapter{AdapterID: "muse:web:01"}
	a.chat = func(_ context.Context, prompt string, o muse.ChatOptions) (*muse.ChatResult, error) {
		if !strings.Contains(prompt, "<tool_call>") {
			t.Errorf("tool protocol missing from prompt")
		}
		return &muse.ChatResult{Reply: "<tool_call>\n{\"name\":\"Bash\",\"arguments\":{\"command\":\"ls\"}}\n</tool_call>"}, nil
	}
	ch, err := a.SendMessageStream(context.Background(), &types.ChatRequest{
		Messages:    []types.ChatMessage{{Role: "user", Content: "list files"}},
		Tools:       []types.ToolDef{{Name: "Bash", InputSchema: []byte(`{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`)}},
		FullContext: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, calls, err := collectMuse(t, ch)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].Name != "Bash" || !strings.Contains(calls[0].Arguments, "ls") {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestMuseWeb_BuildFromConfig(t *testing.T) {
	ad, err := BuildAdapter(ProviderConfig{ID: "muse:web:01", Type: "muse_web", Priority: PriorityWebMuse, CDPEndpoint: "http://127.0.0.1:9222"})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := ad.(*MuseWebAdapter)
	if !ok || m.Endpoint != "http://127.0.0.1:9222" || m.Model() != muse.DefaultModel {
		t.Fatalf("adapter = %#v", ad)
	}
	if InferIDE("muse_web", "", "muse:web:01") != "web" {
		t.Fatal("muse_web must be a web-tier account")
	}
	if !(ProviderConfig{Type: "muse_web", CDPEndpoint: "http://127.0.0.1:9222"}).HasCredentials() {
		t.Fatal("an explicit CDP endpoint counts as configured")
	}
}
