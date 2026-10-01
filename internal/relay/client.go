package relay

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip04"
	"github.com/nbd-wtf/go-nostr/nip44"
	"laptopcontrol/internal/config"
	"laptopcontrol/internal/win32"
)

var DefaultRelays = []string{
	"wss://relay.damus.io",
	"wss://nos.lol",
	"wss://relay.primal.net",
}

type Packet struct {
	ID         string      `json:"id"`
	Action     string      `json:"action"`
	Pin        string      `json:"pin,omitempty"`
	NewPin     string      `json:"new_pin,omitempty"`
	Token      string      `json:"token,omitempty"`
	DeviceName string      `json:"deviceName,omitempty"`
	Timestamp  int64       `json:"timestamp"`
	Status     string      `json:"status,omitempty"`
	Message    string      `json:"message,omitempty"`
	Error      string      `json:"error,omitempty"`
	Telemetry  interface{} `json:"telemetry,omitempty"`
}

type Client struct {
	cfg                *config.Config
	relays             []string
	pool               *nostr.SimplePool
	ctx                context.Context
	cancel             context.CancelFunc
	replayGuard        *ReplayGuard
	pairingTokenGetter func() string
	mu                 sync.Mutex
	running            bool
}

// NewClient creates a new production Nostr relay client
func NewClient(cfg *config.Config, relayUrls []string, pairingTokenGetter func() string) *Client {
	if len(relayUrls) == 0 {
		relayUrls = DefaultRelays
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{
		cfg:                cfg,
		relays:             relayUrls,
		ctx:                ctx,
		cancel:             cancel,
		pool:               nostr.NewSimplePool(ctx),
		replayGuard:        NewReplayGuard(120),
		pairingTokenGetter: pairingTokenGetter,
	}
}

// Start launches the background listener across the relay pool
func (c *Client) Start() {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	c.running = true
	c.mu.Unlock()

	log.Printf("[Relay] Starting Nostr E2EE client for PubKey: %s", c.cfg.LaptopPubKey)
	for _, r := range c.relays {
		log.Printf("[Relay] Configured relay: %s", r)
	}

	go c.listenLoop()
}

// Stop gracefully shuts down connections and listeners
func (c *Client) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.running {
		return
	}
	c.running = false
	if c.cancel != nil {
		c.cancel()
	}
	if c.pool != nil {
		c.pool.Close("service shutdown")
	}
	log.Println("[Relay] Nostr relay client stopped cleanly")
}

func (c *Client) listenLoop() {
	// Subscribe to direct message events (Kind 4) tagged with laptop's public key
	// Listen for events created since 60 seconds ago to avoid processing stale historical messages
	since := nostr.Timestamp(time.Now().Unix() - 60)
	filter := nostr.Filter{
		Kinds: []int{4},
		Tags:  nostr.TagMap{"p": []string{c.cfg.LaptopPubKey}},
		Since: &since,
	}

	subChan := c.pool.SubscribeMany(c.ctx, c.relays, filter)

	// Bounded worker semaphore to prevent unbounded goroutine explosion
	sem := make(chan struct{}, 16)

	for {
		select {
		case <-c.ctx.Done():
			return
		case relayEvt, ok := <-subChan:
			if !ok {
				return
			}
			if relayEvt.Event == nil {
				continue
			}

			// Process event asynchronously with bounded concurrency
			select {
			case sem <- struct{}{}:
				go func(evt *nostr.Event) {
					defer func() { <-sem }()
					c.handleEvent(evt)
				}(relayEvt.Event)
			case <-c.ctx.Done():
				return
			}
		}
	}
}

