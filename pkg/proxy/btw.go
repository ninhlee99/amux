package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// BtwQueue is a thread-safe, size-bounded queue of "by-the-way" messages
// injected into the next outgoing LLM request while an agent is running.
// Users send messages via `am btw <text>` → POST /_am/btw → queue.
// The bridge layer drains the queue before forwarding each turn.
type BtwQueue struct {
	mu       sync.Mutex
	messages []btwEntry
}

type btwEntry struct {
	Text string
	At   time.Time
}

// maxBtwMessages caps queue depth so stale messages don't accumulate when
// the agent is idle for a long time.
const maxBtwMessages = 20

// globalBtwQueue is the singleton used by the proxy server and bridge.
// Exposed via GetGlobalBtwQueue() so bridge can drain it.
var globalBtwQueue = &BtwQueue{}

// GetGlobalBtwQueue returns the singleton BtwQueue used by the proxy.
func GetGlobalBtwQueue() *BtwQueue { return globalBtwQueue }

// Push adds a message to the queue (bounded, thread-safe).
func (q *BtwQueue) Push(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.messages) >= maxBtwMessages {
		// Drop oldest to stay bounded.
		q.messages = q.messages[1:]
	}
	q.messages = append(q.messages, btwEntry{Text: text, At: time.Now()})
}

// Drain returns all pending messages and empties the queue atomically.
// Returns nil when empty.
func (q *BtwQueue) Drain() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.messages) == 0 {
		return nil
	}
	out := make([]string, len(q.messages))
	for i, e := range q.messages {
		out[i] = e.Text
	}
	q.messages = q.messages[:0]
	return out
}

// Len returns current queue depth (for status reporting).
func (q *BtwQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.messages)
}

// HandleBtw is the HTTP handler for POST /_am/btw.
// Body: JSON {"text":"..."} OR raw text in query param ?text=...
// Also handles GET /_am/btw for status.
func HandleBtw(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodGet {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"pending": globalBtwQueue.Len(),
		})
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	text := strings.TrimSpace(r.URL.Query().Get("text"))
	if text == "" {
		body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
		if err != nil {
			http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
			return
		}
		var req struct {
			Text string `json:"text"`
			Msg  string `json:"msg"`
		}
		if json.Unmarshal(body, &req) == nil {
			if req.Text != "" {
				text = req.Text
			} else if req.Msg != "" {
				text = req.Msg
			}
		}
		if text == "" {
			text = strings.TrimSpace(string(body))
		}
	}

	if text == "" {
		http.Error(w, `{"error":"empty message"}`, http.StatusBadRequest)
		return
	}

	globalBtwQueue.Push(text)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"queued":  text,
		"pending": globalBtwQueue.Len(),
	})
}

// BuildBtwInjection formats pending BTW messages as a system note to prepend
// to the user prompt. Returns "" when there are no pending messages.
func BuildBtwInjection(msgs []string) string {
	if len(msgs) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n[BTW — user note while you were working]\n")
	for i, m := range msgs {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, m))
	}
	sb.WriteString("[/BTW — continue your current task after noting the above]\n")
	return sb.String()
}
