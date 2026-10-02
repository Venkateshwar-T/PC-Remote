package relay

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"laptopcontrol/internal/config"
	"laptopcontrol/internal/protocol"
)

var DefaultRelays = []string{
	"wss://relay.damus.io",
	"wss://nos.lol",
	"wss://relay.primal.net",
}

// Client manages persistent multi-relay WebSocket subscriptions and outbound publishing for Nostr events.
type Client struct {
	cfg     *config.Config
	relays  []string
	pool    *nostr.SimplePool
	ctx     context.Context
	cancel  context.CancelFunc
	handler *protocol.Handler
	mu      sync.Mutex
	running bool
}

// NewClient creates a new production Nostr relay client bound to the unified protocol handler.
func NewClient(cfg *config.Config, relayUrls []string, handler *protocol.Handler) *Client {
	if len(relayUrls) == 0 {
		relayUrls = DefaultRelays
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{
		cfg:     cfg,
		relays:  relayUrls,
		ctx:     ctx,
		cancel:  cancel,
		pool:    nostr.NewSimplePool(ctx),
		handler: handler,
	}
}

// Start launches the background listener across the relay pool.
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

// Stop gracefully shuts down connections and listeners.
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
	// Listen exclusively for live events created from this moment onwards to avoid processing historical offline backlogs
	since := nostr.Timestamp(time.Now().Unix())
	filter := nostr.Filter{
		Kinds: []int{4},
		Tags:  nostr.TagMap{"p": []string{c.cfg.LaptopPubKey}},
		Since: &since,
	}

	subChan := c.pool.SubscribeMany(c.ctx, c.relays, filter)

	// Bounded worker semaphore: limits concurrent cryptographic operations to 8
	// prevents malicious relays from causing CPU/goroutine exhaustion
	sem := make(chan struct{}, 8)

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

			select {
			case sem <- struct{}{}:
				go func(evt *nostr.Event) {
					defer func() { <-sem }()
					c.HandleEvent(evt)
				}(relayEvt.Event)
			case <-c.ctx.Done():
				return
			}
		}
	}
}

// HandleEvent processes an incoming event through the unified protocol handler and publishes any reply.
func (c *Client) HandleEvent(evt *nostr.Event) {
	if evt == nil || c.handler == nil {
		return
	}

	respEvt, err := c.handler.ProcessCommandEvent(evt)
	if err != nil {
		log.Printf("[Relay] Event %s dropped: %v", evt.ID, err)
		return
	}

	if respEvt == nil {
		return
	}

	c.PublishEvent(respEvt)
}

// PublishEvent publishes a prepared Nostr event across all configured relays concurrently.
func (c *Client) PublishEvent(evt *nostr.Event) {
	if evt == nil || c.pool == nil {
		return
	}
	pubCtx, pubCancel := context.WithTimeout(c.ctx, 4*time.Second)
	defer pubCancel()

	results := c.pool.PublishMany(pubCtx, c.relays, *evt)
	for range results {
	}
}

