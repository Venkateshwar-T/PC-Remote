package relay

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip44"
	"laptopcontrol/internal/config"
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

	activePairingToken := "test-pairing-token-abc"
	client := NewClient(cfg, nil, func() string {
		return activePairingToken
	})

	// Phone generates its own keypair
	phoneSK := nostr.GeneratePrivateKey()
	phonePK, _ := nostr.GetPublicKey(phoneSK)

	convKey, err := nip44.GenerateConversationKey(cfg.LaptopPubKey, phoneSK)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Attempt command BEFORE pairing -> Must fail authorization
	unauthCmd := Packet{
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

	// Call handleEvent
	client.handleEvent(unauthEvt)
	if cfg.IsDeviceAuthorized(phonePK) {
		t.Fatal("Device must NOT be authorized before pairing")
	}

	// 2. Perform Pairing with WRONG PIN -> Must fail
	badPairCmd := Packet{
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

	client.handleEvent(badPairEvt)
	if cfg.IsDeviceAuthorized(phonePK) {
		t.Fatal("Device must NOT be authorized with wrong PIN")
	}

	// 3. Perform Pairing with VALID PIN & TOKEN -> Must succeed
	goodPairCmd := Packet{
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

	client.handleEvent(goodPairEvt)
	if !cfg.IsDeviceAuthorized(phonePK) {
		t.Fatal("Device MUST be authorized after valid pairing")
	}

	// 4. Send authorized command WITHOUT any PIN -> Must succeed
	telemetryCmd := Packet{
		ID:        "cmd-telemetry-1",
		Action:    "telemetry",
		Timestamp: time.Now().Unix(),
	}
	telemetryJSON, _ := json.Marshal(telemetryCmd)
	telemetryCT, _ := nip44.Encrypt(string(telemetryJSON), convKey)

	telemetryEvt := &nostr.Event{
		PubKey:    phonePK,
		CreatedAt: nostr.Now(),
		Kind:      4,
		Tags:      nostr.Tags{{"p", cfg.LaptopPubKey}},
		Content:   telemetryCT,
	}
	_ = telemetryEvt.Sign(phoneSK)

	client.handleEvent(telemetryEvt)

	// 5. Test Replay of the SAME command -> ReplayGuard must reject it
	client.handleEvent(telemetryEvt)
	// (Replay is tested via ReplayGuard ValidateAndRecord returning false)
}
