package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"amux-accounts/pkg/types"
)

const (
	googleAIStudioBaseURL = "https://generativelanguage.googleapis.com/v1beta/openai"

	DefaultGeminiFlashModel = "gemini-3.8-flash"
	DefaultGeminiProModel   = "gemini-3.8-flash"
)

// googleAIStudioModelsURL is a var (not const) so tests can point it at an
// httptest server instead of the real Google endpoint.
var googleAIStudioModelsURL = "https://generativelanguage.googleapis.com/v1beta/models"

// GeminiAdapter wraps Google AI Studio's OpenAI-compatible endpoint.
type GeminiAdapter struct {
	AdapterID   string
	PriorityLvl int
	APIKey      string
	FlashModel  string
	ProModel    string
	TargetModel string
	HTTPClient  *http.Client

	wrapped types.ProviderAdapter
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

	return &GeminiAdapter{
		AdapterID:   id,
		PriorityLvl: priority,
		APIKey:      apiKey,
		FlashModel:  flash,
		ProModel:    pro,
		TargetModel: model,
		HTTPClient:  defaultHTTPClient,
	}
}

func (a *GeminiAdapter) ID() string    { return a.AdapterID }
func (a *GeminiAdapter) Priority() int { return a.PriorityLvl }

func (a *GeminiAdapter) SendMessageStream(ctx context.Context, req *types.ChatRequest) (<-chan types.StreamChunk, error) {
	if a.APIKey == "" {
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

	adapter := &OpenAICompatibleAdapter{
		AdapterID:   a.AdapterID,
		PriorityLvl: a.PriorityLvl,
		BaseURL:     googleAIStudioBaseURL,
		APIKey:      a.APIKey,
		TargetModel: selectedModel,
		HTTPClient:  a.HTTPClient,
	}
	ch, err := adapter.SendMessageStream(ctx, &clonedReq)
	if err != nil && (errors.Is(err, types.ErrRateLimitReached) || strings.Contains(err.Error(), "429") || strings.Contains(err.Error(), "thought_signature")) {
		fallbackModel := a.FlashModel
		if strings.Contains(err.Error(), "thought_signature") {
			fallbackModel = "gemini-2.5-flash"
		}
		if fallbackModel != "" && fallbackModel != selectedModel {
			fallbackAdapter := &OpenAICompatibleAdapter{
				AdapterID:   a.AdapterID,
				PriorityLvl: a.PriorityLvl,
				BaseURL:     googleAIStudioBaseURL,
				APIKey:      a.APIKey,
				TargetModel: fallbackModel,
				HTTPClient:  a.HTTPClient,
			}
			return fallbackAdapter.SendMessageStream(ctx, req)
		}
	}
	return ch, err
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
