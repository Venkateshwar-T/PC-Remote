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
func NewSessionPipeServer(pipeName, userSID string) (*SessionPipeServer, error) {
	listener, err := rootipc.ListenPipe(pipeName, userSID)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on session pipe %s: %w", pipeName, err)
	}

	return &SessionPipeServer{
		pipeName: pipeName,
		userSID:  userSID,
		listener: listener,
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

// Accept blocks until the session-agent connects and passes identity verification.
func (s *SessionPipeServer) Accept() error {
	conn, err := s.listener.Accept()
	if err != nil {
		return err
	}

	// Verify client identity via impersonation
	ident, err := rootipc.GetClientIdentity(conn.Handle())
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("failed to verify session-agent identity: %w", err)
	}

	// Authorized if client matches configured userSID, or is SYSTEM / Administrator
	if s.userSID != "" && ident.SID != s.userSID && !ident.IsSystem && !ident.IsAdmin {
		_ = conn.Close()
		return fmt.Errorf("unauthorized process (SID %s) connected to session pipe", ident.SID)
	}

	s.mu.Lock()
	s.conn = conn
	s.reader = bufio.NewReader(conn)
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
	conn := s.conn
	s.mu.Unlock()

	if conn == nil {
		return errors.New("agent not connected")
	}

	_, err = conn.Write(data)
	return err
}

func (s *SessionPipeServer) readLoop() {
	defer func() {
		s.mu.Lock()
		cb := s.onClientClosed
		s.mu.Unlock()
		if cb != nil {
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
	pipeName  string
	conn      *rootipc.PipeConn
	reader    *bufio.Reader
	mu        sync.Mutex
	closed    bool
	onMessage func(env *AgentEnvelope)
}

// ConnectSessionPipe dials the daemon's session named pipe.
func ConnectSessionPipe(pipeName string, timeout time.Duration) (*SessionPipeClient, error) {
	conn, err := rootipc.Dial(pipeName, timeout)
	if err != nil {
		return nil, err
	}

	client := &SessionPipeClient{
		pipeName: pipeName,
		conn:     conn,
		reader:   bufio.NewReader(conn),
	}
	go client.readLoop()
	return client, nil
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
	conn := c.conn
	c.mu.Unlock()

	if conn == nil {
		return errors.New("not connected to session pipe")
	}

	_, err = conn.Write(data)
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
