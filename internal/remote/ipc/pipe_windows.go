//go:build windows

package ipc

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	rootipc "laptopcontrol/internal/ipc"
)

// SessionPipeServer manages the daemon side of the session named pipe.
type SessionPipeServer struct {
	pipeName       string
	sessionID      string
	userSID        string
	listener       *rootipc.PipeListener
	conn           *rootipc.PipeConn
	reader         *bufio.Reader
	mu             sync.Mutex
	closed         bool
	onMessage      func(env *AgentEnvelope)
	onClientClosed func()
}

// NewSessionPipeServer creates and listens on a dedicated named pipe for this session.
func NewSessionPipeServer(pipeName, sessionID, userSID string) (*SessionPipeServer, error) {
	listener, err := rootipc.ListenPipe(pipeName, userSID)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on session pipe %s: %w", pipeName, err)
	}

	return &SessionPipeServer{
		pipeName:  pipeName,
		sessionID: sessionID,
		userSID:   userSID,
		listener:  listener,
	}, nil
}

// SetOnMessage registers a callback for incoming messages from the agent.
func (s *SessionPipeServer) SetOnMessage(fn func(env *AgentEnvelope)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onMessage = fn
}

// SetOnClientClosed registers a callback invoked when the agent disconnects.
func (s *SessionPipeServer) SetOnClientClosed(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onClientClosed = fn
}

// Accept blocks until the session-agent connects, reads the handshake hello, and passes identity verification.
func (s *SessionPipeServer) Accept() error {
	conn, err := s.listener.Accept()
	if err != nil {
		return err
	}

	reader := bufio.NewReader(conn)

	// Read hello line bounded by MaxHelloMessageSize and HelloTimeout
	type readResult struct {
		data []byte
		err  error
	}
	ch := make(chan readResult, 1)
	go func() {
		buf := make([]byte, 0, 512)
		for len(buf) < MaxHelloMessageSize {
			b, err := reader.ReadByte()
			if err != nil {
				ch <- readResult{nil, err}
				return
			}
			if b == '\n' {
				ch <- readResult{buf, nil}
				return
			}
			buf = append(buf, b)
		}
		ch <- readResult{nil, errors.New("oversized agent hello handshake")}
	}()

	var line []byte
	select {
	case res := <-ch:
		if res.err != nil {
			_ = conn.Close()
			return fmt.Errorf("failed to read agent hello handshake: %w", res.err)
		}
		line = res.data
	case <-time.After(HelloTimeout):
		_ = conn.Close() // unblocks the goroutine and terminates immediately
		return errors.New("timeout waiting for agent hello handshake")
	}

	// Parse and validate the hello handshake envelope
	_, err = ParseAndValidateHello(line, s.sessionID)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("agent hello handshake rejected: %w", err)
	}

	log.Printf("[SessionPipeServer] [Session: %s] hello received (%d bytes)", s.sessionID, len(line))

	// ONLY AFTER DATA HAS BEEN READ: Verify client identity via Win32 impersonation
	ident, err := rootipc.GetClientIdentity(conn.Handle())
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("failed to verify session-agent identity: %w", err)
	}

	log.Printf("[SessionPipeServer] [Session: %s] identity verified for agent (SID: %s, IsSystem: %t, IsAdmin: %t)", s.sessionID, ident.SID, ident.IsSystem, ident.IsAdmin)

	// Authorized if client matches configured userSID, or is SYSTEM / Administrator
	if s.userSID != "" && ident.SID != s.userSID && !ident.IsSystem && !ident.IsAdmin {
		_ = conn.Close()
		return fmt.Errorf("unauthorized process (SID %s) connected to session pipe", ident.SID)
	}

	// Store connection and EXACT SAME buffered reader, then launch background readLoop
	s.mu.Lock()
	s.conn = conn
	s.reader = reader
	s.mu.Unlock()

	go s.readLoop()
	return nil
}

// Send serializes and writes an envelope to the agent.
func (s *SessionPipeServer) Send(msgType AgentMessageType, sessionID string, payload interface{}) error {
	data, err := EncodeEnvelope(msgType, sessionID, payload)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.conn == nil {
		return errors.New("agent not connected to session pipe")
	}

	_, err = s.conn.Write(data)
	return err
}

