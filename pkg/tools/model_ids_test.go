package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// retiredModelIDs were removed or renamed upstream (checked 2026-10-05 against
// the official model pages of Anthropic, OpenAI/Codex, Google, Groq, xAI and
// Kimi). They must not come back as defaults or advertised models.
var retiredModelIDs = []string{
	`"claude-3-7-sonnet-20250219"`, `"claude-3-7-sonnet"`, `"claude-3-5-sonnet-20241022"`,
	`"claude-3-5-haiku-20241022"`, `"claude-3-opus-20240229"`, `"claude-3-haiku-20240307"`,
	`"gpt-4o"`, `"gpt-4o-mini"`, `"o1-mini"`, `"gpt-5.6-terra"`,
	`"gemini-2.0-flash"`, `"gemini-3-pro-preview"`,
	`"mixtral-8x7b-32768"`, `"grok-2-latest"`, `"moonshot-v1-128k"`,
}

func TestNoRetiredModelIDsInSource(t *testing.T) {
	root := ".."
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, id := range retiredModelIDs {
			if strings.Contains(string(b), id) {
				t.Errorf("%s still references retired model %s", path, id)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