func (c *Client) handleEvent(evt *nostr.Event) {
	if evt == nil {
		return
	}

	// 1. Cryptographic BIP-340 Schnorr signature verification
	validSig, err := evt.CheckSignature()
	if err != nil || !validSig {
		log.Printf("[Relay] Dropped event %s: invalid signature", evt.ID)
		return
	}

	// 2. Decrypt Content (Try NIP-44 modern encryption first, fallback to NIP-04)
	var plaintext string
	isNip44 := false

	convKey, err := nip44.GenerateConversationKey(evt.PubKey, c.cfg.LaptopPrivKey)
	if err == nil {
		decrypted, errDec := nip44.Decrypt(evt.Content, convKey)
		if errDec == nil {
			plaintext = decrypted
			isNip44 = true
		}
	}

	if !isNip44 {
		// Fallback to NIP-04
		sharedSecret, errSS := nip04.ComputeSharedSecret(evt.PubKey, c.cfg.LaptopPrivKey)
		if errSS == nil {
			decrypted, errDec := nip04.Decrypt(evt.Content, sharedSecret)
			if errDec == nil {
				plaintext = decrypted
			}
		}
	}

	if plaintext == "" {
		// Event was not decryptable with our private key
		return
	}

	// 3. Parse command packet
	var p Packet
	if err := json.Unmarshal([]byte(plaintext), &p); err != nil {
		return
	}

	if p.ID == "" || p.Action == "" {
		return
	}

	// 4. Replay Guard & Freshness Verification
	// Fall back to event CreatedAt if client didn't supply inner timestamp
	ts := p.Timestamp
	if ts <= 0 {
		ts = int64(evt.CreatedAt)
	}

	ok, errMsg := c.replayGuard.ValidateAndRecord(p.ID, ts)
	if !ok {
		log.Printf("[Relay] Dropped event %s (action: %s): %s", p.ID, p.Action, errMsg)
		return
	}

	reply := Packet{
		ID:     p.ID,
		Status: "ok",
	}

	// 5. Authorization & Action Routing
	if p.Action == "pair" {
		// Pairing Handshake Flow:
		// 1. Verify transient pairing token from QR code
		// 2. Verify Master PIN
		expectedToken := ""
		if c.pairingTokenGetter != nil {
			expectedToken = c.pairingTokenGetter()
		}

		if p.Token == "" || p.Token != expectedToken {
			reply.Status = "unauthorized"
			reply.Error = "Invalid or expired pairing token. Scan QR code again."
			c.sendReply(evt.PubKey, reply, isNip44)
			return
		}

		validPin, pinErr := c.cfg.VerifyPin(p.Pin)
		if !validPin {
			reply.Status = "unauthorized"
			reply.Error = pinErr
			c.sendReply(evt.PubKey, reply, isNip44)
			return
		}

		devName := p.DeviceName
		if devName == "" {
			devName = "Phone Remote"
		}
		if err := c.cfg.AuthorizeDevice(evt.PubKey, devName); err != nil {
			reply.Status = "error"
			reply.Error = "Failed to store authorized device"
		} else {
			reply.Status = "ok"
			reply.Message = "Device paired successfully"
			reply.Telemetry = win32.QueryTelemetry(c.cfg.DeviceName)
			log.Printf("[Relay] New device authorized: %s (%s)", devName, evt.PubKey[:12])
		}
		c.sendReply(evt.PubKey, reply, isNip44)
		return
	}

	// For all standard remote commands: strictly enforce device authorization
	if !c.cfg.IsDeviceAuthorized(evt.PubKey) {
		reply.Status = "unauthorized"
		reply.Error = "Device is not authorized. Please pair your phone first."
		c.sendReply(evt.PubKey, reply, isNip44)
		return
	}

	// Update device last seen
	c.cfg.UpdateDeviceLastSeen(evt.PubKey)

	// Execute authorized Win32 actions
	switch p.Action {
	case "telemetry":
		reply.Telemetry = win32.QueryTelemetry(c.cfg.DeviceName)

	case "lock":
		win32.LockScreen()
		reply.Message = "Workstation locked"

	case "sleep":
		win32.SuspendSystem()
		reply.Message = "Entering sleep mode"

	case "restart":
		win32.RebootSystem()
		reply.Message = "Restart initiated"

	case "shutdown":
		win32.PowerOffSystem()
		reply.Message = "Shutdown initiated"

	case "change_pin":
		reply.Status = "error"
		reply.Error = "For security, Master PIN can only be changed directly on the PC desktop."

	default:
		reply.Status = "error"
		reply.Error = "Unknown action requested"
	}

	c.sendReply(evt.PubKey, reply, isNip44)
}

func (c *Client) sendReply(recipientPubKey string, p Packet, useNip44 bool) {
	data, err := json.Marshal(p)
	if err != nil {
		return
	}

	var ciphertext string
	if useNip44 {
		convKey, err := nip44.GenerateConversationKey(recipientPubKey, c.cfg.LaptopPrivKey)
		if err != nil {
			return
		}
		ct, err := nip44.Encrypt(string(data), convKey)
		if err != nil {
			return
		}
		ciphertext = ct
	} else {
		secret, err := nip04.ComputeSharedSecret(recipientPubKey, c.cfg.LaptopPrivKey)
		if err != nil {
			return
		}
		ct, err := nip04.Encrypt(string(data), secret)
		if err != nil {
			return
		}
		ciphertext = ct
	}

	// Create and sign response Nostr event
	respEvt := nostr.Event{
		PubKey:    c.cfg.LaptopPubKey,
		CreatedAt: nostr.Now(),
		Kind:      4,
		Tags:      nostr.Tags{{"p", recipientPubKey}},
		Content:   ciphertext,
	}

	if err := respEvt.Sign(c.cfg.LaptopPrivKey); err != nil {
		log.Printf("[Relay] Failed to sign response event: %v", err)
		return
	}

	// Publish response to all active relays concurrently
	pubCtx, pubCancel := context.WithTimeout(c.ctx, 4*time.Second)
	defer pubCancel()

	results := c.pool.PublishMany(pubCtx, c.relays, respEvt)
	// Consume results to ensure goroutines finish
	for range results {
	}
}
