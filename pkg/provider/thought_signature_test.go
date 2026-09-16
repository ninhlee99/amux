package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"amux-accounts/pkg/tools"
	"amux-accounts/pkg/types"
)

func TestOpenAIAdapter_CapturesThoughtSignature(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)

		deltaJSON, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{
				{
					"delta": map[string]any{
						"tool_calls": []map[string]any{
							{
								"index": 0,
								"id":    "call_gemini_sig_test",
								"type":  "function",
								"extra_content": map[string]any{
									"google": map[string]string{
										"thought_signature": "SIG_TEST_ENCRYPTED_123",
									},
								},
								"function": map[string]string{
									"name":      "Bash",
									"arguments": `{"command":"ls"}`,
								},
							},
						},
					},
				},
			},
		})
		fmt.Fprintf(w, "data: %s\n\n", deltaJSON)
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer ts.Close()

	adapter := &OpenAICompatibleAdapter{
		AdapterID:   "gemini:api:test",
		BaseURL:     ts.URL,
		TargetModel: "gemini-3.6-flash",
		HTTPClient:  ts.Client(),
	}

	ch, err := adapter.SendMessageStream(context.Background(), &types.ChatRequest{
		Model: "gemini-3.6-flash",
	})
	if err != nil {
		t.Fatalf("SendMessageStream failed: %v", err)
	}

	var calls []types.ToolCall
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("unexpected chunk error: %v", chunk.Error)
		}
		if len(chunk.ToolCalls) > 0 {
			calls = append(calls, chunk.ToolCalls...)
		}
	}

	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].ThoughtSignature != "SIG_TEST_ENCRYPTED_123" {
		t.Errorf("expected ThoughtSignature SIG_TEST_ENCRYPTED_123, got %q", calls[0].ThoughtSignature)
	}

	// Verify it was recorded in global cache for subsequent turns
	cachedSig := tools.LookupThoughtSignature("call_gemini_sig_test")
	if cachedSig != "SIG_TEST_ENCRYPTED_123" {
		t.Errorf("expected cached signature SIG_TEST_ENCRYPTED_123, got %q", cachedSig)
	}
}

func TestOpenAIAdapter_ThoughtSignatureFallback(t *testing.T) {
	var attempts int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&attempts, 1)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)

		if count == 1 {
			// First attempt with gemini-3.6-flash: simulate Google 400 error
			if body["model"] != "gemini-3.6-flash" {
				t.Errorf("attempt 1: expected model gemini-3.6-flash, got %v", body["model"])
			}
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintln(w, `{"error":{"message":"Function call is missing a thought_signature in functionCall parts."}}`)
			return
		}

		// Fallback attempt: should switch to gemini-2.5-flash
		if body["model"] != "gemini-2.5-flash" {
			t.Errorf("attempt 2: expected fallback model gemini-2.5-flash, got %v", body["model"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		fmt.Fprintf(w, "data: %s\n\n", `{"choices":[{"delta":{"content":"recovered without signature"}}]}`)
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer ts.Close()

	adapter := &OpenAICompatibleAdapter{
		AdapterID:   "gemini:api:test",
		BaseURL:     ts.URL,
		TargetModel: "gemini-3.6-flash",
		HTTPClient:  ts.Client(),
	}

	ch, err := adapter.SendMessageStream(context.Background(), &types.ChatRequest{
		Model: "gemini-3.6-flash",
	})
	if err != nil {
		t.Fatalf("SendMessageStream should have succeeded via fallback, got err: %v", err)
	}

	var content string
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("unexpected chunk error: %v", chunk.Error)
		}
		content += chunk.Content
	}

	if content != "recovered without signature" {
		t.Fatalf("expected fallback response, got %q", content)
	}
	if atomic.LoadInt32(&attempts) != 2 {
		t.Fatalf("expected 2 attempts (initial 400 + fallback), got %d", atomic.LoadInt32(&attempts))
	}
}
