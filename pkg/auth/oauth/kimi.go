package oauth

import (
	"context"

	"amux-accounts/pkg/provider"
)

const (
	KimiClientID      = "17e5f671-d194-4dfb-9706-5516cb48c098"
	KimiDeviceAuthURL = "https://auth.kimi.com/api/oauth/device_authorization"
	KimiTokenURL      = "https://auth.kimi.com/api/oauth/token"
	KimiAPIBaseURL    = "https://api.moonshot.cn/v1"
	KimiDefaultModel  = "moonshot-v1-128k"
)

// LoginKimiDeviceFlow executes the RFC 8628 Device Authorization Flow for Kimi (Moonshot AI).
func LoginKimiDeviceFlow(ctx context.Context, customName string) (string, error) {
	return RunDeviceFlow(ctx, DeviceFlowConfig{
		ProviderLabel: "Kimi (Moonshot AI)",
		ClientID:      KimiClientID,
		DeviceAuthURL: KimiDeviceAuthURL,
		TokenURL:      KimiTokenURL,
		BaseURL:       KimiAPIBaseURL,
		DefaultModel:  KimiDefaultModel,
		IDPrefix:      "kimi:api",
		Priority:      provider.PriorityAPIKimi,
	}, customName)
}
