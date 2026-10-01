package relay

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip44"
	"laptopcontrol/internal/config"
	"laptopcontrol/internal/pairing"
	"laptopcontrol/internal/protocol"
)

func TestNostrFlow_PairingAndAuthorizedCommands(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pcremote_nostr_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	cfg, err := config.LoadOrCreate(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	_ = cfg.SetPin("123456")

	pairMgr := pairing.NewManager()
	replayGuard := protocol.NewReplayGuard(120)
	handler := protocol.NewHandler(cfg, pairMgr, replayGuard)
	client := NewClient(cfg, nil, handler)

	activePairingToken := pairMgr.GetToken()

	// Phone generates its own keypair
	phoneSK := nostr.GeneratePrivateKey()
	phonePK, _ := nostr.GetPublicKey(phoneSK)

	convKey, err := nip44.GenerateConversationKey(cfg.LaptopPubKey, phoneSK)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Attempt command BEFORE pairing -> Must fail authorization
	unauthCmd := protocol.CommandPacket{
		ID:        "cmd-unauth-1",
		Action:    "telemetry",
		Timestamp: time.Now().Unix(),
	}
	unauthJSON, _ := json.Marshal(unauthCmd)
	unauthCT, _ := nip44.Encrypt(string(unauthJSON), convKey)

	unauthEvt := &nostr.Event{
		PubKey:    phonePK,
		CreatedAt: nostr.Now(),
		Kind:      4,
		Tags:      nostr.Tags{{"p", cfg.LaptopPubKey}},
		Content:   unauthCT,
	}
	_ = unauthEvt.Sign(phoneSK)

	client.HandleEvent(unauthEvt)
	if cfg.IsDeviceAuthorized(phonePK) {
		t.Fatal("Device must NOT be authorized before pairing")
	}

	// 2. Perform Pairing with WRONG PIN -> Must fail
	badPairCmd := protocol.CommandPacket{
		ID:         "cmd-pair-bad",
		Action:     "pair",
		Token:      activePairingToken,
		Pin:        "999999", // wrong PIN
		DeviceName: "Phone Test",
		Timestamp:  time.Now().Unix(),
	}
	badPairJSON, _ := json.Marshal(badPairCmd)
	badPairCT, _ := nip44.Encrypt(string(badPairJSON), convKey)

	badPairEvt := &nostr.Event{
		PubKey:    phonePK,
		CreatedAt: nostr.Now(),
		Kind:      4,
		Tags:      nostr.Tags{{"p", cfg.LaptopPubKey}},
		Content:   badPairCT,
	}
	_ = badPairEvt.Sign(phoneSK)

	client.HandleEvent(badPairEvt)
	if cfg.IsDeviceAuthorized(phonePK) {
		t.Fatal("Device must NOT be authorized with wrong PIN")
	}

	// 3. Perform Pairing with VALID PIN & TOKEN -> Must succeed
	goodPairCmd := protocol.CommandPacket{
		ID:         "cmd-pair-good",
		Action:     "pair",
		Token:      activePairingToken,
		Pin:        "123456",
		DeviceName: "Phone Test",
		Timestamp:  time.Now().Unix(),
	}
	goodPairJSON, _ := json.Marshal(goodPairCmd)
	goodPairCT, _ := nip44.Encrypt(string(goodPairJSON), convKey)

	goodPairEvt := &nostr.Event{
		PubKey:    phonePK,
		CreatedAt: nostr.Now(),
		Kind:      4,
		Tags:      nostr.Tags{{"p", cfg.LaptopPubKey}},
		Content:   goodPairCT,
	}
	_ = goodPairEvt.Sign(phoneSK)

	client.HandleEvent(goodPairEvt)
	if !cfg.IsDeviceAuthorized(phonePK) {
		t.Fatal("Device must be authorized after successful pairing handshake")
	}

	// 4. Send Authorized Command after Pairing -> Must succeed
	authCmd := protocol.CommandPacket{
		ID:        "cmd-auth-telemetry",
		Action:    "telemetry",
		Timestamp: time.Now().Unix(),
	}
	authJSON, _ := json.Marshal(authCmd)
	authCT, _ := nip44.Encrypt(string(authJSON), convKey)

	authEvt := &nostr.Event{
		PubKey:    phonePK,
		CreatedAt: nostr.Now(),
		Kind:      4,
		Tags:      nostr.Tags{{"p", cfg.LaptopPubKey}},
		Content:   authCT,
	}
	_ = authEvt.Sign(phoneSK)

	client.HandleEvent(authEvt)

	// 5. Test Replay of Authorized Command -> Must be dropped by ReplayGuard
	client.HandleEvent(authEvt)
}
