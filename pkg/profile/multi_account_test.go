package profile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"amux-accounts/pkg/types"
)

func TestMultiAccount_SnapshotPreservationAndLogin(t *testing.T) {
	tmpDir := t.TempDir()

	origAMDir := os.Getenv("AM_DIR")
	amDir := filepath.Join(tmpDir, "am_data")
	_ = os.Setenv("AM_DIR", amDir)
	defer func() {
		if origAMDir != "" {
			_ = os.Setenv("AM_DIR", origAMDir)
		} else {
			_ = os.Unsetenv("AM_DIR")
		}
	}()

	// Mock Claude credentials file at mock directory
	credFile := filepath.Join(tmpDir, "claude_credentials.json")

	// Save custom tool config to avoid triggering OS Keychain during automated test
	mockConfig := types.ToolConfig{
		Tools: map[string]types.ToolSpec{
			"claude": {
				Name: "claude",
				Artifacts: []types.Artifact{
					{
						Kind:         "file",
						Path:         credFile,
						Optional:     true,
						AccountField: "email",
					},
				},
			},
		},
	}
	SaveConfig(mockConfig)

	// Step 1: User 1 (ninhle) is currently logged in on the system
	user1Creds := map[string]any{
		"account_id":  "acc-ninhle-001",
		"email":       "ninhle@example.com",
		"oauth_token": "token-ninhle-xyz",
	}
	u1Bytes, _ := json.Marshal(user1Creds)
	if err := os.WriteFile(credFile, u1Bytes, 0o600); err != nil {
		t.Fatalf("write user1 creds: %v", err)
	}

	// Step 2: amux runs SyncActiveFromSystem("claude") prior to or during login
	SyncActiveFromSystem("claude")

	// Verify user 1 was auto-saved as a profile
	profiles := ListProfiles("claude")
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile saved, got %d", len(profiles))
	}
	user1ProfileName := profiles[0].Name

	// Verify profile data matches ninhle
	entries1 := LoadProfileEntries("claude", user1ProfileName)
	if len(entries1) == 0 {
		t.Fatalf("expected non-empty bundle for user1")
	}
	var readU1 map[string]any
	if err := json.Unmarshal(entries1[0].Data, &readU1); err != nil {
		t.Fatalf("unmarshal bundle u1: %v", err)
	}
	if readU1["email"] != "ninhle@example.com" {
		t.Fatalf("expected ninhle@example.com in snapshot, got %v", readU1["email"])
	}

	// Step 3: User 2 (tungnt) logs in, replacing system file
	user2Creds := map[string]any{
		"account_id":  "acc-tungnt-002",
		"email":       "tungnt@example.com",
		"oauth_token": "token-tungnt-abc",
	}
	u2Bytes, _ := json.Marshal(user2Creds)
	if err := os.WriteFile(credFile, u2Bytes, 0o600); err != nil {
		t.Fatalf("write user2 creds: %v", err)
	}

	// Set active profile pointer to user2 (simulating amux login completing for tungnt)
	WriteActivePointer("claude", "tungnt")

	// Step 4: amux snapshots user2
	SyncActiveFromSystem("claude")

	// Step 5: Verify BOTH accounts coexist in amux storage
	allProfiles := ListProfiles("claude")
	if len(allProfiles) != 2 {
		t.Fatalf("expected 2 profiles (ninhle & tungnt), got %d: %v", len(allProfiles), allProfiles)
	}

	user2ProfileName := MatchProfileByAccount("claude", "tungnt@example.com")
	if user2ProfileName == "" {
		t.Fatalf("expected tungnt@example.com to be matched in profiles")
	}

	// Verify User 1's bundle was completely unharmed by User 2's login
	entries1Again := LoadProfileEntries("claude", user1ProfileName)
	if len(entries1Again) == 0 {
		t.Fatalf("expected user1 entries to still exist")
	}
	var readU1Again map[string]any
	_ = json.Unmarshal(entries1Again[0].Data, &readU1Again)
	if readU1Again["email"] != "ninhle@example.com" || readU1Again["oauth_token"] != "token-ninhle-xyz" {
		t.Fatalf("user1 credentials clobbered! got %v", readU1Again)
	}

	// Step 6: Test token rotation update while tungnt is active
	user2Creds["oauth_token"] = "token-tungnt-rotated-999"
	u2RotatedBytes, _ := json.Marshal(user2Creds)
	_ = os.WriteFile(credFile, u2RotatedBytes, 0o600)

	// SyncActiveFromSystem should update tungnt bundle with rotated token
	SyncActiveFromSystem("claude")
	entries2Rotated := LoadProfileEntries("claude", user2ProfileName)
	if len(entries2Rotated) == 0 {
		t.Fatalf("expected tungnt entries to exist")
	}
	var readU2Rotated map[string]any
	_ = json.Unmarshal(entries2Rotated[0].Data, &readU2Rotated)
	if readU2Rotated["oauth_token"] != "token-tungnt-rotated-999" {
		t.Fatalf("expected rotated token to be captured, got %v", readU2Rotated["oauth_token"])
	}
}
