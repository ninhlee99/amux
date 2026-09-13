package proxy_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"amux-accounts/pkg/proxy"
)

// TestBtwQueue_PushDrain verifies the in-memory queue semantics.
func TestBtwQueue_PushDrain(t *testing.T) {
	q := &proxy.BtwQueue{}

	// Empty drain
	if got := q.Drain(); len(got) != 0 {
		t.Fatalf("expected empty drain, got %v", got)
	}

	// Push and drain
	q.Push("hello agent")
	q.Push("also check the tests")
	msgs := q.Drain()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d: %v", len(msgs), msgs)
	}
	if msgs[0] != "hello agent" || msgs[1] != "also check the tests" {
		t.Fatalf("unexpected messages: %v", msgs)
	}

	// Queue is empty after drain
	if got := q.Drain(); len(got) != 0 {
		t.Fatalf("expected empty after drain, got %v", got)
	}
}

// TestBtwQueue_Bounded verifies the queue drops oldest when full.
func TestBtwQueue_Bounded(t *testing.T) {
	q := &proxy.BtwQueue{}
	for i := 0; i < 25; i++ {
		q.Push("msg")
	}
	msgs := q.Drain()
	if len(msgs) > 20 {
		t.Fatalf("queue should be bounded at 20, got %d", len(msgs))
	}
}

// TestBtwQueue_EmptyIgnored verifies empty/whitespace messages are ignored.
func TestBtwQueue_EmptyIgnored(t *testing.T) {
	q := &proxy.BtwQueue{}
	q.Push("")
	q.Push("   ")
	if q.Len() != 0 {
		t.Fatalf("expected 0 after empty pushes, got %d", q.Len())
	}
}

// TestHandleBtw_PostJSON verifies the HTTP handler accepts JSON body.
func TestHandleBtw_PostJSON(t *testing.T) {
	// Reset global queue.
	proxy.GetGlobalBtwQueue().Drain()

	body, _ := json.Marshal(map[string]string{"text": "check the lint output too"})
	req := httptest.NewRequest(http.MethodPost, "/_am/btw", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	proxy.HandleBtw(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var res map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("invalid json response: %v", err)
	}
	if res["ok"] != true {
		t.Fatalf("expected ok=true: %v", res)
	}
	if pending, _ := res["pending"].(float64); pending != 1 {
		t.Fatalf("expected pending=1, got %v", res["pending"])
	}

	// Drain and verify
	msgs := proxy.GetGlobalBtwQueue().Drain()
	if len(msgs) != 1 || msgs[0] != "check the lint output too" {
		t.Fatalf("unexpected queued messages: %v", msgs)
	}
}

// TestHandleBtw_PostQueryParam verifies the HTTP handler accepts ?text= query param.
func TestHandleBtw_PostQueryParam(t *testing.T) {
	proxy.GetGlobalBtwQueue().Drain()

	req := httptest.NewRequest(http.MethodPost, "/_am/btw?text=also+run+the+tests", nil)
	w := httptest.NewRecorder()

	proxy.HandleBtw(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	msgs := proxy.GetGlobalBtwQueue().Drain()
	if len(msgs) != 1 || msgs[0] != "also run the tests" {
		t.Fatalf("unexpected: %v", msgs)
	}
}

// TestHandleBtw_GetStatus verifies GET returns pending count.
func TestHandleBtw_GetStatus(t *testing.T) {
	proxy.GetGlobalBtwQueue().Drain()
	proxy.GetGlobalBtwQueue().Push("a pending message")

	req := httptest.NewRequest(http.MethodGet, "/_am/btw", nil)
	w := httptest.NewRecorder()
	proxy.HandleBtw(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var res map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if pending, _ := res["pending"].(float64); pending != 1 {
		t.Fatalf("expected pending=1, got %v", res["pending"])
	}
	proxy.GetGlobalBtwQueue().Drain()
}

// TestHandleBtw_EmptyBodyRejected verifies that an empty POST body returns 400.
func TestHandleBtw_EmptyBodyRejected(t *testing.T) {
	proxy.GetGlobalBtwQueue().Drain()

	req := httptest.NewRequest(http.MethodPost, "/_am/btw", strings.NewReader(""))
	w := httptest.NewRecorder()
	proxy.HandleBtw(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty body, got %d", w.Code)
	}
}
