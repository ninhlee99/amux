package router

import (
	"context"
	"testing"

	"amux-accounts/pkg/types"
)

type freeWebStub struct {
	id   string
	plan string
}

func (f freeWebStub) ID() string       { return f.id }
func (f freeWebStub) Priority() int    { return 9 }
func (f freeWebStub) Plan() string     { return f.plan }
func (f freeWebStub) SupportsTools() bool { return false }
func (f freeWebStub) SendMessageStream(ctx context.Context, req *types.ChatRequest) (<-chan types.StreamChunk, error) {
	return nil, nil
}

func TestSkipFreeWebHardTask(t *testing.T) {
	free := freeWebStub{id: "claude:web:01", plan: "free"}
	plus := freeWebStub{id: "chatgpt:01", plan: "plus"}
	coding := &types.ChatRequest{TaskKind: TaskCoding}
	review := &types.ChatRequest{TaskKind: TaskReview}

	if !skipFreeWebHardTask(free, coding, true) {
		t.Fatal("free web must skip for coding when stronger exists")
	}
	if skipFreeWebHardTask(free, coding, false) {
		t.Fatal("free web must stay when it is last resort")
	}
	if skipFreeWebHardTask(free, review, true) {
		t.Fatal("review may use free web")
	}
	if skipFreeWebHardTask(plus, coding, true) {
		t.Fatal("plus web must not skip")
	}
}

func TestExtractTouchedFiles(t *testing.T) {
	msgs := []types.ChatMessage{{
		Role: "assistant",
		ToolCalls: []types.ToolCall{
			{Name: "Read", Arguments: `{"file_path":"pkg/cli/cli.go"}`},
			{Name: "Bash", Arguments: `{"command":"ls"}`},
		},
	}}
	got := extractTouchedFiles(msgs)
	if len(got) != 1 || got[0] != "pkg/cli/cli.go" {
		t.Fatalf("%v", got)
	}
}
