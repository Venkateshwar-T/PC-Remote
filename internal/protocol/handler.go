package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip44"
	"laptopcontrol/internal/config"
	"laptopcontrol/internal/pairing"
	"laptopcontrol/internal/win32"
)

const (
	MaxEventSize      = 16 * 1024 // 16 KB max serialized event size
	MaxCiphertextSize = 8 * 1024  // 8 KB max ciphertext length
	MaxPlaintextSize  = 4 * 1024  // 4 KB max decrypted plaintext size
	MaxDeviceNameLen  = 64
)

var (
	reqIDRegex = regexp.MustCompile(`^[a-zA-Z0-9_\-]{6,64}$`)

	allowedActions = map[string]bool{
		"pair":       true,
		"telemetry":  true,
		"lock":       true,
		"sleep":      true,
		"restart":    true,
		"shutdown":   true,
		"change_pin": true, // handled explicitly to reject remote attempts
	}
)

type CommandPacket struct {
	ID         string `json:"id"`
	Action     string `json:"action"`
	Pin        string `json:"pin,omitempty"`
	Token      string `json:"token,omitempty"`
	DeviceName string `json:"deviceName,omitempty"`
	Timestamp  int64  `json:"timestamp"`
}

type ResponsePacket struct {
	ID        string      `json:"id"`
	Status    string      `json:"status"` // "ok", "error", "unauthorized"
	Message   string      `json:"message,omitempty"`
	Error     string      `json:"error,omitempty"`
	Telemetry interface{} `json:"telemetry,omitempty"`
	Timestamp int64       `json:"timestamp"`
}

// Handler provides unified cryptographic command validation, authorization, and execution
// regardless of whether the transport is direct LAN HTTP or Nostr relay.
type Handler struct {
	cfg              *config.Config
	pairingMgr       *pairing.Manager
	replayGuard      *ReplayGuard
	onPairingSuccess func()
	startTime        time.Time
	mu               sync.RWMutex
}

func NewHandler(cfg *config.Config, pairingMgr *pairing.Manager, replayGuard *ReplayGuard) *Handler {
	if replayGuard == nil {
		replayGuard = NewReplayGuard(120)
	}
	return &Handler{
		cfg:         cfg,
		pairingMgr:  pairingMgr,
		replayGuard: replayGuard,
		startTime:   time.Now(),
	}
}

// SetOnPairingSuccess registers a thread-safe callback invoked whenever a new device pairs successfully.
func (h *Handler) SetOnPairingSuccess(fn func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onPairingSuccess = fn
}