func (s *SessionPipeServer) readLoop() {
	defer func() {
		s.mu.Lock()
		isClosed := s.closed
		cb := s.onClientClosed
		s.mu.Unlock()
		if !isClosed && cb != nil {
			cb()
		}
	}()

	for {
		s.mu.Lock()
		reader := s.reader
		closed := s.closed
		s.mu.Unlock()

		if closed || reader == nil {
			return
		}

		line, err := reader.ReadBytes('\n')
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				log.Printf("[SessionPipe] Read error: %v", err)
			}
			return
		}

		var env AgentEnvelope
		if err := json.Unmarshal(line, &env); err != nil {
			log.Printf("[SessionPipe] Malformed envelope JSON: %v", err)
			continue
		}

		s.mu.Lock()
		cb := s.onMessage
		s.mu.Unlock()

		if cb != nil {
			cb(&env)
		} else {
			log.Printf("[SessionPipeServer] [Session: %s] Warning: message type %s received before callback installed", s.sessionID, env.Type)
		}
	}
}

// Close disconnects and cleans up the named pipe.
func (s *SessionPipeServer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
	if s.listener != nil {
		_ = s.listener.Close()
		s.listener = nil
	}
	return nil
}

// SessionPipeClient manages the session-agent worker side of the named pipe.
type SessionPipeClient struct {
	pipeName    string
	conn        *rootipc.PipeConn
	reader      *bufio.Reader
	mu          sync.Mutex
	closed      bool
	readStarted bool
	onMessage   func(env *AgentEnvelope)
}

// ConnectSessionPipe dials the daemon's session named pipe and immediately transmits MsgAgentHello.
// It connects the named pipe and transmits hello, but DOES NOT start the background reader.
// To prevent dropping messages, the caller must call SetOnMessage() followed by StartReadLoop().
func ConnectSessionPipe(pipeName, sessionID string, timeout time.Duration) (*SessionPipeClient, error) {
	conn, err := rootipc.Dial(pipeName, timeout)
	if err != nil {
		return nil, err
	}

	// Immediately transmit MsgAgentHello before waiting for daemon bootstrap
	helloPayload := AgentHelloPayload{
		SessionID: sessionID,
		Timestamp: time.Now().Unix(),
	}
	helloData, err := EncodeEnvelope(MsgAgentHello, sessionID, helloPayload)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to encode agent hello: %w", err)
	}
	if _, err := conn.Write(helloData); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to write agent hello to pipe: %w", err)
	}

	client := &SessionPipeClient{
		pipeName: pipeName,
		conn:     conn,
		reader:   bufio.NewReader(conn),
	}
	return client, nil
}

// StartReadLoop initiates the background reader goroutine.
// It must be called ONLY AFTER SetOnMessage() has been installed to prevent dropped messages.
func (c *SessionPipeClient) StartReadLoop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed || c.conn == nil {
		return errors.New("not connected to session pipe")
	}
	if c.readStarted {
		return errors.New("read loop already started")
	}
	c.readStarted = true

	go c.readLoop()
	return nil
}

// SetOnMessage registers a callback for incoming messages from daemon.
func (c *SessionPipeClient) SetOnMessage(fn func(env *AgentEnvelope)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onMessage = fn
}

// Send writes an envelope to the daemon.
func (c *SessionPipeClient) Send(msgType AgentMessageType, sessionID string, payload interface{}) error {
	data, err := EncodeEnvelope(msgType, sessionID, payload)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed || c.conn == nil {
		return errors.New("not connected to session pipe")
	}

	_, err = c.conn.Write(data)
	return err
}

func (c *SessionPipeClient) readLoop() {
	for {
		c.mu.Lock()
		reader := c.reader
		closed := c.closed
		c.mu.Unlock()

		if closed || reader == nil {
			return
		}

		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}

		var env AgentEnvelope
		if err := json.Unmarshal(line, &env); err != nil {
			continue
		}

		c.mu.Lock()
		cb := c.onMessage
		c.mu.Unlock()

		if cb != nil {
			cb(&env)
		} else {
			log.Printf("[SessionPipeClient] Warning: message type %s discarded (callback not installed)", env.Type)
		}
	}
}

// Close terminates the client connection.
func (c *SessionPipeClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
	return nil
}
