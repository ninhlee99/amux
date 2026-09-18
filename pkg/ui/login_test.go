package ui

import (
	"testing"
)

func TestParseLoginFlags_API(t *testing.T) {
	cases := []struct {
		name         string
		args         []string
		wantProvider string
		wantToken    string
		wantModel    string
		wantBaseURL  string
		wantName     string
	}{
		{
			name:         "api with separate flags",
			args:         []string{"api", "--api-key", "sk-12345", "--model", "deepseek-chat", "--endpoint", "https://api.deepseek.com/v1", "--name", "deepseek"},
			wantProvider: "api",
			wantToken:    "sk-12345",
			wantModel:    "deepseek-chat",
			wantBaseURL:  "https://api.deepseek.com/v1",
			wantName:     "deepseek",
		},
		{
			name:         "api with key=value flags",
			args:         []string{"api", "--api-key=sk-secret", "--model=gpt-4o", "--endpoint=https://api.openai.com/v1", "--name=openai"},
			wantProvider: "api",
			wantToken:    "sk-secret",
			wantModel:    "gpt-4o",
			wantBaseURL:  "https://api.openai.com/v1",
			wantName:     "openai",
		},
		{
			name:         "api with aliases (--key, --url, --provider)",
			args:         []string{"api", "--key", "sk-key", "--url", "http://localhost:11434/v1", "--provider", "ollama", "--model", "llama3"},
			wantProvider: "api",
			wantToken:    "sk-key",
			wantModel:    "llama3",
			wantBaseURL:  "http://localhost:11434/v1",
			wantName:     "ollama",
		},
		{
			name:         "gemini direct login",
			args:         []string{"gemini"},
			wantProvider: "gemini",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prov, flags, _ := parseLoginFlags(tc.args)
			if prov != tc.wantProvider {
				t.Errorf("provider = %q, want %q", prov, tc.wantProvider)
			}
			if tc.wantToken != "" && flags.token != tc.wantToken {
				t.Errorf("token = %q, want %q", flags.token, tc.wantToken)
			}
			if tc.wantModel != "" && flags.model != tc.wantModel {
				t.Errorf("model = %q, want %q", flags.model, tc.wantModel)
			}
			if tc.wantBaseURL != "" && flags.baseURL != tc.wantBaseURL {
				t.Errorf("baseURL = %q, want %q", flags.baseURL, tc.wantBaseURL)
			}
			if tc.wantName != "" && flags.name != tc.wantName {
				t.Errorf("name = %q, want %q", flags.name, tc.wantName)
			}
		})
	}
}
