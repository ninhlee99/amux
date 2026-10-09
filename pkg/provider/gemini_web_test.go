package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
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
