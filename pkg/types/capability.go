package types

// ProviderCapability declares the concrete functional features supported by a provider adapter.
type ProviderCapability struct {
	Streaming   bool `json:"streaming"`
	NativeTools bool `json:"native_tools"`
	WebTools    bool `json:"web_tools"`
	Vision      bool `json:"vision"`
	Reasoning   bool `json:"reasoning"`
}

// FullCapabilities returns a capability set where all API features are supported.
func FullCapabilities() ProviderCapability {
	return ProviderCapability{
		Streaming:   true,
		NativeTools: true,
		WebTools:    false,
		Vision:      true,
		Reasoning:   true,
	}
}

// WebCapabilities returns typical capabilities of web session adapters.
func WebCapabilities() ProviderCapability {
	return ProviderCapability{
		Streaming:   true,
		NativeTools: false,
		WebTools:    true,
		Vision:      true,
		Reasoning:   true,
	}
}

// Satisfies checks whether this capability set meets all required capabilities.
func (c ProviderCapability) Satisfies(req ProviderCapability) bool {
	if req.Streaming && !c.Streaming {
		return false
	}
	if req.NativeTools && !c.NativeTools {
		return false
	}
	if req.WebTools && !c.WebTools {
		return false
	}
	if req.Vision && !c.Vision {
		return false
	}
	if req.Reasoning && !c.Reasoning {
		return false
	}
	return true
}
