package identity

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"amux-accounts/pkg/types"
)

var (
	storeMu sync.RWMutex
)

// DefaultIdentitiesPath returns ~/.amux/identities.json.
func DefaultIdentitiesPath() string {
	return filepath.Join(types.BaseDir(), "identities.json")
}

// LoadConfig loads the flat identity config from disk. If the file does not exist,
// it returns a default config with ThresholdPct: 95.0.
func LoadConfig(path string) (*Config, error) {
	storeMu.RLock()
	defer storeMu.RUnlock()

	if path == "" {
		path = DefaultIdentitiesPath()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{
				ThresholdPct: DefaultThresholdPct,
				Identities:   []Identity{},
			}, nil
		}
		return nil, fmt.Errorf("read identity config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal identity config: %w", err)
	}
	if cfg.ThresholdPct <= 0 {
		cfg.ThresholdPct = DefaultThresholdPct
	}
	if cfg.Identities == nil {
		cfg.Identities = []Identity{}
	}
	return &cfg, nil
}

// SaveConfig atomically writes the flat identity config to disk.
func SaveConfig(path string, cfg *Config) error {
	storeMu.Lock()
	defer storeMu.Unlock()

	if path == "" {
		path = DefaultIdentitiesPath()
	}

	if cfg.ThresholdPct <= 0 {
		cfg.ThresholdPct = DefaultThresholdPct
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal identity config: %w", err)
	}

	tmpFile := fmt.Sprintf("%s.tmp.%d", path, os.Getpid())
	if err := os.WriteFile(tmpFile, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write tmp identity config: %w", err)
	}

	if err := os.Rename(tmpFile, path); err != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("atomic rename identity config: %w", err)
	}

	return nil
}

// List returns all identities.
func List(path string) ([]Identity, error) {
	cfg, err := LoadConfig(path)
	if err != nil {
		return nil, err
	}
	return cfg.Identities, nil
}

// Get finds an identity by ID, email, or profile name, or returns nil.
func Get(path string, id string) (*Identity, error) {
	cfg, err := LoadConfig(path)
	if err != nil {
		return nil, err
	}
	id = strings.TrimSpace(id)
	for _, item := range cfg.Identities {
		if item.ID == id {
			return &item, nil
		}
	}
	for _, item := range cfg.Identities {
		if strings.EqualFold(item.Email(), id) {
			return &item, nil
		}
		if item.Metadata != nil {
			if prof, ok := item.Metadata["profile_name"].(string); ok && strings.EqualFold(prof, id) {
				return &item, nil
			}
		}
	}
	return nil, nil
}

// Upsert adds or updates an identity by ID.
func Upsert(path string, id Identity) error {
	cfg, err := LoadConfig(path)
	if err != nil {
		return err
	}

	found := false
	for i, existing := range cfg.Identities {
		if existing.ID == id.ID {
			cfg.Identities[i] = id
			found = true
			break
		}
	}
	if !found {
		cfg.Identities = append(cfg.Identities, id)
	}

	return SaveConfig(path, cfg)
}

// Remove deletes an identity by ID.
func Remove(path string, id string) (bool, error) {
	cfg, err := LoadConfig(path)
	if err != nil {
		return false, err
	}

	var updated []Identity
	removed := false
	for _, item := range cfg.Identities {
		match := item.ID == id
		if !match && item.Metadata != nil {
			if prof, ok := item.Metadata["profile_name"].(string); ok && prof == id {
				match = true
			}
		}
		if match {
			removed = true
			continue
		}
		updated = append(updated, item)
	}
	if !removed {
		return false, nil
	}
	cfg.Identities = updated
	return true, SaveConfig(path, cfg)
}

// SetActive marks an identity as active for its tool/provider group and deactivates other identities in that group.
func SetActive(path string, id string) error {
	cfg, err := LoadConfig(path)
	if err != nil {
		return err
	}

	id = strings.TrimSpace(id)
	var targetIdentity *Identity
	for i := range cfg.Identities {
		if cfg.Identities[i].ID == id || strings.EqualFold(cfg.Identities[i].Email(), id) {
			targetIdentity = &cfg.Identities[i]
			break
		}
		if cfg.Identities[i].Metadata != nil {
			if prof, ok := cfg.Identities[i].Metadata["profile_name"].(string); ok && strings.EqualFold(prof, id) {
				targetIdentity = &cfg.Identities[i]
				break
			}
		}
	}
	if targetIdentity == nil {
		return fmt.Errorf("identity %q not found", id)
	}

	actualID := targetIdentity.ID

	for i := range cfg.Identities {
		if isSameIdentityGroup(*targetIdentity, cfg.Identities[i]) {
			cfg.Identities[i].Active = (cfg.Identities[i].ID == actualID)
		}
	}

	return SaveConfig(path, cfg)
}

