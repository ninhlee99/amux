package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"amux-accounts/pkg/types"
)

func TestGeminiParseEnvelope_TextAndMeta(t *testing.T) {
	inner := []any{
		nil,
		[]any{"c_abc", "r_1", "rcid_1", nil, nil, nil, nil, nil, nil, ""},
		nil,
		nil,
		[]any{
			[]any{"rcid_1", []any{"Hello from Gemini"}, nil},
		},
	}
	innerJSON, _ := json.Marshal(inner)
	envelope := []any{"wrb.fr", nil, string(innerJSON)}
	text, meta := geminiParseEnvelope(envelope)
	if text != "Hello from Gemini" {
		t.Fatalf("text=%q", text)
	}
	if len(meta) < 3 || meta[0] != "c_abc" {
		t.Fatalf("meta=%v", meta)
	}
}

func TestGeminiDrillInt_ShortEnvelope(t *testing.T) {
	if got := geminiDrillInt([]any{"a", "b", "c"}, 5, 2, 0, 1, 0); got != 0 {
		t.Fatalf("want 0, got %d", got)
	}
	if got := geminiEnvelopeError([]any{"wrb.fr", nil, "x"}); got != 0 {
		t.Fatalf("short envelope error=%d", got)
	}
}

func TestUnlinkGeminiAutolinks(t *testing.T) {
	in := "base [http://127.0.0.1:18789](http://127.0.0.1:18789) and [docs](https://example.com)"
	want := "base http://127.0.0.1:18789 and [docs](https://example.com)"
	if got := unlinkGeminiAutolinks(in); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	call := `{"name":"Write","arguments":{"file_path":"~/.local/share/open-pr/[github.com/o/r/p.json](https://github.com/o/r/p.json)"}}`
	if got := unlinkGeminiAutolinks(call); got != `{"name":"Write","arguments":{"file_path":"~/.local/share/open-pr/github.com/o/r/p.json"}}` {
		t.Fatalf("scheme-less autolink in tool args not undone: %s", got)
	}
}

type geminiFrames []string

func (f geminiFrames) RoundTrip(*http.Request) (*http.Response, error) {
	var b strings.Builder
	b.WriteString(")]}'\n")
	for _, text := range f {
		inner, _ := json.Marshal([]any{nil, []any{"c_1", "r_1", "rc_1"}, nil, nil, []any{[]any{"rc_1", []any{text}}}})
		line, _ := json.Marshal([]any{[]any{"wrb.fr", nil, string(inner)}})
		b.Write(line)
		b.WriteByte('\n')
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(b.String())), Header: http.Header{}}, nil
}

// Gemini can restart an answer mid-stream; the rewrite is the answer and the
// abandoned draft must not be glued to it.
func TestStreamGenerate_MidStreamRewrite(t *testing.T) {
	frames := geminiFrames{"<tool_call> {\"name\":\"Ba", "<tool_call> {\"name\":\"Bash\",\"argu", "## Review", "## Review\nAll good."}
	a := &GeminiWebAdapter{AdapterID: "gemini:web:t", HTTPClient: &http.Client{Transport: frames}}
	text, _, err := a.streamGenerate(context.Background(), "p", nil)
	if err != nil {
		t.Fatal(err)
	}
	if text != "## Review\nAll good." {
		t.Fatalf("final must be the latest snapshot, got %q", text)
	}
	var streamed strings.Builder
	if _, _, err := a.streamGenerate(context.Background(), "p", nil, func(d string) { streamed.WriteString(d) }); err != nil {
		t.Fatal(err)
	}
	if got := streamed.String(); strings.Contains(got, "<tool_call> {\"name\":\"Bash\",\"argu## Review") || !strings.HasSuffix(got, "## Review\nAll good.") {
		t.Fatalf("streamed text glued draft and rewrite: %q", got)
	}
}

// heartbeatOnly mimics a Gemini request that was accepted but never started:
// only {"37":[n]} heartbeats, and the body stays open until closed.
type heartbeatOnly struct{}

