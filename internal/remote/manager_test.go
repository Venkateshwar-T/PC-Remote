package remote

import (
	"os"
	"testing"

	"laptopcontrol/internal/config"
)

func TestSessionManager_HandleSessionRequest_Unauthorized(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "mgr_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	cfg, err := config.LoadOrCreate(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	mgr := NewSessionManager(cfg)
	defer mgr.Close()

	resp, err := mgr.HandleSessionRequest("req-test-unauth", "unauthorized_pubkey_123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != "unauthorized" {
		t.Fatalf("expected status unauthorized, got %s", resp.Status)
	}
}
