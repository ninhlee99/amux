package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"amux-accounts/pkg/types"
)

const (
	googleAIStudioBaseURL = "https://generativelanguage.googleapis.com/v1beta/openai"

	DefaultGeminiFlashModel = "gemini-3.8-flash"
	DefaultGeminiProModel   = "gemini-3.8-flash"
	// GeminiFreeRPMLimitPerKey is the Google AI Studio free tier limit of 15 RPM per key.
	GeminiFreeRPMLimitPerKey = 15
)

// googleAIStudioModelsURL is a var (not const) so tests can point it at an
// httptest server instead of the real Google endpoint.
var googleAIStudioModelsURL = "https://generativelanguage.googleapis.com/v1beta/models"

// GeminiAdapter wraps Google AI Studio's OpenAI-compatible endpoint with multi-key rotation.
type GeminiAdapter struct {
	AdapterID   string
	PriorityLvl int
	APIKey      string
	APIKeys     []string
	FlashModel  string
	ProModel    string
	TargetModel string
	HTTPClient  *http.Client

	keyIndex  uint64
	keyLimits sync.Map // map[string]time.Time (cooldown per key)
	wrapped   types.ProviderAdapter
}

func parseGeminiKeys(apiKey string) []string {
	var keys []string
	addKey := func(k string) {
		k = strings.TrimSpace(k)
		if k != "" && !strings.HasPrefix(k, "env:") {
			for _, existing := range keys {
				if existing == k {
					return
				}
			}
			keys = append(keys, k)
		}
	}

	if apiKey != "" {
		for _, part := range strings.FieldsFunc(apiKey, func(r rune) bool {
			return r == ',' || r == ';' || r == '\n' || r == '\r'
		}) {
			addKey(part)
		}
	}

	// Also check environment variables for multi-key rotation
	if envKeys := os.Getenv("GEMINI_API_KEYS"); envKeys != "" {
		for _, part := range strings.FieldsFunc(envKeys, func(r rune) bool {
			return r == ',' || r == ';' || r == '\n' || r == '\r'
		}) {
			addKey(part)
		}
	}
	for i := 1; i <= 10; i++ {
		if k := os.Getenv(fmt.Sprintf("GEMINI_API_KEY_%d", i)); k != "" {
			addKey(k)
		}
	}
	return keys
}

func NewGeminiAdapter(id string, priority int, apiKey, model string) *GeminiAdapter {
	if id == "" {
		id = "google-ai-studio"
	}
	flash := DefaultGeminiFlashModel
	pro := DefaultGeminiProModel

	if model != "" {
		if strings.Contains(model, "pro") {
			pro = model
		} else {
			flash = model
		}
	} else {
		model = flash
	}

	keys := parseGeminiKeys(apiKey)
	firstKey := apiKey
	if len(keys) > 0 {
		firstKey = keys[0]
	}

	return &GeminiAdapter{
		AdapterID:   id,
		PriorityLvl: priority,
		APIKey:      firstKey,
		APIKeys:     keys,
		FlashModel:  flash,
		ProModel:    pro,
		TargetModel: model,
		HTTPClient:  defaultHTTPClient,
	}
}

func (a *GeminiAdapter) ID() string    { return a.AdapterID }
func (a *GeminiAdapter) Priority() int { return a.PriorityLvl }

func (a *GeminiAdapter) getNextAPIKey() (string, int) {
	keys := a.APIKeys
	if len(keys) == 0 {
		return a.APIKey, 0
	}
	if len(keys) == 1 {
		return keys[0], 0
	}
	idx := int(atomic.AddUint64(&a.keyIndex, 1)-1) % len(keys)
	now := time.Now()
	// Pick first key that is not in cooldown
	for step := 0; step < len(keys); step++ {
		candidateIdx := (idx + step) % len(keys)
		candidate := keys[candidateIdx]
		if cdVal, ok := a.keyLimits.Load(candidate); ok {
			if cd, ok := cdVal.(time.Time); ok && now.Before(cd) {
				continue
			}
		}
		return candidate, candidateIdx
	}
	// All in cooldown, return candidate by round-robin
	return keys[idx], idx
}

func (a *GeminiAdapter) markKeyCooldown(key string, d time.Duration) {
	if key == "" {
		return
	}
	if d <= 0 {
		d = 30 * time.Second
	}
	a.keyLimits.Store(key, time.Now().Add(d))
}

