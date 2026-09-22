package cli

import (
	"strings"
	"testing"
)

func TestRouter_DomainRouteRegistration(t *testing.T) {
	r := NewRouter()

	expectedDomains := map[string]string{
		// Identity domain
		"login":    "identity",
		"account":  "identity",
		"accounts": "identity",
		"id":       "identity",
		"switch":   "identity",
		"migrate":  "identity",
		"vault":    "identity",
		"use":      "identity",
		"project":  "identity",

		// Gateway domain
		"start":   "gateway",
		"stop":    "gateway",
		"restart": "gateway",
		"hook":    "gateway",
		"unhook":  "gateway",
		"env":     "gateway",
		"gateway": "gateway",

		// Diagnostics domain
		"status":     "diagnostics",
		"doctor":     "diagnostics",
		"audit":      "diagnostics",
		"usage":      "diagnostics",
		"setup":      "diagnostics",
		"config":     "diagnostics",
		"threshold":  "diagnostics",
		"update":     "diagnostics",
		"uninstall":  "diagnostics",
		"feedback":   "diagnostics",
		"completion": "diagnostics",
		"init":       "diagnostics",
		"statusline": "diagnostics",
		"whoami":     "diagnostics",
		"ps":         "diagnostics",
		"ls":         "diagnostics",
		"version":    "diagnostics",
	}

	for cmd, expectedDomain := range expectedDomains {
		route, exists := r.GetRoute(cmd)
		if !exists {
			t.Errorf("expected route %q to be registered in Router", cmd)
			continue
		}
		if route.Domain != expectedDomain {
			t.Errorf("route %q: expected domain %q, got %q", cmd, expectedDomain, route.Domain)
		}
		if route.Handler == nil {
			t.Errorf("route %q: expected Handler to be non-nil", cmd)
		}
	}
}

func TestRouter_CustomRouteRegistration(t *testing.T) {
	r := NewRouter()
	invoked := false

	r.Register("custom-cmd", CommandRoute{
		Domain: "custom",
		Handler: func(args []string) {
			invoked = true
		},
	})

	route, exists := r.GetRoute("custom-cmd")
	if !exists {
		t.Fatalf("expected custom-cmd to be registered")
	}
	if route.Domain != "custom" {
		t.Fatalf("expected domain 'custom', got %q", route.Domain)
	}

	route.Handler([]string{})
	if !invoked {
		t.Fatalf("expected handler to execute")
	}
}

func TestRouter_DispatchHelpFlags(t *testing.T) {
	r := NewRouter()
	helpCalled := false

	r.Register("test-help", CommandRoute{
		Domain: "test",
		Handler: func(args []string) {},
		HelpHandler: func() {
			helpCalled = true
		},
	})

	r.Dispatch([]string{"amux", "test-help", "--help"})
	if !helpCalled {
		t.Errorf("expected help handler to be called when --help is passed")
	}
}

func TestRouter_DispatchRootHelp(t *testing.T) {
	r := NewRouter()
	out := captureStdout(func() {
		r.Dispatch([]string{"amux", "--help"})
	})

	if !strings.Contains(out, "Usage: amux <command>") {
		t.Errorf("expected general help output when 'amux --help' is dispatched")
	}
}

func TestRouter_FuzzySuggestion(t *testing.T) {
	r := NewRouter()

	tests := []struct {
		input    string
		expected string
	}{
		{"stat", "status"},
		{"swich", "switch"},
		{"docto", "doctor"},
		{"usag", "usage"},
		{"settup", "setup"},
	}

	for _, tt := range tests {
		suggested := r.findClosestCommand(tt.input)
		if suggested != tt.expected {
			t.Errorf("input %q: expected suggestion %q, got %q", tt.input, tt.expected, suggested)
		}
	}
}
