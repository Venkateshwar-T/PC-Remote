package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip44"
	"laptopcontrol/internal/config"
	"laptopcontrol/internal/pairing"
	"laptopcontrol/internal/protocol"
)

func setupTestServer(t *testing.T) (*Server, *config.Config, *pairing.Manager) {
	tempDir, err := os.MkdirTemp("", "server_test_*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tempDir) })

	cfg, err := config.LoadOrCreate(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	_ = cfg.SetPin("123456")

	pairMgr, err := pairing.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	replayGuard := protocol.NewReplayGuard(120)
	handler := protocol.NewHandler(cfg, pairMgr, replayGuard)
	srv := NewServer(cfg, nil, 8765, pairMgr, handler)

	return srv, cfg, pairMgr
}

func TestServer_PlaintextCommandRejected(t *testing.T) {
	srv, _, _ := setupTestServer(t)

	// Send legacy plaintext request to /api/control
	plaintextReq := map[string]interface{}{
		"action": "telemetry",
		"pin":    "123456",
	}
	body, _ := json.Marshal(plaintextReq)
	req := httptest.NewRequest(http.MethodPost, "/api/control", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401 Unauthorized for plaintext command, got: %d", w.Code)
	}

	var resp map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "Plaintext commands rejected. PC Remote requires cryptographically signed & encrypted Nostr envelope." {
		t.Fatalf("Unexpected rejection message: %s", resp["error"])
	}
}

func TestServer_CryptographicEnvelopeAcceptedOverLAN(t *testing.T) {
	srv, cfg, pairMgr := setupTestServer(t)

	clientPrivKey := nostr.GeneratePrivateKey()
	clientPubKey, _ := nostr.GetPublicKey(clientPrivKey)

	// Pair device first
	pairCmd := protocol.CommandPacket{
		ID:         "req-lan-pair",
		Action:     "pair",
		Token:      pairMgr.GetToken(),
		Pin:        "123456",
		DeviceName: "LAN Phone",
		Timestamp:  time.Now().Unix(),
	}
	payloadBytes, _ := json.Marshal(pairCmd)
	convKey, _ := nip44.GenerateConversationKey(cfg.LaptopPubKey, clientPrivKey)
	ciphertext, _ := nip44.Encrypt(string(payloadBytes), convKey)

	evt := nostr.Event{
		PubKey:    clientPubKey,
		CreatedAt: nostr.Now(),
		Kind:      4,
		Tags:      nostr.Tags{{"p", cfg.LaptopPubKey}},
		Content:   ciphertext,
	}
	_ = evt.Sign(clientPrivKey)

	body, _ := json.Marshal(evt)
	req := httptest.NewRequest(http.MethodPost, "/api/control", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for cryptographic envelope over LAN, got: %d (%s)", w.Code, w.Body.String())
	}

	// Response must be a valid signed Nostr event
	var respEvt nostr.Event
	if err := json.Unmarshal(w.Body.Bytes(), &respEvt); err != nil {
		t.Fatalf("Response is not a valid Nostr event: %v", err)
	}

	valid, err := respEvt.CheckSignature()
	if err != nil || !valid {
		t.Fatalf("Response signature invalid: %v", err)
	}
}

func TestServer_SanitizedStatus(t *testing.T) {
	srv, _, _ := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()

	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for /api/status, got: %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	if resp["status"] != "running" {
		t.Fatalf("Expected status=running, got: %v", resp["status"])
	}

	// Must NOT contain sensitive leaks
	if _, exists := resp["pairingToken"]; exists {
		t.Fatal("SECURITY LEAK: pairingToken leaked in /api/status!")
	}
	if _, exists := resp["token"]; exists {
		t.Fatal("SECURITY LEAK: token leaked in /api/status!")
	}
	if _, exists := resp["telemetry"]; exists {
		t.Fatal("SECURITY LEAK: telemetry leaked in /api/status!")
	}
}

func TestServer_LANControlConcurrencyBounding(t *testing.T) {
	srv, _, _ := setupTestServer(t)

	// Set small concurrency limit of 2 for testing overload behavior
	srv.SetControlConcurrency(2)

	// Saturate the 2 concurrency slots manually
	srv.controlSem <- struct{}{}
	srv.controlSem <- struct{}{}

	// Excess 3rd request to /api/control must fail fast with HTTP 429
	reqControl := httptest.NewRequest(http.MethodPost, "/api/control", bytes.NewReader([]byte("{}")))
	wControl := httptest.NewRecorder()
	srv.ServeHTTP(wControl, reqControl)

	if wControl.Code != http.StatusTooManyRequests {
		t.Fatalf("Expected 429 Too Many Requests when saturated, got: %d", wControl.Code)
	}

	var errResp map[string]string
	_ = json.Unmarshal(wControl.Body.Bytes(), &errResp)
	if errResp["error"] == "" {
		t.Fatal("Expected error message in 429 response")
	}

	// Crucial check: unrelated /api/status MUST NOT be blocked or throttled by /api/control saturation!
	reqStatus := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	wStatus := httptest.NewRecorder()
	srv.ServeHTTP(wStatus, reqStatus)

	if wStatus.Code != http.StatusOK {
		t.Fatalf("Unrelated /api/status must remain 200 OK during control saturation, got: %d", wStatus.Code)
	}

	// Release one slot and verify /api/control accepts requests again
	<-srv.controlSem

	// Send request now: should pass semaphore and fail on invalid payload (401), NOT 429
	wControl2 := httptest.NewRecorder()
	reqControl2 := httptest.NewRequest(http.MethodPost, "/api/control", bytes.NewReader([]byte("{}")))
	srv.ServeHTTP(wControl2, reqControl2)

	if wControl2.Code == http.StatusTooManyRequests {
		t.Fatal("Request should have been accepted by semaphore after slot release")
	}
	if wControl2.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401 Unauthorized for empty payload, got: %d", wControl2.Code)
	}

	// Clean up second held slot
	<-srv.controlSem
}