// ProcessCommandEvent processes an incoming signed Nostr command event through the unified security pipeline.
// Returns a signed, NIP-44 encrypted response Nostr event.
func (h *Handler) ProcessCommandEvent(evt *nostr.Event) (*nostr.Event, error) {
	if evt == nil {
		return nil, errors.New("nil event provided")
	}

	// 1. Strict event size validation
	if len(evt.Content) > MaxCiphertextSize {
		return nil, fmt.Errorf("ciphertext size (%d bytes) exceeds limit of %d bytes", len(evt.Content), MaxCiphertextSize)
	}

	// 2. Validate event Kind and destination 'p' tag
	if evt.Kind != 4 {
		return nil, fmt.Errorf("unsupported event kind: %d (expected kind 4)", evt.Kind)
	}

	hasMatchingPTag := false
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "p" && tag[1] == h.cfg.LaptopPubKey {
			hasMatchingPTag = true
			break
		}
	}
	if !hasMatchingPTag {
		return nil, errors.New("event not addressed to this laptop public key")
	}

	// 3. Cryptographic BIP-340 Schnorr signature verification
	validSig, err := evt.CheckSignature()
	if err != nil || !validSig {
		return nil, errors.New("invalid BIP-340 Schnorr signature")
	}

	// 4. Strict NIP-44 v2 decryption (no legacy NIP-04 fallback)
	convKey, err := nip44.GenerateConversationKey(evt.PubKey, h.cfg.LaptopPrivKey)
	if err != nil {
		return nil, fmt.Errorf("conversation key derivation failed: %w", err)
	}

	plaintext, err := nip44.Decrypt(evt.Content, convKey)
	if err != nil {
		return nil, fmt.Errorf("NIP-44 decryption failed: %w", err)
	}

	if len(plaintext) > MaxPlaintextSize {
		return nil, fmt.Errorf("plaintext size (%d bytes) exceeds limit of %d bytes", len(plaintext), MaxPlaintextSize)
	}

	// 5. Parse and validate Command Packet schema
	var cmd CommandPacket
	if err := json.Unmarshal([]byte(plaintext), &cmd); err != nil {
		return nil, fmt.Errorf("malformed command JSON: %w", err)
	}

	if cmd.ID == "" || !reqIDRegex.MatchString(cmd.ID) {
		return nil, errors.New("missing or invalid request ID format")
	}

	if !allowedActions[cmd.Action] {
		return nil, fmt.Errorf("action '%s' is not permitted", cmd.Action)
	}

	// 6. Sender-scoped Replay Guard & Freshness Verification
	ts := cmd.Timestamp
	if ts <= 0 {
		ts = int64(evt.CreatedAt)
	}
	okReplay, replayErr := h.replayGuard.ValidateAndRecordScoped(evt.PubKey, cmd.ID, int64(evt.CreatedAt), ts)
	if !okReplay {
		return nil, fmt.Errorf("replay rejection: %s", replayErr)
	}

	// Power state modifications (shutdown, restart, sleep) must be strictly live:
	// 1. Must NOT precede service startup time (prevents execution of commands queued while PC was off/booting)
	// 2. Must not exceed 30 seconds of age
	if cmd.Action == "shutdown" || cmd.Action == "restart" || cmd.Action == "sleep" {
		if int64(evt.CreatedAt) < h.startTime.Unix() {
			return nil, fmt.Errorf("power command '%s' rejected: command was sent before service started (offline queue)", cmd.Action)
		}
		if time.Now().Unix()-int64(evt.CreatedAt) > 30 {
			return nil, fmt.Errorf("power command '%s' rejected: event expired (>30s old)", cmd.Action)
		}
	}

	// 7. Route and execute authorized actions
	if cmd.Action == "pair" {
		// Pairing Handshake Flow:
		// 1. Check transient pairing token is valid and unexpired
		if h.pairingMgr == nil {
			return h.BuildEncryptedResponse(evt.PubKey, cmd.ID, "error", "", "Pairing manager uninitialized", nil)
		}

		okToken, tokenErr := h.pairingMgr.Validate(cmd.Token)
		if !okToken {
			return h.BuildEncryptedResponse(evt.PubKey, cmd.ID, "unauthorized", "", tokenErr, nil)
		}

		// 2. Verify Master PIN
		okPin, pinErr := h.cfg.VerifyPin(cmd.Pin)
		if !okPin {
			return h.BuildEncryptedResponse(evt.PubKey, cmd.ID, "unauthorized", "", pinErr, nil)
		}

		// 3. Atomically consume the pairing token upon successful verification
		okConsume, consumeErr := h.pairingMgr.ValidateAndConsume(cmd.Token)
		if !okConsume {
			return h.BuildEncryptedResponse(evt.PubKey, cmd.ID, "unauthorized", "", consumeErr, nil)
		}

		// Authorize phone public key
		devName := cmd.DeviceName
		if len(devName) > MaxDeviceNameLen {
			devName = devName[:MaxDeviceNameLen]
		}
		if devName == "" {
			devName = "Phone Remote"
		}

		if err := h.cfg.AuthorizeDevice(evt.PubKey, devName); err != nil {
			return h.BuildEncryptedResponse(evt.PubKey, cmd.ID, "error", "", "Failed to authorize device on laptop", nil)
		}

		h.mu.RLock()
		onSuccess := h.onPairingSuccess
		h.mu.RUnlock()
		if onSuccess != nil {
			go onSuccess()
		}

		telemetry := win32.QueryTelemetry(h.cfg.DeviceName)
		return h.BuildEncryptedResponse(evt.PubKey, cmd.ID, "ok", "Device paired successfully", "", telemetry)
	}

	// Non-pairing commands: strictly enforce server-side device authorization
	if !h.cfg.IsDeviceAuthorized(evt.PubKey) {
		return h.BuildEncryptedResponse(evt.PubKey, cmd.ID, "unauthorized", "", "Device is not authorized. Please pair your phone first.", nil)
	}

	// Update last seen
	h.cfg.UpdateDeviceLastSeen(evt.PubKey)

	// Execute Win32 action
	switch cmd.Action {
	case "telemetry":
		telemetry := win32.QueryTelemetry(h.cfg.DeviceName)
		return h.BuildEncryptedResponse(evt.PubKey, cmd.ID, "ok", "", "", telemetry)

	case "lock":
		win32.LockScreen()
		return h.BuildEncryptedResponse(evt.PubKey, cmd.ID, "ok", "Workstation locked", "", nil)

	case "sleep":
		win32.SuspendSystem()
		return h.BuildEncryptedResponse(evt.PubKey, cmd.ID, "ok", "Entering sleep mode", "", nil)

	case "restart":
		win32.RebootSystem()
		return h.BuildEncryptedResponse(evt.PubKey, cmd.ID, "ok", "Restart initiated", "", nil)

	case "shutdown":
		win32.PowerOffSystem()
		return h.BuildEncryptedResponse(evt.PubKey, cmd.ID, "ok", "Shutdown initiated", "", nil)

	case "change_pin":
		return h.BuildEncryptedResponse(evt.PubKey, cmd.ID, "error", "", "For security, Master PIN can only be changed directly on the PC desktop.", nil)

	default:
		return h.BuildEncryptedResponse(evt.PubKey, cmd.ID, "error", "", "Unknown action requested", nil)
	}
}

// BuildEncryptedResponse constructs a signed, NIP-44 encrypted Nostr event reply.
func (h *Handler) BuildEncryptedResponse(recipientPubKey, reqID, status, msg, errMsg string, telemetry interface{}) (*nostr.Event, error) {
	resp := ResponsePacket{
		ID:        reqID,
		Status:    status,
		Message:   msg,
		Error:     errMsg,
		Telemetry: telemetry,
		Timestamp: time.Now().Unix(),
	}

	data, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response packet: %w", err)
	}

	// Encrypt strictly using NIP-44 v2
	convKey, err := nip44.GenerateConversationKey(recipientPubKey, h.cfg.LaptopPrivKey)
	if err != nil {
		return nil, fmt.Errorf("failed to derive conversation key for response: %w", err)
	}

	ciphertext, err := nip44.Encrypt(string(data), convKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt response: %w", err)
	}

	respEvt := &nostr.Event{
		PubKey:    h.cfg.LaptopPubKey,
		CreatedAt: nostr.Now(),
		Kind:      4,
		Tags:      nostr.Tags{{"p", recipientPubKey}},
		Content:   ciphertext,
	}

	if err := respEvt.Sign(h.cfg.LaptopPrivKey); err != nil {
		return nil, fmt.Errorf("failed to sign response event: %w", err)
	}

	return respEvt, nil
}
