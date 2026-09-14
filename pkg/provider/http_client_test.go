package provider

import (
	"testing"
	"time"
)

func TestDefaultTransport_AllowsSlowFirstByte(t *testing.T) {
	transport := newDefaultTransport()
	if transport.ResponseHeaderTimeout < 5*time.Minute {
		t.Fatalf("ResponseHeaderTimeout = %v, want at least 5m", transport.ResponseHeaderTimeout)
	}
}
