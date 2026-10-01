package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nbd-wtf/go-nostr"
)

func TestConfig_KeyGenerationAndDerivation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pcremote_cfg_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	cfg, err := LoadOrCreate(tempDir)
	if err != nil {
		t.Fatalf("LoadOrCreate failed: %v", err)
	}

	if cfg.LaptopPrivKey == "" || cfg.LaptopPubKey == "" {
		t.Fatal("Expected keys to be non-empty")
	}

	// Verify that laptopPubKey is the mathematical public key of laptopPrivKey
	expectedPub, err := nostr.GetPublicKey(cfg.LaptopPrivKey)
	if err != nil {
		t.Fatalf("Failed to derive public key: %v", err)
	}
	if cfg.LaptopPubKey != expectedPub {
		t.Fatalf("Public key mismatch! Got %s, expected %s", cfg.LaptopPubKey, expectedPub)
	}

	// Verify that LaptopPrivKey is NOT in config.json in plaintext
	data, err := os.ReadFile(filepath.Join(tempDir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), cfg.LaptopPrivKey) {
		t.Fatal("SECURITY VIOLATION: Plaintext LaptopPrivKey found in config.json!")
	}
	if !strings.Contains(string(data), "laptopPrivKeyEncrypted") {
		t.Fatal("Expected laptopPrivKeyEncrypted field in config.json")
	}

	// Verify reload decrypts key correctly
	cfg2, err := LoadOrCreate(tempDir)
	if err != nil {
		t.Fatalf("Reload failed: %v", err)
	}
	if cfg2.LaptopPrivKey != cfg.LaptopPrivKey {
		t.Fatalf("Reloaded private key mismatch! Got %s, expected %s", cfg2.LaptopPrivKey, cfg.LaptopPrivKey)
	}
	if cfg2.LaptopPubKey != cfg.LaptopPubKey {
		t.Fatalf("Reloaded public key mismatch! Got %s, expected %s", cfg2.LaptopPubKey, cfg.LaptopPubKey)
	}
}

func TestConfig_MigrationFromLegacyPlaintextKey(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pcremote_cfg_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	legacyPrivKey := nostr.GeneratePrivateKey()
	legacyPubKey, _ := nostr.GetPublicKey(legacyPrivKey)

	// Write legacy config with plaintext laptopPrivKey
	legacyJSON := map[string]interface{}{
		"deviceName":    "Test Laptop",
		"laptopPrivKey": legacyPrivKey,
		"laptopPubKey":  legacyPubKey,
	}
	rawBytes, _ := json.Marshal(legacyJSON)
	_ = os.WriteFile(filepath.Join(tempDir, "config.json"), rawBytes, 0600)

	// Load should trigger migration
	cfg, err := LoadOrCreate(tempDir)
	if err != nil {
		t.Fatalf("LoadOrCreate on legacy config failed: %v", err)
	}

	if cfg.LaptopPrivKey != legacyPrivKey {
		t.Fatalf("Migrated private key mismatch! Got %s, expected %s", cfg.LaptopPrivKey, legacyPrivKey)
	}
	if cfg.LaptopPubKey != legacyPubKey {
		t.Fatalf("Migrated public key mismatch! Got %s, expected %s", cfg.LaptopPubKey, legacyPubKey)
	}

	// Verify disk file no longer contains plaintext key
	savedData, _ := os.ReadFile(filepath.Join(tempDir, "config.json"))
	if strings.Contains(string(savedData), legacyPrivKey) {
		t.Fatal("Migration failed to purge plaintext private key from disk!")
	}
	if !strings.Contains(string(savedData), "laptopPrivKeyEncrypted") {
		t.Fatal("Migration failed to generate laptopPrivKeyEncrypted")
	}
}

func TestConfig_DeviceAuthorization(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pcremote_cfg_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	cfg, err := LoadOrCreate(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	testPhoneKey := "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

	// Initially, when no devices are registered, IsDeviceAuthorized MUST return false
	if cfg.IsDeviceAuthorized(testPhoneKey) {
		t.Fatal("Unregistered device must NOT be authorized when list is empty")
	}

	// Authorize device
	if err := cfg.AuthorizeDevice(testPhoneKey, "Test Phone"); err != nil {
		t.Fatalf("AuthorizeDevice failed: %v", err)
	}

	if !cfg.IsDeviceAuthorized(testPhoneKey) {
		t.Fatal("Device must be authorized after AuthorizeDevice")
	}

	// Verify persistence
	cfg2, err := LoadOrCreate(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg2.IsDeviceAuthorized(testPhoneKey) {
		t.Fatal("Device authorization must persist to disk")
	}

	// Revoke device
	if err := cfg.RevokeDevice(testPhoneKey); err != nil {
		t.Fatal(err)
	}
	if cfg.IsDeviceAuthorized(testPhoneKey) {
		t.Fatal("Device must NOT be authorized after revocation")
	}
}

func TestConfig_PinVerification(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pcremote_cfg_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	cfg, _ := LoadOrCreate(tempDir)
	_ = cfg.SetPin("123456")

	valid, _ := cfg.VerifyPin("123456")
	if !valid {
		t.Fatal("Correct PIN should pass verification")
	}

	invalid, errMsg := cfg.VerifyPin("000000")
	if invalid {
		t.Fatal("Incorrect PIN must fail verification")
	}
	if errMsg == "" {
		t.Fatal("Expected error message for incorrect PIN")
	}
}
