package ipc

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestIPCComprehensive(t *testing.T) {
	pipeName := fmt.Sprintf(`\\.\pipe\TestPCRemoteIPC_%d`, time.Now().UnixNano())

	var tokenGen int32 = 1
	var pinConfigured bool = false
	var savedPin string

	handlers := Handlers{
		GetStatus: func() (*StatusResult, error) {
			return &StatusResult{
				Running:     true,
				Version:     "2.0.0",
				DeviceName:  "TestLaptop",
				PairedCount: 2,
				Port:        8765,
				Uptime:      "1d 2h",
			}, nil
		},
		GetPairingInfo: func() (*PairingInfoResult, error) {
			curr := atomic.LoadInt32(&tokenGen)
			return &PairingInfoResult{
				URL:          fmt.Sprintf("https://pc-remote-45t.pages.dev/#pair=token_%d", curr),
				Token:        fmt.Sprintf("token_%d", curr),
				ExpiryUnix:   time.Now().Add(5 * time.Minute).Unix(),
				LaptopPubKey: "test_pubkey_12345",
			}, nil
		},
		RotatePairingToken: func() (*PairingInfoResult, error) {
			curr := atomic.AddInt32(&tokenGen, 1)
			return &PairingInfoResult{
				URL:          fmt.Sprintf("https://pc-remote-45t.pages.dev/#pair=token_%d", curr),
				Token:        fmt.Sprintf("token_%d", curr),
				ExpiryUnix:   time.Now().Add(5 * time.Minute).Unix(),
				LaptopPubKey: "test_pubkey_12345",
			}, nil
		},
		GetPinStatus: func() (*PinStatusResult, error) {
			return &PinStatusResult{IsSet: pinConfigured}, nil
		},
		SetPin: func(oldPin, newPin string) (*SetPinResult, error) {
			if len(newPin) != 6 {
				return nil, fmt.Errorf("PIN must be 6 digits")
			}
			if pinConfigured && oldPin != savedPin {
				return nil, fmt.Errorf("incorrect previous PIN")
			}
			savedPin = newPin
			pinConfigured = true
			return &SetPinResult{Success: true, Message: "PIN updated"}, nil
		},
		MigrateLegacyKey: func(privateKey string) (*MigrateKeyResult, error) {
			if len(privateKey) != 64 {
				return nil, fmt.Errorf("invalid 64-char hex private key")
			}
			return &MigrateKeyResult{Success: true, Message: "Key migrated"}, nil
		},
		StopService: func() (*StopServiceResult, error) {
			return &StopServiceResult{Success: true}, nil
		},
	}

	server := NewServer(pipeName, handlers)
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start IPC server: %v", err)
	}
	defer func() {
		_ = server.Stop()
	}()

	client := NewClient(pipeName)
	defer client.Close()

	// 1. Test GetStatus
	status, err := client.GetStatus()
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if !status.Running || status.DeviceName != "TestLaptop" || status.PairedCount != 2 {
		t.Fatalf("Unexpected status output: %+v", status)
	}

	// 2. Test GetPairingInfo and RotatePairingToken
	pairInfo, err := client.GetPairingInfo()
	if err != nil {
		t.Fatalf("GetPairingInfo failed: %v", err)
	}
	if pairInfo.Token != "token_1" {
		t.Fatalf("Expected token_1, got: %s", pairInfo.Token)
	}

	rotated, err := client.RotatePairingToken()
	if err != nil {
		t.Fatalf("RotatePairingToken failed: %v", err)
	}
	if rotated.Token != "token_2" {
		t.Fatalf("Expected token_2 after rotation, got: %s", rotated.Token)
	}

	// 3. Test PIN status and update
	pinStat, err := client.GetPinStatus()
	if err != nil || pinStat.IsSet {
		t.Fatalf("Expected PIN not set initially: %v, %+v", err, pinStat)
	}

	// Invalid PIN (not 6 digits)
	_, err = client.SetPin("", "123")
	if err == nil {
		t.Fatalf("Expected error setting short PIN")
	}

	// Valid PIN
	setRes, err := client.SetPin("", "123456")
	if err != nil || !setRes.Success {
		t.Fatalf("Failed to set PIN: %v, %+v", err, setRes)
	}

	pinStat, err = client.GetPinStatus()
	if err != nil || !pinStat.IsSet {
		t.Fatalf("Expected PIN to be marked set: %v, %+v", err, pinStat)
	}

	// 4. Test MigrateLegacyKey
	_, err = client.MigrateLegacyKey("badkey")
	if err == nil {
		t.Fatalf("Expected error migrating invalid short key")
	}

	dummy64Key := strings.Repeat("a", 64)
	migRes, err := client.MigrateLegacyKey(dummy64Key)
	if err != nil || !migRes.Success {
		t.Fatalf("Expected successful key migration: %v, %+v", err, migRes)
	}

	// 5. Test Strict Method Allowlist: Unauthorized / Arbitrary execution must be blocked!
	forbiddenMethods := []string{
		"ExecuteCommand",
		"RunPowerShell",
		"SpawnProcess",
		"ArbitraryAction",
		"SystemLockNoAuth",
	}
	for _, m := range forbiddenMethods {
		var dummy interface{}
		err := client.Call(m, nil, &dummy)
		if err == nil {
			t.Fatalf("Security failure: method %q was NOT rejected by allowlist!", m)
		}
		if !strings.Contains(err.Error(), "Forbidden or unknown method") {
			t.Fatalf("Unexpected error for forbidden method %q: %v", m, err)
		}
	}

	// 6. Test StopService
	stopRes, err := client.StopService()
	if err != nil || !stopRes.Success {
		t.Fatalf("Failed to call StopService: %v, %+v", err, stopRes)
	}
}

func TestIPCOversizedPayloadRejection(t *testing.T) {
	pipeName := fmt.Sprintf(`\\.\pipe\TestPCRemoteOversized_%d`, time.Now().UnixNano())

	server := NewServer(pipeName, Handlers{
		GetStatus: func() (*StatusResult, error) {
			return &StatusResult{Running: true}, nil
		},
	})
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	client := NewClient(pipeName)
	defer client.Close()

	// Send oversized params > 64KB
	oversizedData := strings.Repeat("x", 70*1024)
	var dummy interface{}
	err := client.Call(MethodGetStatus, map[string]string{"blob": oversizedData}, &dummy)
	if err == nil {
		t.Fatalf("Expected rejection of payload exceeding 64KB limit")
	}
}
