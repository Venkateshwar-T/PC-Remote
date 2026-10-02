package protocol

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip44"
	"laptopcontrol/internal/config"
	"laptopcontrol/internal/pairing"
	"laptopcontrol/internal/remote/session"
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

	pairMgr, err := pairing.NewManager()
	if err != nil {
		t.Fatalf("pairing.NewManager failed: %v", err)
	}
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

type mockRemoteManager struct {
	handleSignalingFunc func(phonePubKey string, packet session.SignalingPacket) (*session.SignalingPacket, error)
}

func (m *mockRemoteManager) HandleSessionRequest(reqID, phonePubKey string) (*session.SessionResponse, error) {
	return &session.SessionResponse{Status: "ready", SessionID: "sess-123"}, nil
}

func (m *mockRemoteManager) HandleSignaling(phonePubKey string, packet session.SignalingPacket) (*session.SignalingPacket, error) {
	if m.handleSignalingFunc != nil {
		return m.handleSignalingFunc(phonePubKey, packet)
	}
	return nil, nil
}

func TestProtocol_RemoteSignalImmediateAck(t *testing.T) {
	cfg, _, _, handler := setupTestEnvironment(t)

	clientPrivKey := nostr.GeneratePrivateKey()
	clientPubKey, _ := nostr.GetPublicKey(clientPrivKey)
	_ = cfg.AuthorizeDevice(clientPubKey, "Authorized Phone")

	mockRM := &mockRemoteManager{
		handleSignalingFunc: func(phonePubKey string, packet session.SignalingPacket) (*session.SignalingPacket, error) {
			// Simulates asynchronous forwarding: worker receives offer/candidate, returns nil
			return nil, nil
		},
	}
	handler.SetRemoteManager(mockRM)

	cmd := CommandPacket{
		ID:        "req-sig-001",
		Action:    "remote_signal",
		Signal: &session.SignalingPacket{
			Type:      session.SignalOffer,
			SessionID: "sess-123",
			SDP:       "v=0\r\no=- 0 0 IN IP4 127.0.0.1...",
		},
		Timestamp: time.Now().Unix(),
	}
	evt := createClientEvent(t, clientPrivKey, cfg.LaptopPubKey, cmd)

	respEvt, err := handler.ProcessCommandEvent(evt)
	if err != nil {
		t.Fatalf("ProcessCommandEvent failed: %v", err)
	}
	if respEvt == nil {
		t.Fatal("Expected immediate correlated ACK event, got nil (would cause client timeout)")
	}

	resp := decryptResponse(t, respEvt, clientPrivKey, cfg.LaptopPubKey)
	if resp.ID != cmd.ID {
		t.Fatalf("expected response ID %s, got %s", cmd.ID, resp.ID)
	}
	if resp.Status != "ok" {
		t.Fatalf("expected status ok, got %s", resp.Status)
	}
	if resp.Message != "signaling forwarded" {
		t.Fatalf("expected message 'signaling forwarded', got %s", resp.Message)
	}
}

func TestProtocol_RemoteRequestTiming(t *testing.T) {
	cfg, _, _, handler := setupTestEnvironment(t)

	clientPrivKey := nostr.GeneratePrivateKey()
	clientPubKey, _ := nostr.GetPublicKey(clientPrivKey)
	_ = cfg.AuthorizeDevice(clientPubKey, "Authorized Phone")

	mockRM := &mockRemoteManager{}
	handler.SetRemoteManager(mockRM)

	cmd := CommandPacket{
		ID:        "req-remotereq-001",
		Action:    "remote_request",
		Timestamp: time.Now().Unix(),
	}
	evt := createClientEvent(t, clientPrivKey, cfg.LaptopPubKey, cmd)

	respEvt, err := handler.ProcessCommandEvent(evt)
	if err != nil {
		t.Fatalf("ProcessCommandEvent failed: %v", err)
	}
	if respEvt == nil {
		t.Fatal("Expected response event for remote_request, got nil")
	}

	resp := decryptResponse(t, respEvt, clientPrivKey, cfg.LaptopPubKey)
	if resp.ID != cmd.ID {
		t.Fatalf("expected ID %s, got %s", cmd.ID, resp.ID)
	}
	if resp.Status != "ready" {
		t.Fatalf("expected status ready, got %s", resp.Status)
	}
	if resp.SessionID != "sess-123" {
		t.Fatalf("expected SessionID sess-123, got %s", resp.SessionID)
	}
}

