package project

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config represents per-project configuration stored in .amux or .amux.json.
type Config struct {
	Account   string `json:"account,omitempty"`
	Provider  string `json:"provider,omitempty"`
	Threshold int    `json:"threshold,omitempty"`
	Model     string `json:"model,omitempty"`
	Note      string `json:"note,omitempty"`
}

// LoadProjectConfig reads .amux / .amux.json if present in the project tree.
func LoadProjectConfig(startDir string) (*Config, string, error) {
	if startDir == "" {
		var err error
		startDir, err = os.Getwd()
		if err != nil {
			return nil, "", err
		}
	}

	curr := filepath.Clean(startDir)
	for {
		for _, name := range []string{".amux", ".amux.json", ".amuxrc"} {
			p := filepath.Join(curr, name)
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				data, err := os.ReadFile(p)
				if err != nil {
					continue
				}
				trimmed := strings.TrimSpace(string(data))
				// If plain string, treat as Account ID
				if !strings.HasPrefix(trimmed, "{") {
					return &Config{
						Account: trimmed,
					}, p, nil
				}
				var cfg Config
				if err := json.Unmarshal(data, &cfg); err == nil {
					return &cfg, p, nil
				}
			}
		}

		parent := filepath.Dir(curr)
		if parent == curr {
			break
		}
		curr = parent
	}

	return nil, "", nil
}

// SaveProjectConfig writes the project configuration into .amux in the specified directory.
func SaveProjectConfig(dir string, cfg *Config) error {
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return err
		}
	}

	p := filepath.Join(dir, ".amux")
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal project config: %w", err)
	}
	return os.WriteFile(p, append(data, '\n'), 0644)
}
