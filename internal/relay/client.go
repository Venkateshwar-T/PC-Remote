package relay

import (
	"crypto/tls"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"laptopcontrol/internal/config"
	"laptopcontrol/internal/win32"
)

var DefaultRelays = []string{
	"wss://relay.damus.io",
}

type Packet struct {
	ID        string      `json:"id"`
	Action    string      `json:"action"`
	Pin       string      `json:"pin"`
	NewPin    string      `json:"new_pin,omitempty"`
	Token     string      `json:"token"`
	Timestamp int64       `json:"timestamp"`
	Status    string      `json:"status,omitempty"`
	Message   string      `json:"message,omitempty"`
	Error     string      `json:"error,omitempty"`
	Telemetry interface{} `json:"telemetry,omitempty"`
}

type Client struct {
	cfg        *config.Config
	relays     []string
	conns      map[string]*websocket.Conn
	mu         sync.Mutex
	stopChan   chan struct{}
	actionChan chan Packet
}

func NewClient(cfg *config.Config, relayUrls []string) *Client {
	if len(relayUrls) == 0 {
		relayUrls = DefaultRelays
	}
	return &Client{
		cfg:        cfg,
		relays:     relayUrls,
		conns:      make(map[string]*websocket.Conn),
		stopChan:   make(chan struct{}),
		actionChan: make(chan Packet, 10),
	}
}

func (c *Client) Start() {
	for _, r := range c.relays {
		go c.maintainConnection(r)
	}
}

func (c *Client) Stop() {
	close(c.stopChan)
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, conn := range c.conns {
		conn.Close()
	}
}

func (c *Client) maintainConnection(url string) {
	dialer := websocket.Dialer{
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: false},
		HandshakeTimeout: 5 * time.Second,
		ReadBufferSize:   1024,
		WriteBufferSize:  1024,
	}

	for {
		select {
		case <-c.stopChan:
			return
		default:
		}

		conn, _, err := dialer.Dial(url, http.Header{
			"User-Agent": []string{"PCRemote/2.0"},
		})
		if err != nil {
			time.Sleep(10 * time.Second)
			continue
		}

		c.mu.Lock()
		c.conns[url] = conn
		c.mu.Unlock()

		log.Printf("[Relay] Connected to %s", url)

		// Read loop
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				break
			}
			c.handleMessage(conn, msg)
		}

		conn.Close()
		c.mu.Lock()
		delete(c.conns, url)
		c.mu.Unlock()

		time.Sleep(5 * time.Second)
	}
}

func (c *Client) handleMessage(conn *websocket.Conn, raw []byte) {
	var p Packet
	if err := json.Unmarshal(raw, &p); err != nil {
		return
	}

	// Validate action
	if p.Action == "" {
		return
	}

	// Validate PIN
	valid, errMsg := c.cfg.VerifyPin(p.Pin)
	if !valid {
		reply := Packet{
			ID:     p.ID,
			Status: "unauthorized",
			Error:  errMsg,
		}
		c.sendReply(conn, reply)
		return
	}

	var reply Packet
	reply.ID = p.ID
	reply.Status = "ok"

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
		reply.Error = "Unknown command"
	}

	c.sendReply(conn, reply)
}

func (c *Client) sendReply(conn *websocket.Conn, p Packet) {
	data, err := json.Marshal(p)
	if err != nil {
		return
	}
	conn.WriteMessage(websocket.TextMessage, data)
}

func isDigitsOnly(s string) bool {
	if len(s) != 6 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
