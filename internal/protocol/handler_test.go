package protocol

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip44"
	"laptopcontrol/internal/config"
	"laptopcontrol/internal/pairing"
)

func setupTestEnvironment(t *testing.T) (*config.Config, *pairing.Manager, *ReplayGuard, *Handler) {
	tempDir, err := os.MkdirTemp("", "protocol_test_*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tempDir) })

	cfg, err := config.LoadOrCreate(tempDir)
	if err != nil {
		t.Fatalf("LoadOrCreate failed: %v", err)
	}
	_ = cfg.SetPin("123456")

	pairMgr := pairing.NewManager()
	replayGuard := NewReplayGuard(120)
	handler := NewHandler(cfg, pairMgr, replayGuard)

	return cfg, pairMgr, replayGuard, handler
}

func createClientEvent(t *testing.T, clientPrivKey, laptopPubKey string, cmd CommandPacket) *nostr.Event {
	clientPubKey, err := nostr.GetPublicKey(clientPrivKey)
	if err != nil {
		t.Fatal(err)
	}

	payloadBytes, err := json.Marshal(cmd)
	if err != nil {
		t.Fatal(err)
	}

	convKey, err := nip44.GenerateConversationKey(laptopPubKey, clientPrivKey)
	if err != nil {
		t.Fatal(err)
	}

	ciphertext, err := nip44.Encrypt(string(payloadBytes), convKey)
	if err != nil {
		t.Fatal(err)
	}

	evt := nostr.Event{
		PubKey:    clientPubKey,
		CreatedAt: nostr.Timestamp(cmd.Timestamp),
		Kind:      4,
		Tags:      nostr.Tags{{"p", laptopPubKey}},
		Content:   ciphertext,
	}

	if err := evt.Sign(clientPrivKey); err != nil {
		t.Fatal(err)
	}

	return &evt
}

func decryptResponse(t *testing.T, respEvt *nostr.Event, clientPrivKey, laptopPubKey string) ResponsePacket {
	if respEvt.PubKey != laptopPubKey {
		t.Fatalf("Expected response from laptop %s, got %s", laptopPubKey, respEvt.PubKey)
	}

	valid, err := respEvt.CheckSignature()
	if err != nil || !valid {
		t.Fatalf("Invalid response signature: %v", err)
	}

	convKey, err := nip44.GenerateConversationKey(laptopPubKey, clientPrivKey)
	if err != nil {
		t.Fatal(err)
	}

	plaintext, err := nip44.Decrypt(respEvt.Content, convKey)
	if err != nil {
		t.Fatalf("Failed to decrypt response: %v", err)
	}

	var resp ResponsePacket
	if err := json.Unmarshal([]byte(plaintext), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response packet: %v", err)
	}
	return resp
}

func TestProtocol_PairingAndAuthorizedFlow(t *testing.T) {
	cfg, pairMgr, _, handler := setupTestEnvironment(t)

	clientPrivKey := nostr.GeneratePrivateKey()
	clientPubKey, _ := nostr.GetPublicKey(clientPrivKey)
	token := pairMgr.GetToken()

	// 1. Initial Pairing Command
	pairCmd := CommandPacket{
		ID:         "req-pair-001",
		Action:     "pair",
		Token:      token,
		Pin:        "123456",
		DeviceName: "Test Phone",
		Timestamp:  time.Now().Unix(),
	}
	evt := createClientEvent(t, clientPrivKey, cfg.LaptopPubKey, pairCmd)

	respEvt, err := handler.ProcessCommandEvent(evt)
	if err != nil {
		t.Fatalf("Pairing event failed: %v", err)
	}

	resp := decryptResponse(t, respEvt, clientPrivKey, cfg.LaptopPubKey)
	if resp.Status != "ok" || resp.Message != "Device paired successfully" {
		t.Fatalf("Unexpected pairing response: %+v", resp)
	}

	// Verify device is authorized
	if !cfg.IsDeviceAuthorized(clientPubKey) {
		t.Fatal("Device was not marked authorized on laptop")
	}

	// 2. Token Single-Use: Attempt pairing again with same token -> MUST BE REJECTED
	pairCmd2 := CommandPacket{
		ID:         "req-pair-002",
		Action:     "pair",
		Token:      token,
		Pin:        "123456",
		DeviceName: "Test Phone",
		Timestamp:  time.Now().Unix(),
	}
	evt2 := createClientEvent(t, clientPrivKey, cfg.LaptopPubKey, pairCmd2)
	respEvt2, err := handler.ProcessCommandEvent(evt2)
	if err != nil {
		t.Fatalf("ProcessCommandEvent error: %v", err)
	}
	resp2 := decryptResponse(t, respEvt2, clientPrivKey, cfg.LaptopPubKey)
	if resp2.Status != "unauthorized" {
		t.Fatalf("Expected token reuse to be unauthorized, got: %+v", resp2)
	}

	// 3. Authorized Remote Command: Telemetry (No PIN sent or required!)
	telemCmd := CommandPacket{
		ID:        "req-telem-001",
		Action:    "telemetry",
		Timestamp: time.Now().Unix(),
	}
	evtTelem := createClientEvent(t, clientPrivKey, cfg.LaptopPubKey, telemCmd)
	respTelemEvt, err := handler.ProcessCommandEvent(evtTelem)
	if err != nil {
		t.Fatalf("Telemetry command failed: %v", err)
	}
	respTelem := decryptResponse(t, respTelemEvt, clientPrivKey, cfg.LaptopPubKey)
	if respTelem.Status != "ok" || respTelem.Telemetry == nil {
		t.Fatalf("Expected telemetry payload, got: %+v", respTelem)
	}
}

func TestProtocol_UnauthorizedDeviceRejected(t *testing.T) {
	cfg, _, _, handler := setupTestEnvironment(t)

	unauthorizedPrivKey := nostr.GeneratePrivateKey()

	cmd := CommandPacket{
		ID:        "req-unauth-001",
		Action:    "lock",
		Timestamp: time.Now().Unix(),
	}
	evt := createClientEvent(t, unauthorizedPrivKey, cfg.LaptopPubKey, cmd)

	respEvt, err := handler.ProcessCommandEvent(evt)
	if err != nil {
		t.Fatalf("ProcessCommandEvent failed: %v", err)
	}

	resp := decryptResponse(t, respEvt, unauthorizedPrivKey, cfg.LaptopPubKey)
	if resp.Status != "unauthorized" {
		t.Fatalf("Expected unauthorized status for unregistered phone, got: %+v", resp)
	}
}

func TestProtocol_ModifiedSignatureRejected(t *testing.T) {
	cfg, _, _, handler := setupTestEnvironment(t)

	clientPrivKey := nostr.GeneratePrivateKey()
	cmd := CommandPacket{
		ID:        "req-tamper-001",
		Action:    "telemetry",
		Timestamp: time.Now().Unix(),
	}
	evt := createClientEvent(t, clientPrivKey, cfg.LaptopPubKey, cmd)

	// Tamper with signature
	evt.Sig = "00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"

	_, err := handler.ProcessCommandEvent(evt)
	if err == nil {
		t.Fatal("Tampered event signature must be rejected")
	}
}

func TestProtocol_ModifiedCiphertextRejected(t *testing.T) {
	cfg, _, _, handler := setupTestEnvironment(t)

	clientPrivKey := nostr.GeneratePrivateKey()
	cmd := CommandPacket{
		ID:        "req-tamper-002",
		Action:    "telemetry",
		Timestamp: time.Now().Unix(),
	}
	evt := createClientEvent(t, clientPrivKey, cfg.LaptopPubKey, cmd)

	// Corrupt the ciphertext and re-sign with client's key
	evt.Content = evt.Content + "deadbeef"
	_ = evt.Sign(clientPrivKey)

	_, err := handler.ProcessCommandEvent(evt)
	if err == nil {
		t.Fatal("Corrupted ciphertext must fail decryption and be rejected")
	}
}

func TestProtocol_ChangePinRemotelyForbidden(t *testing.T) {
	cfg, _, _, handler := setupTestEnvironment(t)

	clientPrivKey := nostr.GeneratePrivateKey()
	clientPubKey, _ := nostr.GetPublicKey(clientPrivKey)
	_ = cfg.AuthorizeDevice(clientPubKey, "Authorized Phone")

	cmd := CommandPacket{
		ID:        "req-pinchange-001",
		Action:    "change_pin",
		Timestamp: time.Now().Unix(),
	}
	evt := createClientEvent(t, clientPrivKey, cfg.LaptopPubKey, cmd)

	respEvt, err := handler.ProcessCommandEvent(evt)
	if err != nil {
		t.Fatalf("ProcessCommandEvent failed: %v", err)
	}

	resp := decryptResponse(t, respEvt, clientPrivKey, cfg.LaptopPubKey)
	if resp.Status != "error" || resp.Error == "" {
		t.Fatalf("change_pin must be rejected remotely, got: %+v", resp)
	}
}