func (a *GeminiAdapter) SendMessageStream(ctx context.Context, req *types.ChatRequest) (<-chan types.StreamChunk, error) {
	keys := a.APIKeys
	if len(keys) == 0 && a.APIKey != "" {
		keys = []string{a.APIKey}
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("%s: %w: empty API key", a.AdapterID, types.ErrAuthentication)
	}

	selectedModel := a.TargetModel
	if selectedModel == "" {
		selectedModel = a.FlashModel
	}
	if selectedModel == "" {
		selectedModel = DefaultGeminiFlashModel
	}

	clonedReq := *req
	clonedReq.Thinking = false
	clonedReq.ThinkingBudget = 0
	clonedReq.ReasoningEffort = "none"

	maxAttempts := len(keys)
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		currentKey, _ := a.getNextAPIKey()
		if currentKey == "" {
			continue
		}

		adapter := &OpenAICompatibleAdapter{
			AdapterID:   a.AdapterID,
			PriorityLvl: a.PriorityLvl,
			BaseURL:     googleAIStudioBaseURL,
			APIKey:      currentKey,
			TargetModel: selectedModel,
			HTTPClient:  a.HTTPClient,
		}
		ch, err := adapter.SendMessageStream(ctx, &clonedReq)
		if err == nil {
			return ch, nil
		}

		lastErr = err
		if errors.Is(err, types.ErrRateLimitReached) || strings.Contains(err.Error(), "429") || strings.Contains(err.Error(), "quota") {
			// Mark this specific key in cooldown and try next key in pool
			a.markKeyCooldown(currentKey, 15*time.Second)
			continue
		}

		if strings.Contains(err.Error(), "thought_signature") {
			fallbackModel := "gemini-2.5-flash"
			if fallbackModel != selectedModel {
				fallbackAdapter := &OpenAICompatibleAdapter{
					AdapterID:   a.AdapterID,
					PriorityLvl: a.PriorityLvl,
					BaseURL:     googleAIStudioBaseURL,
					APIKey:      currentKey,
					TargetModel: fallbackModel,
					HTTPClient:  a.HTTPClient,
				}
				return fallbackAdapter.SendMessageStream(ctx, req)
			}
		}
		return nil, err
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("%s: all gemini API keys exhausted", a.AdapterID)
}

// geminiModelsResponse is the subset of v1beta/models we need to pick a
// default: name ("models/gemini-3.6-pro") and which generation methods the
// key is actually entitled to call.
type geminiModelsResponse struct {
	Models []struct {
		Name                       string   `json:"name"`
		SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
	} `json:"models"`
}

// DetectGeminiModel queries Google AI Studio's models endpoint with apiKey
// and returns the best model that key can actually call via generateContent,
// preferring a "pro" tier model over "flash"/"lite" when both are available.
// Returns "" (never an error the caller must handle) when detection fails —
// callers should fall back to their existing hardcoded default so login
// never blocks on this.
func DetectGeminiModel(apiKey string) string {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" || strings.HasPrefix(apiKey, "env:") {
		return ""
	}

	req, err := http.NewRequest(http.MethodGet, googleAIStudioModelsURL+"?key="+apiKey, nil)
	if err != nil {
		return ""
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil {
		return ""
	}

	var parsed geminiModelsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return ""
	}
	return pickGeminiModel(parsed)
}

// pickGeminiModel chooses the best callable gemini-* model from a decoded
// v1beta/models response: pro > flash (non-lite) > whatever else is offered.
func pickGeminiModel(parsed geminiModelsResponse) string {
	var pro, flash, any string
	for _, m := range parsed.Models {
		name := strings.TrimPrefix(m.Name, "models/")
		supportsGenerate := false
		for _, method := range m.SupportedGenerationMethods {
			if method == "generateContent" {
				supportsGenerate = true
				break
			}
		}
		if !supportsGenerate || !strings.HasPrefix(name, "gemini-") {
			continue
		}
		if any == "" {
			any = name
		}
		switch {
		case strings.Contains(name, "-pro"):
			if pro == "" || strings.Contains(name, "3.1") {
				pro = name
			}
		case flash == "" && strings.Contains(name, "-flash") && !strings.Contains(name, "-lite"):
			flash = name
		}
	}
	switch {
	case pro != "":
		return pro
	case flash != "":
		return flash
	default:
		return any
	}
}
