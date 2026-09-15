package router

import (
	"context"
	"os"
	"testing"

	"amux-accounts/pkg/types"
)

func TestApplyTaskClassification_CodingSkipsAutoThink(t *testing.T) {
	req := &types.ChatRequest{
		Messages: []types.ChatMessage{
			{Role: "user", Content: "Please implement a new login endpoint and write tests for it."},
		},
	}
	applyTaskClassification(req)
	if req.TaskKind != TaskCoding {
		t.Fatalf("kind=%s", req.TaskKind)
	}
	if req.Thinking {
		t.Fatal("coding must not auto-enable thinking")
	}
	if req.TargetTier == "pro" {
		t.Fatal("coding must not auto-escalate pro")
	}
}

func TestApplyTaskClassification_StackStillThinks(t *testing.T) {
	req := &types.ChatRequest{
		Messages: []types.ChatMessage{
			{Role: "user", Content: "fix this crash:\npanic: runtime error\n[signal SIGSEGV]"},
		},
	}
	applyTaskClassification(req)
	if !req.Thinking {
		t.Fatal("crash/fix must keep thinking")
	}
	if req.ThinkingBudget != 512 {
		t.Fatalf("budget=%d", req.ThinkingBudget)
	}
}

func TestApplyTaskClassification_ReviewLowerBudget(t *testing.T) {
	req := &types.ChatRequest{
		Messages: []types.ChatMessage{
			{Role: "user", Content: "Please code review this PR:\n```\ndiff --git a/x.go\n@@ -1 +1 @@\n-old\n+new\n```"},
		},
	}
	applyTaskClassification(req)
	if req.TaskKind != TaskReview {
		t.Fatalf("kind=%s", req.TaskKind)
	}
	if !req.Thinking || req.ThinkingBudget != 512 {
		t.Fatalf("thinking=%v budget=%d", req.Thinking, req.ThinkingBudget)
	}
}

func TestWebPolicy_EnvOverrides(t *testing.T) {
	prev := EffectiveWebPolicy()
	t.Cleanup(func() {
		_ = os.Unsetenv("AM_WEB_POLICY")
		SetWebPolicy(prev)
	})
	SetWebPolicy(WebPolicyLastResort)
	_ = os.Setenv("AM_WEB_POLICY", WebPolicyForce)
	if EffectiveWebPolicy() != WebPolicyForce {
		t.Fatal(EffectiveWebPolicy())
	}
}

func TestSkipTextOnly_ForceAllowsWeb(t *testing.T) {
	prev := EffectiveWebPolicy()
	t.Cleanup(func() {
		_ = os.Unsetenv("AM_WEB_POLICY")
		SetWebPolicy(prev)
	})
	_ = os.Unsetenv("AM_WEB_POLICY")
	SetWebPolicy(WebPolicyForce)
	web := stubNoTools{id: "chatgpt:01"}
	req := &types.ChatRequest{Tools: []types.ToolDef{{Name: "Bash"}}}
	if skipTextOnly(web, req, true) {
		t.Fatal("force must not skip web")
	}
	SetWebPolicy(WebPolicyLastResort)
	if !skipTextOnly(web, req, true) {
		t.Fatal("last_resort must skip web when native exists")
	}
}

type stubNoTools struct{ id string }

func (s stubNoTools) ID() string       { return s.id }
func (s stubNoTools) Priority() int    { return 1 }
func (s stubNoTools) SupportsTools() bool { return false }
func (s stubNoTools) SendMessageStream(ctx context.Context, req *types.ChatRequest) (<-chan types.StreamChunk, error) {
	return nil, nil
}