func (heartbeatOnly) RoundTrip(r *http.Request) (*http.Response, error) {
	pr, pw := io.Pipe()
	go func() {
		pw.Write([]byte(")]}'\n\n"))
		for i := 1; ; i++ {
			line := fmt.Sprintf(`[["wrb.fr",null,"[{\"37\":[%d]}]"]]`+"\n", i)
			if _, err := pw.Write([]byte(line)); err != nil {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	return &http.Response{StatusCode: 200, Body: pr, Header: http.Header{}}, nil
}

func TestStreamGenerate_HeartbeatOnlyStalls(t *testing.T) {
	old := geminiAckTimeout
	geminiAckTimeout = 150 * time.Millisecond
	t.Cleanup(func() { geminiAckTimeout = old })

	a := &GeminiWebAdapter{AdapterID: "gemini:web:t", HTTPClient: &http.Client{Transport: heartbeatOnly{}}}
	start := time.Now()
	_, _, err := a.streamGenerate(context.Background(), "p", nil)
	if !errors.Is(err, errGeminiStalled) {
		t.Fatalf("want errGeminiStalled, got %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("stall detection took %s", time.Since(start))
	}
}

// Slow but live answers are not cut: progress frames keep resetting the watch.
func TestStreamGenerate_ProgressKeepsStreamAlive(t *testing.T) {
	old := geminiAckTimeout
	geminiAckTimeout = 150 * time.Millisecond
	t.Cleanup(func() { geminiAckTimeout = old })

	frames := geminiFrames{"a", "ab", "abc"}
	a := &GeminiWebAdapter{AdapterID: "gemini:web:t", HTTPClient: &http.Client{Transport: frames}}
	text, _, err := a.streamGenerate(context.Background(), "p", nil)
	if err != nil || text != "abc" {
		t.Fatalf("got %q, %v", text, err)
	}
}

// Gemini's generic failure text is an error, not an answer, and is never
// streamed to the client.
func TestStreamGenerate_CannedErrorIsError(t *testing.T) {
	frames := geminiFrames{"Sorry, something", "Sorry, something went wrong. Please try your request again."}
	a := &GeminiWebAdapter{AdapterID: "gemini:web:t", HTTPClient: &http.Client{Transport: frames}}
	var streamed strings.Builder
	_, _, err := a.streamGenerate(context.Background(), "p", nil, func(d string) { streamed.WriteString(d) })
	if !errors.Is(err, errGeminiCanned) {
		t.Fatalf("want errGeminiCanned, got %v", err)
	}
	if streamed.Len() != 0 {
		t.Fatalf("canned error leaked to client: %q", streamed.String())
	}
	if isGeminiCannedError("Sorry, something went wrong in your code: the loop never ends because i is never incremented, so fix the increment and rerun the tests to confirm the behaviour is correct now.") {
		t.Fatalf("a real answer that starts like the canned text must not be dropped")
	}
}

// noHeaders mimics Gemini holding the response headers back.
type noHeaders struct{}

func (noHeaders) RoundTrip(r *http.Request) (*http.Response, error) {
	<-r.Context().Done()
	return nil, r.Context().Err()
}

func TestStreamGenerate_NoHeadersStalls(t *testing.T) {
	old := geminiAckTimeout
	geminiAckTimeout = 100 * time.Millisecond
	t.Cleanup(func() { geminiAckTimeout = old })

	a := &GeminiWebAdapter{AdapterID: "gemini:web:t", HTTPClient: &http.Client{Transport: noHeaders{}}}
	if _, _, err := a.streamGenerate(context.Background(), "p", nil); !errors.Is(err, errGeminiStalled) {
		t.Fatalf("want errGeminiStalled, got %v", err)
	}
}

// Two stalls in a row (the retry runs on a fresh chat) mean Google is
// throttling the account: report a rate limit with a cooldown so the router
// moves the turn to another account.
func TestGeminiSend_RepeatedStallIsThrottle(t *testing.T) {
	old := geminiAckTimeout
	geminiAckTimeout = 50 * time.Millisecond
	t.Cleanup(func() { geminiAckTimeout = old })

	a := &GeminiWebAdapter{AdapterID: "gemini:web:t", Cookies: "__Secure-1PSID=x", inited: true, accessToken: "tok",
		HTTPClient: &http.Client{Transport: heartbeatOnly{}}}
	ch, err := a.SendMessageStream(context.Background(), &types.ChatRequest{Messages: []types.ChatMessage{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	var got error
	for c := range ch {
		if c.Error != nil {
			got = c.Error
		}
	}
	if !errors.Is(got, types.ErrRateLimitReached) || types.ExtractRetryAfter(got) != geminiThrottleCooldown {
		t.Fatalf("want throttle rate limit with cooldown, got %v (retry %s)", got, types.ExtractRetryAfter(got))
	}
}
