package types

import "testing"

func TestProviderCapability_Satisfies(t *testing.T) {
	full := FullCapabilities()
	web := WebCapabilities()

	// Tool requirements
	needNativeTools := ProviderCapability{NativeTools: true}
	if !full.Satisfies(needNativeTools) {
		t.Fatalf("full capabilities should satisfy NativeTools")
	}
	if web.Satisfies(needNativeTools) {
		t.Fatalf("web capabilities should not satisfy NativeTools")
	}

	needWebTools := ProviderCapability{WebTools: true}
	if !web.Satisfies(needWebTools) {
		t.Fatalf("web capabilities should satisfy WebTools")
	}

	// Streaming
	needStreaming := ProviderCapability{Streaming: true}
	if !full.Satisfies(needStreaming) || !web.Satisfies(needStreaming) {
		t.Fatalf("both should satisfy Streaming")
	}
}
