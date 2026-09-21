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

// FindProjectRoot locates the nearest directory containing .amux or .git starting from startDir.
func FindProjectRoot(startDir string) (string, error) {
	if startDir == "" {
		var err error
		startDir, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}

	curr := filepath.Clean(startDir)
	for {
		// Check for .amux or .amux.json or .amuxrc
		if _, err := os.Stat(filepath.Join(curr, ".amux")); err == nil {
			return curr, nil
		}
		if _, err := os.Stat(filepath.Join(curr, ".amux.json")); err == nil {
			return curr, nil
		}
		if _, err := os.Stat(filepath.Join(curr, ".amuxrc")); err == nil {
			return curr, nil
		}

		// If at git root, stop traversal
		if _, err := os.Stat(filepath.Join(curr, ".git")); err == nil {
			return curr, nil
		}

		parent := filepath.Dir(curr)
		if parent == curr {
			// Reached filesystem root
			break
		}
		curr = parent
	}
	return startDir, nil
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
