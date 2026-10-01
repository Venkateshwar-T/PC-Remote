package tray

import (
	"bytes"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"laptopcontrol/internal/ipc"
	"laptopcontrol/internal/pairing"
)

func TestTrayManager_DynamicPairingQRRotation(t *testing.T) {
	pairMgr, err := pairing.NewManager()
	if err != nil {
		t.Fatal(err)
	}

	pipeName := fmt.Sprintf(`\\.\pipe\TestTrayIPC_%d`, time.Now().UnixNano())

	var tokenGen int32 = 1
	tokenA := pairMgr.GetToken()

	handlers := ipc.Handlers{
		GetPairingInfo: func() (*ipc.PairingInfoResult, error) {
			curr := atomic.LoadInt32(&tokenGen)
			var tok string
			if curr == 1 {
				tok = tokenA
			} else {
				tok = pairMgr.GetToken()
			}
			return &ipc.PairingInfoResult{
				URL:          fmt.Sprintf("https://pc-remote-45t.pages.dev/#pair=%s&name=TestDevice", tok),
				Token:        tok,
				ExpiryUnix:   time.Now().Add(5 * time.Minute).Unix(),
				LaptopPubKey: "testpubkey123",
			}, nil
		},
		SetPin: func(oldPin, newPin string) (*ipc.SetPinResult, error) {
			return &ipc.SetPinResult{Success: true}, nil
		},
	}

	server := ipc.NewServer(pipeName, handlers)
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start IPC server: %v", err)
	}
	defer server.Stop()

	client := ipc.NewClient(pipeName)
	defer client.Close()

	// 1. Initial State: QR contains Token A
	mgr := NewManager(client, "TestDevice", true, nil)
	initialURL := mgr.GetCurrentPairingURL()

	if !strings.Contains(initialURL, tokenA) {
		t.Fatalf("expected initial URL to contain Token A (%s), got: %s", tokenA, initialURL)
	}

	mgr.qrMu.RLock()
	initialPixels := make([]byte, len(mgr.qrPixels))
	copy(initialPixels, mgr.qrPixels)
	mgr.qrMu.RUnlock()

	if len(initialPixels) == 0 {
		t.Fatal("expected initial QR pixels to be populated")
	}

	// 2. Rotate token in service
	tokenB, errRegen := pairMgr.Regenerate()
	if errRegen != nil {
		t.Fatal(errRegen)
	}
	atomic.StoreInt32(&tokenGen, 2)

	// 3. Trigger RefreshPairingQR (as occurs when ShowPairingWindow is called or pairing window repaints)
	mgr.RefreshPairingQR()
	rotatedURL := mgr.GetCurrentPairingURL()

	if !strings.Contains(rotatedURL, tokenB) {
		t.Fatalf("expected rotated URL to contain Token B (%s), got: %s", tokenB, rotatedURL)
	}
	if strings.Contains(rotatedURL, tokenA) {
		t.Fatal("rotated URL must no longer contain consumed Token A")
	}

	mgr.qrMu.RLock()
	rotatedPixels := make([]byte, len(mgr.qrPixels))
	copy(rotatedPixels, mgr.qrPixels)
	mgr.qrMu.RUnlock()

	if bytes.Equal(initialPixels, rotatedPixels) {
		t.Fatal("QR pixels must be re-rendered and differ between Token A and Token B")
	}
}
