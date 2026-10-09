package provider

import (
	"context"
	"strings"
	"testing"

	"amux-accounts/pkg/types"
)

func chunks(cs ...types.StreamChunk) <-chan types.StreamChunk {
	ch := make(chan types.StreamChunk, len(cs))
	for _, c := range cs {
		ch <- c
	}
	close(ch)
	return ch
}

func TestSendWithWebNudge_RetriesStallOnce(t *testing.T) {
	req := &types.ChatRequest{
		Tools:    []types.ToolDef{{Name: "Bash"}},
		Messages: []types.ChatMessage{{Role: "user", Content: "run the remaining steps"}},
	}
	var sent []*types.ChatRequest
	send := func(_ context.Context, r *types.ChatRequest) (<-chan types.StreamChunk, error) {
		sent = append(sent, r)
		if len(sent) == 1 {
			return chunks(types.StreamChunk{Content: "I need to continue by running the remaining required tool steps."}, types.StreamChunk{Done: true}), nil
		}
		return chunks(types.StreamChunk{ToolCalls: []types.ToolCall{{ID: "t1", Name: "Bash", Arguments: `{"command":"ls"}`}}, Done: true}), nil
	}
	out, err := sendWithWebNudge(context.Background(), req, send)
	if err != nil {
		t.Fatal(err)
	}
	var got []types.StreamChunk
	for c := range out {
		got = append(got, c)
	}
	if len(sent) != 2 {
		t.Fatalf("want one retry, sent %d", len(sent))
	}
	if len(got) != 1 || len(got[0].ToolCalls) != 1 {
		t.Fatalf("client must see only the retried tool call, got %+v", got)
	}
	retry := sent[1]
	last := retry.Messages[len(retry.Messages)-1]
	if last.Role != "user" || !strings.Contains(last.Content, "<tool_call>") || !strings.Contains(last.Content, "run the remaining steps") {
		t.Fatalf("nudge turn must restate protocol and task: %+v", last)
	}
	if retry.ClientMessages != 1 || ClientHistoryMark(retry) != HistoryMarkOf(req.Messages) {
		t.Fatal("thread checkpoint must fingerprint the client's history only")
	}
}

func TestSendWithWebNudge_PassesAnswersThrough(t *testing.T) {
	req := &types.ChatRequest{Tools: []types.ToolDef{{Name: "Bash"}}, Messages: []types.ChatMessage{{Role: "user", Content: "what is 2+2"}}}
	calls := 0
	send := func(_ context.Context, r *types.ChatRequest) (<-chan types.StreamChunk, error) {
		calls++
		return chunks(types.StreamChunk{Content: "4"}, types.StreamChunk{Done: true}), nil
	}
	out, _ := sendWithWebNudge(context.Background(), req, send)
	var text string
	for c := range out {
		text += c.Content
	}
	if calls != 1 || text != "4" {
		t.Fatalf("answer must pass through unchanged: calls=%d text=%q", calls, text)
	}
}
