package oauth

import (
	"context"

	"amux-accounts/pkg/provider"
)

const (
	GrokClientID      = "xai-cli-oauth"
	GrokDeviceAuthURL = "https://auth.x.ai/oauth/device_authorization"
	GrokTokenURL      = "https://auth.x.ai/oauth/token"
	GrokAPIBaseURL    = "https://api.x.ai/v1"
	GrokDefaultModel  = "grok-2-latest"
)

// LoginGrokDeviceFlow executes the RFC 8628 Device Authorization Flow for xAI Grok.
func LoginGrokDeviceFlow(ctx context.Context, customName string) (string, error) {
	return RunDeviceFlow(ctx, DeviceFlowConfig{
		ProviderLabel: "xAI Grok",
		ClientID:      GrokClientID,
		DeviceAuthURL: GrokDeviceAuthURL,
		TokenURL:      GrokTokenURL,
		BaseURL:       GrokAPIBaseURL,
		DefaultModel:  GrokDefaultModel,
		IDPrefix:      "grok:api",
		Priority:      provider.PriorityAPIGrok,
	}, customName)
}
