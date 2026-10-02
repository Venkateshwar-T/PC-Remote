package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"sync"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip44"
	"laptopcontrol/internal/config"
	"laptopcontrol/internal/pairing"
	"laptopcontrol/internal/remote/session"
	"laptopcontrol/internal/win32"
)

const (
	MaxEventSize               = 64 * 1024 // 64 KB max serialized event size
	MaxNormalCiphertextSize     = 8 * 1024  // 8 KB max ciphertext length for normal commands
	MaxSignalingCiphertextSize  = 32 * 1024 // 32 KB max ciphertext length for remote WebRTC signaling
	MaxCiphertextSize          = MaxSignalingCiphertextSize // Global envelope upper bound (32 KB)

	MaxNormalPlaintextSize      = 4 * 1024  // 4 KB max decrypted plaintext size for normal commands
	MaxSignalingPlaintextSize   = 32 * 1024 // 32 KB max decrypted plaintext size for remote WebRTC signaling
	MaxPlaintextSize           = MaxNormalPlaintextSize // Maintained for backwards-compatibility (4 KB)

	MaxDeviceNameLen           = 64
)

var (
	reqIDRegex = regexp.MustCompile(`^[a-zA-Z0-9_\-]{6,64}$`)

	allowedActions = map[string]bool{
		"pair":           true,
		"telemetry":      true,
		"lock":           true,
		"sleep":          true,
		"restart":        true,
		"shutdown":       true,
		"change_pin":     true, // handled explicitly to reject remote attempts
		"remote_request": true,
		"remote_signal":  true,
	}
)

type CommandPacket struct {
	ID         string                   `json:"id"`
	Action     string                   `json:"action"`
	Pin        string                   `json:"pin,omitempty"`
	Token      string                   `json:"token,omitempty"`
	DeviceName string                   `json:"deviceName,omitempty"`
	Signal     *session.SignalingPacket `json:"signal,omitempty"`
	Timestamp  int64                    `json:"timestamp"`
}

type ResponsePacket struct {
	ID            string                   `json:"id"`
	Status        string                   `json:"status"` // "ok", "error", "unauthorized", "busy", "accepted"
	Message       string                   `json:"message,omitempty"`
	Error         string                   `json:"error,omitempty"`
	Telemetry     interface{}              `json:"telemetry,omitempty"`
	SessionID     string                   `json:"sessionId,omitempty"`
	AuthChallenge string                   `json:"authChallenge,omitempty"`
	Signal        *session.SignalingPacket `json:"signal,omitempty"`
	Timestamp     int64                    `json:"timestamp"`
}

// RemoteManagerInterface defines the interaction contract for Remote Desktop.
type RemoteManagerInterface interface {
	HandleSessionRequest(reqID, phonePubKey string) (*session.SessionResponse, error)
	HandleSignaling(phonePubKey string, packet session.SignalingPacket) (*session.SignalingPacket, error)
}

// Handler provides unified cryptographic command validation, authorization, and execution
// regardless of whether the transport is direct LAN HTTP or Nostr relay.
type Handler struct {
	cfg              *config.Config
	pairingMgr       *pairing.Manager
	replayGuard      *ReplayGuard
	remoteMgr        RemoteManagerInterface
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

// SetRemoteManager binds the Remote Desktop session manager to the protocol handler.
func (h *Handler) SetRemoteManager(mgr RemoteManagerInterface) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.remoteMgr = mgr
}

