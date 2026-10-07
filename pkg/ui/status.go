package ui

import (
	"strings"
)

// proxyStatus is the decoded /_am/status payload.
type proxyStatus struct {
	Tool     string           `json:"tool"`
	Switches int              `json:"switches"`
	Sessions int              `json:"sessions"`
	Upstream string           `json:"upstream"`
	Mode     string           `json:"mode"`
	Accounts []statusAccount  `json:"accounts"`
	Pool     []map[string]any `json:"pool"`
	ToolPool []map[string]any `json:"tool_pool"`
	Guard    map[string]any   `json:"guard,omitempty"`
}

type statusAccount struct {
	Profile        string   `json:"profile"`
	Account        string   `json:"account"`
	Active         bool     `json:"active"`
	Remaining      float64  `json:"remaining"`
	LimitReset     string   `json:"limit_reset"`
	Cooldown       string   `json:"cooldown_until"`
	Dead           bool     `json:"dead"`
	Disabled       bool     `json:"disabled"`
	FiveHUsed      *float64 `json:"5h_used"`
	FiveHReset     string   `json:"5h_reset"`
	SevenDUsed     *float64 `json:"7d_used"`
	SevenDReset    string   `json:"7d_reset"`
	AutoSwitches   int      `json:"auto_switches"`
	ManualSwitches int      `json:"manual_switches"`
}

func guardDeductionReason(report map[string]any) string {
	if qReason, ok := report["quarantineReason"].(string); ok && qReason != "" {
		return qReason
	}
	score, _ := report["score"].(float64)
	if score >= 100 {
		return ""
	}
	errMsg, _ := report["lastErrorMessage"].(string)
	errMsg = strings.TrimSpace(errMsg)
	if errMsg == "" {
		if authErr, ok := report["consecutiveAuthErr"].(float64); ok && authErr > 0 {
			return "auth error"
		}
		return "transient error"
	}
	return cleanGuardErrorMessage(errMsg)
}

func cleanGuardErrorMessage(msg string) string {
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "tpm") || strings.Contains(low, "tokens per minute"):
		return "status 413: TPM rate limit exceeded"
	case strings.Contains(low, "missing a thought_signature") || strings.Contains(low, "thought_signature"):
		return "status 400: missing thought_signature"
	case strings.Contains(low, "rate limit") || strings.Contains(low, "429"):
		return "429 rate limit reached"
	case strings.Contains(low, "request too large") || strings.Contains(low, "413"):
		return "status 413: request too large"
	case strings.Contains(low, "auth") || strings.Contains(low, "401") || strings.Contains(low, "403"):
		if strings.Contains(low, "401") {
			return "auth failure (401)"
		}
		if strings.Contains(low, "403") {
			return "auth failure (403 forbidden)"
		}
		return "auth failure"
	case strings.Contains(low, "deadline exceeded") || strings.Contains(low, "timeout"):
		return "request timeout"
	case strings.Contains(low, "status 500"):
		return "status 500: internal error"
	case strings.Contains(low, "status 502"):
		return "status 502: bad gateway"
	case strings.Contains(low, "status 503"):
		return "status 503: service unavailable"
	case strings.Contains(low, "status 404"):
		return "status 404: not found"
	case strings.Contains(low, "status 400"):
		return "status 400: invalid request"
	}

	// If it contains JSON with "message": "...", extract it
	if idx := strings.Index(msg, `"message":`); idx != -1 {
		rest := msg[idx+len(`"message":`):]
		rest = strings.TrimSpace(rest)
		if strings.HasPrefix(rest, `"`) {
			rest = rest[1:]
			if end := strings.Index(rest, `"`); end != -1 {
				inner := rest[:end]
				return truncateRunes(inner, 45)
			}
		}
	}

	// Normalize single line, strip newlines
	msg = strings.ReplaceAll(msg, "\n", " ")
	msg = strings.ReplaceAll(msg, "\r", " ")
	msg = strings.ReplaceAll(msg, "\t", " ")
	for strings.Contains(msg, "  ") {
		msg = strings.ReplaceAll(msg, "  ", " ")
	}
	msg = strings.TrimSpace(msg)

	return truncateRunes(msg, 45)
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
