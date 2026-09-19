package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"amux-accounts/pkg/auth"
	"amux-accounts/pkg/tools"
	"amux-accounts/pkg/types"
)

const (
	agyTokenRefreshLead  = 2 * time.Minute
	agyOAuthTokenURL     = "https://oauth2.googleapis.com/token"
	DefaultAGYModel      = "gemini-3.8-flash-medium"
	DefaultAGYFlashModel = "gemini-3.8-flash-low"
	agyBaseURL           = "https://generativelanguage.googleapis.com/v1beta/openai"
)

type agyCredentials struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Email        string `json:"email"`
	ExpiresAt    int64  `json:"expires_at"` // unix millisecond
}

// AntigravityAdapter connects amux to Google Antigravity / Gemini via direct HTTP or CLI.
type AntigravityAdapter struct {
	AdapterID   string // e.g. "agy:01"
	PriorityLvl int
	TargetModel string
	GroupLabel  string
	PlanTier    string // "subscription" | "free"
	HTTPClient  *http.Client

	mu    sync.Mutex
	creds *agyCredentials
}

const (
	agyDailyEndpoint = "https://daily-cloudcode-pa.googleapis.com/v1internal:streamGenerateContent?alt=sse"
	agyProdEndpoint  = "https://cloudcode-pa.googleapis.com/v1internal:streamGenerateContent?alt=sse"
	agyUserAgent     = "antigravity/cli/1.2.5 (aidev_client; os_type=darwin; arch=amd64; cl=982839923; auth_method=consumer)"
	agyProjectID     = "aicode-consumers"
)

type agyPart struct {
	Text             string                    `json:"text,omitempty"`
	FunctionCall     *tools.GeminiFunctionCall `json:"functionCall,omitempty"`
	FunctionResponse *agyFuncResponse          `json:"functionResponse,omitempty"`
}

type agyFuncResponse struct {
	Name     string          `json:"name"`
	Response json.RawMessage `json:"response"`
}

type agyContent struct {
	Role  string    `json:"role"`
	Parts []agyPart `json:"parts"`
}

type agyToolDeclaration struct {
	FunctionDeclarations []tools.GeminiFunctionDeclaration `json:"functionDeclarations,omitempty"`
}

type agyInnerRequest struct {
	Contents          []agyContent         `json:"contents"`
	SystemInstruction *agyContent          `json:"systemInstruction,omitempty"`
	Tools             []agyToolDeclaration `json:"tools,omitempty"`
	GenerationConfig  *struct {
		Temperature     float64 `json:"temperature,omitempty"`
		MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
	} `json:"generationConfig,omitempty"`
}

type agyRequestBody struct {
	Project string          `json:"project"`
	Model   string          `json:"model"`
	Request agyInnerRequest `json:"request"`
}

