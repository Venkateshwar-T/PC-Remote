package service

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"laptopcontrol/internal/ipc"
)

func TestDaemonLifecycleAndIPC(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pcremote_daemon_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	pipeName := fmt.Sprintf(`\\.\pipe\TestDaemonIPC_%d`, time.Now().UnixNano())
	testPort := 18765 // Non-standard port to avoid any conflict

	daemon := NewDaemon(testPort, pipeName, tempDir)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Failed to start daemon: %v", err)
	}
	defer daemon.Stop()

	// Wait briefly for listeners to initialize
	time.Sleep(100 * time.Millisecond)

	client := ipc.NewClient(pipeName)
	defer client.Close()

	// 1. Verify Status
	status, err := client.GetStatus()
	if err != nil {
		t.Fatalf("client.GetStatus failed: %v", err)
	}
	if !status.Running || status.Port != testPort {
		t.Fatalf("Unexpected status: %+v", status)
	}

	// 2. Verify Pairing Info
	pairInfo, err := client.GetPairingInfo()
	if err != nil {
		t.Fatalf("client.GetPairingInfo failed: %v", err)
	}
	if pairInfo.Token == "" || len(pairInfo.Token) != 32 {
		t.Fatalf("Invalid pairing token: %s", pairInfo.Token)
	}
	if pairInfo.LaptopPubKey == "" {
		t.Fatalf("Missing laptop public key")
	}

	// 3. Verify Token Rotation
	rotated, err := client.RotatePairingToken()
	if err != nil {
		t.Fatalf("client.RotatePairingToken failed: %v", err)
	}
	if rotated.Token == pairInfo.Token {
		t.Fatalf("Expected token to change on rotation: %s vs %s", rotated.Token, pairInfo.Token)
	}

	// 4. Verify PIN Status and Setup
	pinStat, err := client.GetPinStatus()
	if err != nil {
		t.Fatalf("client.GetPinStatus failed: %v", err)
	}
	if pinStat.IsSet {
		t.Fatalf("Expected PIN not set initially")
	}

	setRes, err := client.SetPin("", "987654")
	if err != nil || !setRes.Success {
		t.Fatalf("client.SetPin failed: %v, %+v", err, setRes)
	}

	pinStat, err = client.GetPinStatus()
	if err != nil || !pinStat.IsSet {
		t.Fatalf("Expected PIN to be set: %v, %+v", err, pinStat)
	}

	// 5. Verify Key Migration
	newPrivKey := nostr.GeneratePrivateKey()
	expectedPub, _ := nostr.GetPublicKey(newPrivKey)

	migRes, err := client.MigrateLegacyKey(newPrivKey)
	if err != nil || !migRes.Success {
		t.Fatalf("client.MigrateLegacyKey failed: %v, %+v", err, migRes)
	}

	// Verify the updated pubkey is reflected in pairing info
	updatedPair, err := client.GetPairingInfo()
	if err != nil {
		t.Fatalf("GetPairingInfo after migration failed: %v", err)
	}
	if updatedPair.LaptopPubKey != expectedPub {
		t.Fatalf("Expected laptop pubkey %s after migration, got %s", expectedPub, updatedPair.LaptopPubKey)
	}

	// 6. Verify config persistence on disk
	cfgPath := filepath.Join(tempDir, "config.json")
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("Expected config.json to exist at %s: %v", cfgPath, err)
	}
}