func isSameIdentityGroup(target, item Identity) bool {
	if CanonicalProvider(target.Provider) != CanonicalProvider(item.Provider) {
		return false
	}
	// Antigravity and Gemini Web are distinct tools under the Google/Gemini ecosystem
	isTargetAGY := strings.HasPrefix(target.ID, "antigravity")
	isItemAGY := strings.HasPrefix(item.ID, "antigravity")
	if isTargetAGY != isItemAGY {
		return false
	}
	// Subscription vs Web tiers are distinct execution modes
	if target.Tier != "" && item.Tier != "" && target.Tier != item.Tier {
		return false
	}
	return true
}

// SetAutoRotate updates whether an identity is eligible for automatic rotation.
func SetAutoRotate(path string, id string, enabled bool) error {
	cfg, err := LoadConfig(path)
	if err != nil {
		return err
	}

	found := false
	for i := range cfg.Identities {
		if cfg.Identities[i].ID == id {
			cfg.Identities[i].AutoRotate = &enabled
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("identity %q not found", id)
	}

	return SaveConfig(path, cfg)
}

// CanAutoRotateByID checks if an identity is eligible for auto-rotation/switching.
// If not found in config, defaults to true. If found and CanAutoRotate() is false, returns false.
func CanAutoRotateByID(path string, id string) bool {
	cfg, err := LoadConfig(path)
	if err != nil {
		return true
	}
	for _, ident := range cfg.Identities {
		if ident.ID == id {
			return ident.CanAutoRotate()
		}
	}
	return true
}

// SetEnabled hard-enables or hard-disables an identity by ID.
// disabled=true  → identity is completely blocked from routing (rotator + pool skip it).
// disabled=false → identity is restored to its prior auto-rotate state.
// Unlike SetAutoRotate (soft exclusion for manual-only), this is a hard off/on switch.
func SetEnabled(path string, id string, enabled bool) error {
	cfg, err := LoadConfig(path)
	if err != nil {
		return err
	}

	id = strings.TrimSpace(id)
	found := false
	for i := range cfg.Identities {
		item := &cfg.Identities[i]
		if item.ID != id {
			continue
		}
		if item.Metadata == nil {
			item.Metadata = map[string]interface{}{}
		}
		if enabled {
			delete(item.Metadata, "disabled")
		} else {
			item.Metadata["disabled"] = true
		}
		found = true
		break
	}
	if !found {
		return fmt.Errorf("identity %q not found", id)
	}
	return SaveConfig(path, cfg)
}

// IsEnabled reports whether the identity is enabled (not hard-disabled).
// Defaults to true (enabled) when the field is absent.
func IsEnabled(id Identity) bool {
	if id.Metadata == nil {
		return true
	}
	if v, ok := id.Metadata["disabled"].(bool); ok {
		return !v
	}
	return true
}

// AutoRotateFilter returns a predicate function for auto-rotation eligibility.
// Returns false for identities that are hard-disabled (metadata.disabled=true)
// or have auto-rotate turned off.
func AutoRotateFilter(path string) func(id string) bool {
	return func(id string) bool {
		cfg, err := LoadConfig(path)
		if err != nil {
			return true
		}
		for _, ident := range cfg.Identities {
			if ident.ID == id {
				return IsEnabled(ident) && ident.CanAutoRotate()
			}
		}
		return true
	}
}

// SetThreshold updates the threshold percentage for an identity by ID, email, or profile name.
// If threshold is nil or <= 0, the custom threshold override is cleared.
func SetThreshold(path string, id string, threshold *float64) error {
	cfg, err := LoadConfig(path)
	if err != nil {
		return err
	}

	id = strings.TrimSpace(id)
	targetIdx := -1
	for i, item := range cfg.Identities {
		if item.ID == id {
			targetIdx = i
			break
		}
	}
	if targetIdx == -1 {
		for i, item := range cfg.Identities {
			if strings.EqualFold(item.Email(), id) {
				targetIdx = i
				break
			}
			if item.Metadata != nil {
				if prof, ok := item.Metadata["profile_name"].(string); ok && strings.EqualFold(prof, id) {
					targetIdx = i
					break
				}
			}
		}
	}

	if targetIdx == -1 {
		return fmt.Errorf("identity %q not found", id)
	}

	if threshold != nil && *threshold <= 0 {
		threshold = nil
	}
	cfg.Identities[targetIdx].ThresholdPct = threshold

	return SaveConfig(path, cfg)
}
