package metrics

import (
	"strings"
	"testing"
)

func TestMetrics_PrometheusRender(t *testing.T) {
	reg := NewRegistry()

	reg.IncRequest("claude-code", "anthropic", "ok")
	reg.IncRequest("claude-code", "anthropic", "ok")
	reg.IncRequest("cursor", "openai", "err")

	reg.ObserveDuration("claude-code", "anthropic", 100)
	reg.ObserveDuration("claude-code", "anthropic", 200)

	reg.AddTokens("claude:01", "in", 500)
	reg.AddTokens("claude:01", "out", 150)

	healths := []AccountHealthSummary{
		{Account: "claude:01", Provider: "anthropic", Score: 95.0},
	}

	out := reg.RenderPrometheus(2, healths)

	if !strings.Contains(out, "amux_active_sessions 2") {
		t.Fatalf("missing active sessions in output: %s", out)
	}
	if !strings.Contains(out, `amux_requests_total{client="claude-code",provider="anthropic",status="ok"} 2`) {
		t.Fatalf("missing request count in output: %s", out)
	}
	if !strings.Contains(out, `amux_request_duration_ms_avg{client="claude-code",provider="anthropic"} 150.00`) {
		t.Fatalf("missing duration avg in output: %s", out)
	}
	if !strings.Contains(out, `amux_tokens_total{account="claude:01",direction="in"} 500`) {
		t.Fatalf("missing input tokens in output: %s", out)
	}
	if !strings.Contains(out, `amux_account_health{account="claude:01",provider="anthropic"} 95.0`) {
		t.Fatalf("missing account health in output: %s", out)
	}
}
