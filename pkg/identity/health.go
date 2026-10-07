package identity

import (
	"fmt"
	"strings"
	"time"

	"amux-accounts/pkg/auth"
)

// HealthReport summarizes health status for an identity.
type HealthReport struct {
	ID           string  `json:"id"`
	Provider     string  `json:"provider"`
	Tier         string  `json:"tier"`
	Status       string  `json:"status"` // "healthy", "expired", "needs_refresh", "missing_credentials"
	UsagePercent float64 `json:"usage_percent"`
	Message      string  `json:"message"`
}

// CheckHealth inspects the credentials and quotas of an identity.
func CheckHealth(id Identity) HealthReport {
	report := HealthReport{
		ID:           id.ID,
		Provider:     id.Provider,
		Tier:         string(id.Tier),
		UsagePercent: id.UsagePercent,
		Status:       "healthy",
		Message:      "Valid and ready",
	}

	if len(id.Credentials) == 0 {
		report.Status = "missing_credentials"
		report.Message = fmt.Sprintf("No credentials stored (run 'amux login %s')", id.Provider)
		return report
	}

	// Check web credentials
	if id.Tier == TierWeb {
		hasCookie := id.Credentials["cookies"] != "" || id.Credentials["session_key"] != "" || id.Credentials["secure_1psid"] != "" || id.Credentials["token"] != "" || id.Credentials["access_token"] != ""
		if !hasCookie {
			report.Status = "missing_credentials"
			report.Message = fmt.Sprintf("Missing session cookies (run 'amux login %s')", id.Provider)
			return report
		}
	}

	// Check token expiration if available
	var expTime time.Time
	var hasExp bool
	for _, k := range []string{"access_token", "id_token", "token"} {
		if tok := id.Credentials[k]; tok != "" {
			if t, ok := auth.ParseJWTExpiry(tok); ok {
				expTime = t
				hasExp = true
				break
			}
		}
	}
	if !hasExp && id.ResetAt > 0 {
		expTime = time.Unix(id.ResetAt, 0)
		hasExp = true
	}

	if hasExp {
		now := time.Now()
		if now.After(expTime) {
			if id.Credentials["refresh_token"] != "" {
				report.Status = "needs_refresh"
				report.Message = "Access token expired, refresh token available"
			} else {
				report.Status = "expired"
				report.Message = fmt.Sprintf("Token expired on %s (run 'amux login %s')", expTime.Format("2006-01-02"), id.Provider)
			}
			return report
		}
		remaining := expTime.Sub(now)
		if remaining < 24*time.Hour {
			report.Status = "expiring_soon"
			report.Message = fmt.Sprintf("Expires soon in %s (at %s)", remaining.Round(time.Minute), expTime.Format("15:04 02/01"))
		} else {
			report.Message = fmt.Sprintf("Valid (expires in %d days)", int(remaining.Hours()/24))
		}
	}

	if id.UsagePercent >= 100.0 {
		report.Message = fmt.Sprintf("Quota exhausted (100%%, resets in %s)", id.FormatResetTime())
	} else if id.UsagePercent >= DefaultThresholdPct {
		report.Message = fmt.Sprintf("Approaching limit (%.1f%%, resets in %s)", id.UsagePercent, id.FormatResetTime())
	}

	return report
}

// CheckAllHealth inspects all identities in the configuration.
func CheckAllHealth(path string) ([]HealthReport, error) {
	identities, err := List(path)
	if err != nil {
		return nil, err
	}

	reports := make([]HealthReport, 0, len(identities))
	for _, id := range identities {
		reports = append(reports, CheckHealth(id))
	}
	return reports, nil
}

// ProbeKeychainAccess verifies macOS Keychain accessibility for native direct mode.
func ProbeKeychainAccess() (bool, string) {
	acct := "test-probe"
	service := "amux-probe-test"
	secret := "probe-ok"

	err := auth.KCSet(service, acct, secret)
	if err != nil {
		return false, fmt.Sprintf("write to keychain failed: %v", err)
	}

	got, err := auth.KCGet(service, acct)
	if err != nil || strings.TrimSpace(got) != secret {
		return false, fmt.Sprintf("read from keychain failed: %v", err)
	}

	return true, "macOS Keychain read/write operational"
}