func buildAGYRequestBody(project, model string, req *types.ChatRequest) agyRequestBody {
	var inner agyInnerRequest
	inner.Contents = make([]agyContent, 0, len(req.Messages))

	// Map toolCallID to toolName across all turns for reliable fallback
	toolNames := make(map[string]string)
	for _, m := range req.Messages {
		for _, tc := range m.ToolCalls {
			if tc.ID != "" && tc.Name != "" {
				toolNames[tc.ID] = tc.Name
			}
		}
	}

	var sysParts []agyPart
	for _, m := range req.Messages {
		role := strings.ToLower(m.Role)
		if role == "system" {
			if strings.TrimSpace(m.Content) != "" {
				sysParts = append(sysParts, agyPart{Text: m.Content})
			}
			continue
		}

		switch role {
		case "assistant":
			role = "model"
		case "tool":
			role = "user"
		default:
			role = "user"
		}

		var parts []agyPart

		// Regular conversational text (for role="tool", content is the tool result, not standalone text)
		if !strings.EqualFold(m.Role, "tool") && m.Content != "" {
			parts = append(parts, agyPart{Text: m.Content})
		}

		// Tool calls from model turn
		for _, tc := range m.ToolCalls {
			argsRaw := json.RawMessage(tc.Arguments)
			if len(argsRaw) == 0 {
				argsRaw = json.RawMessage("{}")
			}
			sig := tc.ThoughtSignature
			if sig == "" && tc.ID != "" {
				sig = tools.LookupThoughtSignature(tc.ID)
			}
			parts = append(parts, agyPart{
				FunctionCall: &tools.GeminiFunctionCall{
					Name:             tc.Name,
					Args:             argsRaw,
					ThoughtSignature: sig,
				},
			})
		}

		// Tool results from client
		if strings.EqualFold(m.Role, "tool") || m.ToolCallID != "" {
			toolName := m.Name
			if toolName == "" && m.ToolCallID != "" {
				toolName = toolNames[m.ToolCallID]
			}
			if toolName == "" {
				toolName = m.ToolCallID
			}
			respRaw := json.RawMessage(m.Content)
			if !json.Valid(respRaw) {
				respRaw, _ = json.Marshal(map[string]string{"output": m.Content})
			}
			parts = append(parts, agyPart{
				FunctionResponse: &agyFuncResponse{
					Name:     toolName,
					Response: respRaw,
				},
			})
		}

		if len(parts) > 0 {
			// Merge adjacent turns with identical role (e.g. parallel tool outputs) to enforce alternating turns
			if len(inner.Contents) > 0 && inner.Contents[len(inner.Contents)-1].Role == role {
				inner.Contents[len(inner.Contents)-1].Parts = append(inner.Contents[len(inner.Contents)-1].Parts, parts...)
			} else {
				inner.Contents = append(inner.Contents, agyContent{
					Role:  role,
					Parts: parts,
				})
			}
		}
	}

	if len(inner.Contents) == 0 {
		inner.Contents = append(inner.Contents, agyContent{
			Role:  "user",
			Parts: []agyPart{{Text: "Hello"}},
		})
	} else if inner.Contents[0].Role != "user" {
		inner.Contents = append([]agyContent{
			{Role: "user", Parts: []agyPart{{Text: "Continue"}}},
		}, inner.Contents...)
	}

	if len(sysParts) > 0 {
		inner.SystemInstruction = &agyContent{
			Role:  "system",
			Parts: sysParts,
		}
	}

	if len(req.Tools) > 0 {
		inner.Tools = []agyToolDeclaration{
			{
				FunctionDeclarations: tools.ToGeminiFunctions(req.Tools),
			},
		}
	}

	caps := tools.GetModelCapabilities("gemini", model)
	var temp float64
	if caps.SupportsTemperature && req.Temperature > 0 {
		temp = req.Temperature
	}
	if temp > 0 || req.MaxTokens > 0 {
		inner.GenerationConfig = &struct {
			Temperature     float64 `json:"temperature,omitempty"`
			MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
		}{
			Temperature:     temp,
			MaxOutputTokens: req.MaxTokens,
		}
	}

	return agyRequestBody{
		Project: project,
		Model:   model,
		Request: inner,
	}
}