func TestProtocol_NormalCommandSmallLimits(t *testing.T) {
	cfg, _, _, handler := setupTestEnvironment(t)

	clientPrivKey := nostr.GeneratePrivateKey()
	clientPubKey, _ := nostr.GetPublicKey(clientPrivKey)
	_ = cfg.AuthorizeDevice(clientPubKey, "Authorized Phone")

	// 1. Normal command exceeding 4 KB plaintext must be rejected
	oversizedPin := strings.Repeat("A", 4500)
	cmdOversized := CommandPacket{
		ID:        "req-limit-001",
		Action:    "telemetry",
		Pin:       oversizedPin,
		Timestamp: time.Now().Unix(),
	}
	evtOversized := createClientEvent(t, clientPrivKey, cfg.LaptopPubKey, cmdOversized)
	_, err := handler.ProcessCommandEvent(evtOversized)
	if err == nil {
		t.Fatal("Expected oversized normal command to be rejected, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("Expected limit error message, got: %v", err)
	}

	// 2. Normal command within 4 KB plaintext must be accepted
	cmdValid := CommandPacket{
		ID:        "req-limit-002",
		Action:    "telemetry",
		Timestamp: time.Now().Unix(),
	}
	evtValid := createClientEvent(t, clientPrivKey, cfg.LaptopPubKey, cmdValid)
	respEvt, err := handler.ProcessCommandEvent(evtValid)
	if err != nil {
		t.Fatalf("Valid normal command should succeed: %v", err)
	}
	resp := decryptResponse(t, respEvt, clientPrivKey, cfg.LaptopPubKey)
	if resp.Status != "ok" {
		t.Fatalf("Expected status ok, got %s", resp.Status)
	}
}

func TestProtocol_RemoteSignalLargeAccepted(t *testing.T) {
	cfg, _, _, handler := setupTestEnvironment(t)

	clientPrivKey := nostr.GeneratePrivateKey()
	clientPubKey, _ := nostr.GetPublicKey(clientPrivKey)
	_ = cfg.AuthorizeDevice(clientPubKey, "Authorized Phone")

	mockRM := &mockRemoteManager{}
	handler.SetRemoteManager(mockRM)

	// Construct realistic large SDP offer (~6 KB plaintext), which encrypts to > 8 KB ciphertext
	largeSDP := "v=0\r\no=- 123456789 2 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\n" + strings.Repeat("a=candidate:1 1 UDP 2130706431 192.168.1.100 50000 typ host\r\n", 100)
	if len(largeSDP) < 5000 {
		t.Fatalf("Test setup error: largeSDP too small: %d", len(largeSDP))
	}

	cmd := CommandPacket{
		ID:        "req-sig-large",
		Action:    "remote_signal",
		Signal: &session.SignalingPacket{
			Type:      session.SignalOffer,
			SessionID: "sess-123",
			SDP:       largeSDP,
		},
		Timestamp: time.Now().Unix(),
	}
	evt := createClientEvent(t, clientPrivKey, cfg.LaptopPubKey, cmd)

	// Verify that ciphertext exceeds 8 KB (reproducing the observed 9648-byte issue)
	if len(evt.Content) <= 8192 {
		t.Fatalf("Expected ciphertext > 8192 bytes, got %d", len(evt.Content))
	}

	respEvt, err := handler.ProcessCommandEvent(evt)
	if err != nil {
		t.Fatalf("Large remote_signal (>4KB plaintext / >8KB ciphertext) must be accepted, got: %v", err)
	}
	if respEvt == nil {
		t.Fatal("Expected response event, got nil")
	}

	resp := decryptResponse(t, respEvt, clientPrivKey, cfg.LaptopPubKey)
	if resp.Status != "ok" {
		t.Fatalf("Expected status ok, got %s", resp.Status)
	}
}

func TestProtocol_RemoteSignalOversizedSDPRejected(t *testing.T) {
	cfg, _, _, handler := setupTestEnvironment(t)

	clientPrivKey := nostr.GeneratePrivateKey()
	clientPubKey, _ := nostr.GetPublicKey(clientPrivKey)
	_ = cfg.AuthorizeDevice(clientPubKey, "Authorized Phone")

	mockRM := &mockRemoteManager{}
	handler.SetRemoteManager(mockRM)

	// SDP exceeding 64 KB
	hugeSDP := strings.Repeat("a=candidate:1 1 UDP 2130706431 192.168.1.100 50000 typ host\r\n", 1000)

	cmd := CommandPacket{
		ID:        "req-sig-huge",
		Action:    "remote_signal",
		Signal: &session.SignalingPacket{
			Type:      session.SignalOffer,
			SessionID: "sess-123",
			SDP:       hugeSDP,
		},
		Timestamp: time.Now().Unix(),
	}
	evt := createClientEvent(t, clientPrivKey, cfg.LaptopPubKey, cmd)

	_, err := handler.ProcessCommandEvent(evt)
	if err == nil {
		t.Fatal("Expected huge SDP to be rejected, got nil")
	}
}

