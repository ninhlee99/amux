package identity

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"amux-accounts/pkg/auth"
	"amux-accounts/pkg/types"
)

var (
	storeMu sync.RWMutex
	identitiesFileMagic = []byte("AMENC1:")
)

// DefaultIdentitiesPath returns ~/.amux/identities.json.
func DefaultIdentitiesPath() string {
	return filepath.Join(types.BaseDir(), "identities.json")
}

// LoadConfig loads the flat identity config from disk. If the file does not exist,
// it returns a default config with ThresholdPct: 95.0.
// Transparently decrypts AMENC1: sealed configurations.
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

	if bytes.HasPrefix(data, identitiesFileMagic) {
		enc := data[len(identitiesFileMagic):]
		dec, decErr := base64.StdEncoding.DecodeString(string(bytes.TrimSpace(enc)))
		if decErr == nil {
			if plain, perr := auth.Decrypt(dec); perr == nil {
				data = plain
			}
		}
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

// SaveConfig atomically writes the flat identity config to disk with AES-256-GCM encryption at rest.
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

	var fileData []byte
	enc, encErr := auth.Encrypt(data)
	if encErr == nil {
		fileData = append(append([]byte{}, identitiesFileMagic...), []byte(base64.StdEncoding.EncodeToString(enc))...)
		fileData = append(fileData, '\n')
	} else {
		fileData = append(data, '\n')
	}

	tmpFile := fmt.Sprintf("%s.tmp.%d", path, os.Getpid())
	if err := os.WriteFile(tmpFile, fileData, 0o600); err != nil {
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

// Upsert adds or updates an identity by ID, strictly enforcing that each email
// has at most 1 account per canonical provider, where Subscription strictly
// supersedes and replaces Web accounts.
func Upsert(path string, id Identity) error {
	cfg, err := LoadConfig(path)
	if err != nil {
		return err
	}

	found := false
	// 1. Match by exact ID
	for i, existing := range cfg.Identities {
		if existing.ID == id.ID {
			cfg.Identities[i] = id
			found = true
			break
		}
	}

	// 2. Match by email + canonical provider (1 email per provider boundary)
	email := id.Email()
	if !found && email != "" && email != "-" {
		for i, existing := range cfg.Identities {
			if CanonicalProvider(existing.Provider) == CanonicalProvider(id.Provider) && strings.EqualFold(existing.Email(), email) {
				// Subscription strictly supersedes Web
				if id.IsSubscription() && !existing.IsSubscription() {
					cfg.Identities[i] = id
					found = true
					break
				}
				if !id.IsSubscription() && existing.IsSubscription() {
					// Existing subscription cannot be downgraded or duplicated by web
					found = true
					break
				}
				// Same tier: update existing in place
				cfg.Identities[i] = id
				found = true
				break
			}
		}
	}

	if !found {
		cfg.Identities = append(cfg.Identities, id)
	}

	cfg.Identities = DeduplicateIdentities(cfg.Identities)
	return SaveConfig(path, cfg)
}

// DeduplicateIdentities eliminates ghost entries and enforces the invariant:
// 1 account per email per canonical provider. If an email has both a Subscription
// and a Web account, the Subscription account is preserved and the Web duplicate is pruned.
func DeduplicateIdentities(list []Identity) []Identity {
	if len(list) == 0 {
		return list
	}

	// First pass: remove ghosts (empty credentials and no account email)
	var nonGhosts []Identity
	for _, item := range list {
		if len(item.Credentials) == 0 && item.Email() == "-" {
			continue
		}
		nonGhosts = append(nonGhosts, item)
	}

	// Second pass: group by canonical provider + email
	// For API keys or missing emails ("-"), group by exact ID
	type groupKey struct {
		provider string
		email    string
		id       string
	}

	groups := make(map[groupKey][]Identity)
	var order []groupKey

	for _, item := range nonGhosts {
		em := strings.ToLower(item.Email())
		k := groupKey{
			provider: CanonicalProvider(item.Provider),
		}
		if em != "" && em != "-" {
			k.email = em
		} else {
			k.id = item.ID
		}

		if _, exists := groups[k]; !exists {
			order = append(order, k)
		}
		groups[k] = append(groups[k], item)
	}

	var result []Identity
	for _, k := range order {
		candidates := groups[k]
		if len(candidates) == 1 {
			winner := candidates[0]
			if winner.Metadata != nil {
				if v, ok := winner.Metadata["disabled"].(bool); ok && v {
					winner.Active = false
				}
			}
			result = append(result, winner)
			continue
		}

		best := candidates[0]
		for _, cand := range candidates[1:] {
			if isBetterIdentity(cand, best) {
				mergeIdentityInfo(&cand, best)
				best = cand
			} else {
				mergeIdentityInfo(&best, cand)
			}
		}
		if best.Metadata != nil {
			if v, ok := best.Metadata["disabled"].(bool); ok && v {
				best.Active = false
			}
		}
		result = append(result, best)
	}

	return result
}

func mergeIdentityInfo(target *Identity, source Identity) {
	if target.Credentials == nil {
		target.Credentials = make(map[string]string)
	}
	for k, v := range source.Credentials {
		if target.Credentials[k] == "" && v != "" {
			target.Credentials[k] = v
		}
	}
	if target.Metadata == nil {
		target.Metadata = make(map[string]interface{})
	}
	for k, v := range source.Metadata {
		if target.Metadata[k] == nil && v != nil {
			target.Metadata[k] = v
		}
	}
	if target.Metadata != nil {
		if v, ok := target.Metadata["disabled"].(bool); ok && v {
			target.Active = false
		}
	}
}

func isBetterIdentity(cand, best Identity) bool {
	// 1. Subscription beats Web
	if cand.IsSubscription() != best.IsSubscription() {
		return cand.IsSubscription()
	}
	// 2. Active beats Inactive
	if cand.Active != best.Active {
		return cand.Active
	}
	// 3. More credentials
	if len(cand.Credentials) != len(best.Credentials) {
		return len(cand.Credentials) > len(best.Credentials)
	}
	// 4. Non-numeric ID preferred
	candLegacy := isLegacyNumericIdentityID(cand.ID)
	bestLegacy := isLegacyNumericIdentityID(best.ID)
	if candLegacy != bestLegacy {
		return !candLegacy
	}
	return false
}

func isLegacyNumericIdentityID(id string) bool {
	parts := strings.Split(id, ":")
	last := parts[len(parts)-1]
	if len(last) == 2 && last[0] >= '0' && last[0] <= '9' && last[1] >= '0' && last[1] <= '9' {
		return true
	}
	return false
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
			item.Active = true
		} else {
			item.Metadata["disabled"] = true
			item.Active = false
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
		if len(cfg.Identities) > 0 {
			for _, ident := range cfg.Identities {
				if ident.ID == id {
					return IsEnabled(ident) && ident.CanAutoRotate()
				}
			}
			return false
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