func (a *AntigravityAdapter) sendHTTPStream(ctx context.Context, req *types.ChatRequest) (<-chan types.StreamChunk, error) {
	token, err := a.ensureAccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w: %v", a.AdapterID, types.ErrAuthentication, err)
	}

	model := normalizeAGYModel(a.TargetModel)
	if req.TargetTier == "flash" && a.PlanTier == "free" {
		model = DefaultAGYFlashModel
	}

	bodyObj := buildAGYRequestBody(agyProjectID, model, req)
	b, err := json.Marshal(bodyObj)
	if err != nil {
		return nil, fmt.Errorf("%s: marshal request: %w", a.AdapterID, err)
	}

	endpoints := []string{agyDailyEndpoint, agyProdEndpoint}
	var lastErr error
	var resp *http.Response

	for _, ep := range endpoints {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, ep, bytes.NewReader(b))
		if err != nil {
			lastErr = err
			continue
		}
		httpReq.Header.Set("Authorization", "Bearer "+token)
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("User-Agent", agyUserAgent)
		httpReq.Header.Set("Accept", "text/event-stream")

		r, err := a.client().Do(httpReq)
		if err != nil {
			lastErr = err
			continue
		}
		if r.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(r.Body)
			r.Body.Close()
			lastErr = fmt.Errorf("status %d from %s: %s", r.StatusCode, ep, string(raw))
			continue
		}
		resp = r
		break
	}

	if resp == nil {
		return nil, fmt.Errorf("%s: http request failed: %v", a.AdapterID, lastErr)
	}

	ch := make(chan types.StreamChunk, 64)

	go func() {
		defer close(ch)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		buf := make([]byte, 64*1024)
		scanner.Buffer(buf, 2*1024*1024)

		type sseCandidate struct {
			Content struct {
				Role  string `json:"role"`
				Parts []struct {
					Text             string `json:"text"`
					ThoughtSignature string `json:"thoughtSignature"`
					ThoughtSigSnake  string `json:"thought_signature"`
					FunctionCall     *struct {
						Name             string          `json:"name"`
						Args             json.RawMessage `json:"args"`
						ID               string          `json:"id"`
						ThoughtSignature string          `json:"thoughtSignature"`
						ThoughtSigSnake  string          `json:"thought_signature"`
					} `json:"functionCall"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		}

		type sseChunk struct {
			Response struct {
				Candidates []sseCandidate `json:"candidates"`
			} `json:"response"`
		}

		sentAny := false
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			line := scanner.Bytes()
			if !bytes.HasPrefix(line, []byte("data: ")) {
				continue
			}

			payload := bytes.TrimPrefix(line, []byte("data: "))
			var ev sseChunk
			if err := json.Unmarshal(payload, &ev); err != nil {
				continue
			}

			for _, c := range ev.Response.Candidates {
				for _, p := range c.Content.Parts {
					if p.Text != "" {
						ch <- types.StreamChunk{
							ID:      a.AdapterID,
							Content: p.Text,
						}
						sentAny = true
					}
					if p.FunctionCall != nil {
						args := string(p.FunctionCall.Args)
						if args == "" || args == "null" {
							args = "{}"
						}
						id := p.FunctionCall.ID
						if id == "" {
							id = fmt.Sprintf("call_%d", time.Now().UnixNano())
						}
						sig := p.ThoughtSignature
						if sig == "" {
							sig = p.ThoughtSigSnake
						}
						if sig == "" && p.FunctionCall != nil {
							sig = p.FunctionCall.ThoughtSignature
							if sig == "" {
								sig = p.FunctionCall.ThoughtSigSnake
							}
						}
						if sig != "" {
							tools.RecordThoughtSignature(id, sig)
						}
						ch <- types.StreamChunk{
							ID: a.AdapterID,
							ToolCalls: []types.ToolCall{
								{
									ID:               id,
									Name:             p.FunctionCall.Name,
									Arguments:        args,
									ThoughtSignature: sig,
								},
							},
						}
						sentAny = true
					}
				}
				if c.FinishReason != "" {
					ch <- types.StreamChunk{
						ID:           a.AdapterID,
						Done:         true,
						FinishReason: strings.ToLower(c.FinishReason),
					}
					return
				}
			}
		}

		if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) && !sentAny {
			ch <- types.StreamChunk{
				ID:    a.AdapterID,
				Error: fmt.Errorf("agy sse stream error: %w", err),
			}
			return
		}

		ch <- types.StreamChunk{
			ID:           a.AdapterID,
			Done:         true,
			FinishReason: "stop",
		}
	}()

	return ch, nil
}

func (a *AntigravityAdapter) sendCLIStream(ctx context.Context, req *types.ChatRequest, bin string) (<-chan types.StreamChunk, error) {
	model := normalizeAGYModel(a.TargetModel)
	if req.TargetTier == "flash" && a.PlanTier == "free" {
		model = DefaultAGYFlashModel
	}

	effort := "low"
	if strings.HasSuffix(model, "-high") {
		effort = "high"
	} else if strings.HasSuffix(model, "-medium") {
		effort = "medium"
	}

	prompt := WebBackendPrompt(req, false)
	if strings.TrimSpace(prompt) == "" {
		return nil, fmt.Errorf("%s: empty prompt for agy", a.AdapterID)
	}

	args := []string{
		"--dangerously-skip-permissions",
		"--disable-slash-commands",
		"--effort", effort,
		"--model", model,
		"--output-format", "stream-json",
		"--print", prompt,
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = os.TempDir()
	cmd.Env = os.Environ()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("%s: create stdout pipe: %w", a.AdapterID, err)
	}

	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%s: start agy process: %w", a.AdapterID, err)
	}

	ch := make(chan types.StreamChunk, 64)

	go func() {
		defer close(ch)

		scanner := bufio.NewScanner(stdout)
		buf := make([]byte, 64*1024)
		scanner.Buffer(buf, 2*1024*1024)

		type agyEvent struct {
			Event      string `json:"event"`
			StepUpdate *struct {
				State     string `json:"state"`
				StepType  string `json:"step_type"`
				TextDelta string `json:"text_delta"`
			} `json:"step_update"`
			Result *struct {
				Status   string `json:"status"`
				Response string `json:"response"`
			} `json:"result"`
		}

		sentAny := false
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			line := scanner.Bytes()
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}

			var ev agyEvent
			if err := json.Unmarshal(line, &ev); err != nil {
				continue
			}

			if ev.Event == "step_update" && ev.StepUpdate != nil {
				if ev.StepUpdate.TextDelta != "" {
					ch <- types.StreamChunk{
						ID:      a.AdapterID,
						Content: ev.StepUpdate.TextDelta,
					}
					sentAny = true
				}
			} else if ev.Event == "result" && ev.Result != nil {
				if !sentAny && ev.Result.Response != "" {
					ch <- types.StreamChunk{
						ID:      a.AdapterID,
						Content: ev.Result.Response,
					}
					sentAny = true
				}
			}
		}

		waitErr := cmd.Wait()
		if waitErr != nil && !sentAny {
			errMsg := strings.TrimSpace(stderrBuf.String())
			if errMsg == "" {
				errMsg = waitErr.Error()
			}
			ch <- types.StreamChunk{
				ID:    a.AdapterID,
				Error: fmt.Errorf("agy failed: %s", errMsg),
			}
			return
		}

		if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) && !sentAny {
			ch <- types.StreamChunk{
				ID:    a.AdapterID,
				Error: fmt.Errorf("agy scan error: %w", err),
			}
			return
		}

		ch <- types.StreamChunk{
			ID:           a.AdapterID,
			Done:         true,
			FinishReason: "stop",
		}
	}()

	return ch, nil
}

func (a *AntigravityAdapter) SendMessageStream(ctx context.Context, req *types.ChatRequest) (<-chan types.StreamChunk, error) {
	// 1. Primary: Direct HTTP SSE to Google Cloud Code backend (zero binary needed)
	ch, err := a.sendHTTPStream(ctx, req)
	if err == nil {
		return ch, nil
	}

	// 2. Fallback: CLI binary if present
	if bin := findAGYBinary(); bin != "" {
		if cliCh, cliErr := a.sendCLIStream(ctx, req, bin); cliErr == nil {
			return cliCh, nil
		}
	}

	return nil, err
}

func (a *AntigravityAdapter) ID() string    { return a.AdapterID }
func (a *AntigravityAdapter) Priority() int { return a.PriorityLvl }
func (a *AntigravityAdapter) Group() string {
	if a.GroupLabel != "" {
		return a.GroupLabel
	}
	if strings.EqualFold(a.PlanTier, "free") || strings.Contains(strings.ToLower(a.AdapterID), "free") {
		return "agy_free"
	}
	return "agy_sub"
}

// SupportsTools is true: AGY supports full function calling and tool execution.
func (a *AntigravityAdapter) SupportsTools() bool { return true }

func (a *AntigravityAdapter) client() *http.Client {
	if a.HTTPClient != nil {
		return a.HTTPClient
	}
	return defaultHTTPClient
}

// AGYCredentialsPath returns the path to Antigravity CLI's credentials file.
func AGYCredentialsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".gemini", "antigravity-cli", "credentials.json")
	}
	return filepath.Join(home, ".gemini", "antigravity-cli", "credentials.json")
}

func findAGYBinary() string {
	if p, err := exec.LookPath("agy"); err == nil {
		return p
	}
	if p, err := exec.LookPath("antigravity"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		p := filepath.Join(home, ".local", "bin", "agy")
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
		p2 := filepath.Join(home, ".local", "bin", "antigravity")
		if info, err := os.Stat(p2); err == nil && !info.IsDir() {
			return p2
		}
	}
	for _, p := range []string{"/opt/homebrew/bin/agy", "/usr/local/bin/agy"} {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

// AGYAuthAvailable reports whether Antigravity credentials or CLI binary exist.
func AGYAuthAvailable() bool {
	if findAGYBinary() != "" {
		return true
	}
	c, err := loadAGYCredentials()
	return err == nil && c != nil && (c.AccessToken != "" || c.RefreshToken != "")
}

func normalizeAGYModel(target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return DefaultAGYModel
	}
	valid := []string{
		"gemini-3.8-flash-high",
		"gemini-3.8-flash-medium",
		"gemini-3.8-flash-low",
		"gemini-3.7-flash-high",
		"gemini-3.7-flash-medium",
		"gemini-3.7-flash-low",
		"gemini-3.6-flash-high",
		"gemini-3.6-flash-medium",
		"gemini-3.6-flash-low",
		"gemini-3.1-pro-high",
		"gemini-3.1-pro-low",
		"gpt-oss-120b-medium",
	}
	for _, v := range valid {
		if strings.EqualFold(target, v) {
			return v
		}
	}

	low := strings.ToLower(target)
	switch {
	case strings.Contains(low, "3.1-pro") || strings.Contains(low, "pro"):
		return "gemini-3.1-pro-high"
	case strings.Contains(low, "3.7-flash"):
		return "gemini-3.7-flash-medium"
	case strings.Contains(low, "3.6-flash"):
		return "gemini-3.6-flash-medium"
	case strings.Contains(low, "3.8-flash"):
		if strings.Contains(low, "low") {
			return "gemini-3.8-flash-low"
		}
		if strings.Contains(low, "high") {
			return "gemini-3.8-flash-high"
		}
		return "gemini-3.8-flash-medium"
	case strings.Contains(low, "flash"):
		return "gemini-3.8-flash-medium"
	case strings.Contains(low, "gpt"):
		return "gpt-oss-120b-medium"
	default:
		return DefaultAGYModel
	}
}

func parseAGYCredentialsJSON(b []byte) (*agyCredentials, error) {
	if len(b) == 0 {
		return nil, fmt.Errorf("empty credentials payload")
	}
	var c agyCredentials
	if err := json.Unmarshal(b, &c); err == nil && (c.AccessToken != "" || c.RefreshToken != "") {
		return &c, nil
	}

	// Go keyring doc structure: {"token": {"access_token": ..., "refresh_token": ..., "expiry": ...}, "id_token": ...}
	var doc struct {
		Token struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			Expiry       string `json:"expiry"`
			TokenType    string `json:"token_type"`
		} `json:"token"`
		IDToken string `json:"id_token"`
	}
	if err := json.Unmarshal(b, &doc); err == nil {
		if doc.Token.AccessToken != "" || doc.Token.RefreshToken != "" {
			c.AccessToken = doc.Token.AccessToken
			c.RefreshToken = doc.Token.RefreshToken
			c.IDToken = doc.IDToken
			if doc.Token.Expiry != "" {
				if t, err := time.Parse(time.RFC3339Nano, doc.Token.Expiry); err == nil {
					c.ExpiresAt = t.UnixMilli()
				} else if t, err := time.Parse(time.RFC3339, doc.Token.Expiry); err == nil {
					c.ExpiresAt = t.UnixMilli()
				}
			}
			return &c, nil
		}
	}
	return nil, fmt.Errorf("could not parse antigravity credentials")
}

func parseAGYCredentialsString(s string) (*agyCredentials, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty credentials string")
	}
	if strings.HasPrefix(s, "go-keyring-base64:") {
		dec, err := base64.StdEncoding.DecodeString(s[len("go-keyring-base64:"):])
		if err != nil {
			return nil, fmt.Errorf("decode go-keyring-base64: %w", err)
		}
		return parseAGYCredentialsJSON(dec)
	}
	return parseAGYCredentialsJSON([]byte(s))
}

func loadAGYCredentials() (*agyCredentials, error) {
	// 1. Try file ~/.gemini/antigravity-cli/credentials.json
	path := AGYCredentialsPath()
	if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
		if c, err := parseAGYCredentialsJSON(b); err == nil && c != nil {
			return c, nil
		}
	}

	// 2. Try file ~/.gemini/oauth_creds.json
	home, _ := os.UserHomeDir()
	if home != "" {
		oauthPath := filepath.Join(home, ".gemini", "oauth_creds.json")
		if b, err := os.ReadFile(oauthPath); err == nil && len(b) > 0 {
			if c, err := parseAGYCredentialsJSON(b); err == nil && c != nil {
				return c, nil
			}
		}
	}

	// 3. Try Keychain service "gemini" account "antigravity" (standard Go keyring format used by AGY)
	if s, err := auth.KCGet("gemini", "antigravity"); err == nil && s != "" {
		if c, err := parseAGYCredentialsString(s); err == nil && c != nil {
			return c, nil
		}
	}

	// 4. Try Keychain service "antigravity-service"
	for _, acct := range []string{"antigravity", types.CurrentUser()} {
		if s, err := auth.KCGet("antigravity-service", acct); err == nil && s != "" {
			if c, err := parseAGYCredentialsString(s); err == nil && c != nil {
				return c, nil
			}
		}
	}

	return nil, fmt.Errorf("no valid antigravity credentials found")
}

func saveAGYCredentials(c *agyCredentials) error {
	path := AGYCredentialsPath()
	_ = os.MkdirAll(filepath.Dir(path), 0700)
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	_ = os.WriteFile(path, b, 0600)
	if c.Email != "" {
		_ = auth.KCSet("antigravity-service", c.Email, string(b))
	}
	return nil
}

func (a *AntigravityAdapter) ensureAccessToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.creds == nil {
		c, err := loadAGYCredentials()
		if err != nil {
			return "", err
		}
		a.creds = c
	}

	expiresAt := time.UnixMilli(a.creds.ExpiresAt)
	if a.creds.AccessToken != "" && time.Now().Add(agyTokenRefreshLead).Before(expiresAt) {
		return a.creds.AccessToken, nil
	}

	if a.creds.RefreshToken == "" {
		if a.creds.AccessToken != "" {
			return a.creds.AccessToken, nil
		}
		return "", fmt.Errorf("antigravity: refresh token missing, please re-login via 'am login agy'")
	}

	// Refresh OAuth token
	if a.creds.ClientID == "" {
		a.creds.ClientID = "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com"
	}
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("client_id", a.creds.ClientID)
	if a.creds.ClientSecret != "" {
		data.Set("client_secret", a.creds.ClientSecret)
	}
	data.Set("refresh_token", a.creds.RefreshToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, agyOAuthTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("build token refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("execute token refresh: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("refresh token failed (status %d): %s", resp.StatusCode, string(b))
	}

	var tr struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
		IDToken     string `json:"id_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", fmt.Errorf("decode token refresh response: %w", err)
	}

	a.creds.AccessToken = tr.AccessToken
	if tr.ExpiresIn > 0 {
		a.creds.ExpiresAt = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second).UnixMilli()
	} else {
		a.creds.ExpiresAt = time.Now().Add(3600 * time.Second).UnixMilli()
	}
	_ = saveAGYCredentials(a.creds)

	return a.creds.AccessToken, nil
}