// ProcessCommandEvent processes an incoming signed Nostr command event through the unified security pipeline.
// Returns a signed, NIP-44 encrypted response Nostr event.
func (h *Handler) ProcessCommandEvent(evt *nostr.Event) (*nostr.Event, error) {
	if evt == nil {
		return nil, errors.New("nil event provided")
	}

	// 1. Strict envelope ciphertext size validation (bounded to MaxSignalingCiphertextSize)
	if len(evt.Content) > MaxSignalingCiphertextSize {
		return nil, fmt.Errorf("ciphertext size (%d bytes) exceeds maximum limit of %d bytes", len(evt.Content), MaxSignalingCiphertextSize)
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

	if len(plaintext) > MaxSignalingPlaintextSize {
		return nil, fmt.Errorf("plaintext size (%d bytes) exceeds maximum limit of %d bytes", len(plaintext), MaxSignalingPlaintextSize)
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

	// 5b. Action-specific size & schema validation
	if cmd.Action != "remote_signal" {
		if len(evt.Content) > MaxNormalCiphertextSize {
			return nil, fmt.Errorf("ciphertext size (%d bytes) exceeds limit of %d bytes for non-signaling action '%s'", len(evt.Content), MaxNormalCiphertextSize, cmd.Action)
		}
		if len(plaintext) > MaxNormalPlaintextSize {
			return nil, fmt.Errorf("plaintext size (%d bytes) exceeds limit of %d bytes for non-signaling action '%s'", len(plaintext), MaxNormalPlaintextSize, cmd.Action)
		}
	} else {
		if cmd.Signal == nil {
			return nil, errors.New("remote_signal action requires non-nil signal payload")
		}
		if err := cmd.Signal.Validate(); err != nil {
			return nil, fmt.Errorf("invalid signaling payload: %w", err)
		}
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

	case "remote_request":
		reqStart := time.Now()
		log.Printf("[RemoteReq Timing] [Req: %s] remote_request received", cmd.ID)
		h.mu.RLock()
		rm := h.remoteMgr
		h.mu.RUnlock()
		if rm == nil {
			return h.BuildEncryptedResponse(evt.PubKey, cmd.ID, "error", "", "Remote Desktop capability is uninitialized on host", nil)
		}
		resp, err := rm.HandleSessionRequest(cmd.ID, evt.PubKey)
		if err != nil {
			log.Printf("[RemoteReq Timing] [Req: %s] HandleSessionRequest failed after %v: %v", cmd.ID, time.Since(reqStart), err)
			return h.BuildEncryptedResponse(evt.PubKey, cmd.ID, "error", "", err.Error(), nil)
		}
		respEvt, buildErr := h.BuildEncryptedResponsePacket(evt.PubKey, ResponsePacket{
			ID:            cmd.ID,
			Status:        resp.Status,
			SessionID:     resp.SessionID,
			AuthChallenge: resp.AuthChallenge,
			Message:       resp.Message,
			Timestamp:     time.Now().Unix(),
		})
		log.Printf("[RemoteReq Timing] [Req: %s] [Session: %s] response prepared (elapsed since receive: %v)", cmd.ID, resp.SessionID, time.Since(reqStart))
		return respEvt, buildErr

	case "remote_signal":
		h.mu.RLock()
		rm := h.remoteMgr
		h.mu.RUnlock()
		if rm == nil || cmd.Signal == nil {
			return h.BuildEncryptedResponse(evt.PubKey, cmd.ID, "error", "", "Invalid or missing remote signaling packet", nil)
		}
		replySig, err := rm.HandleSignaling(evt.PubKey, *cmd.Signal)
		if err != nil {
			return h.BuildEncryptedResponse(evt.PubKey, cmd.ID, "error", "", err.Error(), nil)
		}
		if replySig != nil {
			log.Printf("[RemoteMgr] Session %s signaling ACK sent for %s", cmd.Signal.SessionID, replySig.Type)
			return h.BuildEncryptedResponsePacket(evt.PubKey, ResponsePacket{
				ID:        cmd.ID,
				Status:    "ok",
				Signal:    replySig,
				Timestamp: time.Now().Unix(),
			})
		}
		log.Printf("[RemoteMgr] Session %s signaling ACK sent for %s", cmd.Signal.SessionID, cmd.Signal.Type)
		return h.BuildEncryptedResponsePacket(evt.PubKey, ResponsePacket{
			ID:        cmd.ID,
			Status:    "ok",
			Message:   "signaling forwarded",
			Timestamp: time.Now().Unix(),
		})

	default:
		return h.BuildEncryptedResponse(evt.PubKey, cmd.ID, "error", "", "Unknown action requested", nil)
	}
}

// BuildEncryptedResponsePacket constructs a signed, NIP-44 encrypted Nostr event from a ResponsePacket.
func (h *Handler) BuildEncryptedResponsePacket(recipientPubKey string, resp ResponsePacket) (*nostr.Event, error) {
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

// BuildEncryptedResponse constructs a signed, NIP-44 encrypted Nostr event reply.
func (h *Handler) BuildEncryptedResponse(recipientPubKey, reqID, status, msg, errMsg string, telemetry interface{}) (*nostr.Event, error) {
	return h.BuildEncryptedResponsePacket(recipientPubKey, ResponsePacket{
		ID:        reqID,
		Status:    status,
		Message:   msg,
		Error:     errMsg,
		Telemetry: telemetry,
		Timestamp: time.Now().Unix(),
	})
}

