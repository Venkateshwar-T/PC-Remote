package ipc

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

// Handlers contains the callback functions for permitted IPC operations
type Handlers struct {
	GetStatus          func() (*StatusResult, error)
	GetPairingInfo     func() (*PairingInfoResult, error)
	RotatePairingToken func() (*PairingInfoResult, error)
	GetPinStatus       func() (*PinStatusResult, error)
	SetPin             func(oldPin, newPin string) (*SetPinResult, error)
	MigrateLegacyKey   func(privateKey string) (*MigrateKeyResult, error)
	StopService        func() (*StopServiceResult, error)
}

// Server manages the local IPC Named Pipe server
type Server struct {
	pipeName string
	handlers Handlers
	listener *PipeListener
	wg       sync.WaitGroup
	mu       sync.Mutex
	closed   bool
	sem      chan struct{}
}

// NewServer initializes an IPC server with the specified pipe name and operation handlers
func NewServer(pipeName string, handlers Handlers) *Server {
	if pipeName == "" {
		pipeName = PipeName
	}
	return &Server{
		pipeName: pipeName,
		handlers: handlers,
		sem:      make(chan struct{}, 8), // Max 8 concurrent local IPC client connections
	}
}

// Start begins listening and serving local IPC clients
func (s *Server) Start() error {
	l, err := ListenPipe(s.pipeName)
	if err != nil {
		return fmt.Errorf("failed to listen on IPC pipe %s: %w", s.pipeName, err)
	}
	s.listener = l

	s.wg.Add(1)
	go s.serve()
	return nil
}

// Stop terminates the IPC listener and waits for active connections to finish
func (s *Server) Stop() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()

	var err error
	if s.listener != nil {
		err = s.listener.Close()
	}
	s.wg.Wait()
	return err
}

func (s *Server) serve() {
	defer s.wg.Done()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed || errors.Is(err, net.ErrClosed) {
				return
			}
			log.Printf("[IPC] Accept warning: %v", err)
			time.Sleep(50 * time.Millisecond)
			continue
		}

		select {
		case s.sem <- struct{}{}:
			s.wg.Add(1)
			go func(c *PipeConn) {
				defer func() {
					<-s.sem
					s.wg.Done()
				}()
				s.handleConn(c)
			}(conn)
		default:
			// Saturated connection limit: reject immediately to protect daemon resources
			_ = conn.Close()
		}
	}
}

func (s *Server) handleConn(conn *PipeConn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	for {
		line, isPrefix, err := reader.ReadLine()
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return
			}
			return
		}

		if isPrefix {
			// Message exceeded bufio buffer, check if it exceeds MaxMessageSize
			var fullBuf bytes.Buffer
			fullBuf.Write(line)
			for isPrefix && err == nil {
				line, isPrefix, err = reader.ReadLine()
				fullBuf.Write(line)
				if fullBuf.Len() > MaxMessageSize {
					s.sendError(conn, "", fmt.Sprintf("Request payload exceeds maximum allowed size (%d bytes)", MaxMessageSize))
					return
				}
			}
			line = fullBuf.Bytes()
		} else if len(line) > MaxMessageSize {
			s.sendError(conn, "", fmt.Sprintf("Request payload exceeds maximum allowed size (%d bytes)", MaxMessageSize))
			return
		}

		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			s.sendError(conn, "", "Malformed JSON-RPC request")
			continue
		}

		resp := s.dispatch(&req)
		respBytes, err := json.Marshal(resp)
		if err != nil {
			s.sendError(conn, req.ID, "Internal serialization failure")
			continue
		}

		respBytes = append(respBytes, '\n')
		if _, err := conn.Write(respBytes); err != nil {
			return
		}
	}
}

func (s *Server) sendError(conn *PipeConn, id, errMsg string) {
	resp := Response{
		ID:    id,
		Error: errMsg,
	}
	b, _ := json.Marshal(resp)
	b = append(b, '\n')
	_, _ = conn.Write(b)
}

func (s *Server) dispatch(req *Request) Response {
	switch req.Method {
	case MethodGetStatus:
		if s.handlers.GetStatus == nil {
			return Response{ID: req.ID, Error: "Method not implemented"}
		}
		res, err := s.handlers.GetStatus()
		return s.formatResponse(req.ID, res, err)

	case MethodGetPairingInfo:
		if s.handlers.GetPairingInfo == nil {
			return Response{ID: req.ID, Error: "Method not implemented"}
		}
		res, err := s.handlers.GetPairingInfo()
		return s.formatResponse(req.ID, res, err)

	case MethodRotatePairingToken:
		if s.handlers.RotatePairingToken == nil {
			return Response{ID: req.ID, Error: "Method not implemented"}
		}
		res, err := s.handlers.RotatePairingToken()
		return s.formatResponse(req.ID, res, err)

	case MethodGetPinStatus:
		if s.handlers.GetPinStatus == nil {
			return Response{ID: req.ID, Error: "Method not implemented"}
		}
		res, err := s.handlers.GetPinStatus()
		return s.formatResponse(req.ID, res, err)

	case MethodSetPin:
		if s.handlers.SetPin == nil {
			return Response{ID: req.ID, Error: "Method not implemented"}
		}
		var p SetPinParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return Response{ID: req.ID, Error: "Invalid SetPin parameters"}
		}
		res, err := s.handlers.SetPin(p.OldPin, p.NewPin)
		return s.formatResponse(req.ID, res, err)

	case MethodMigrateLegacyKey:
		if s.handlers.MigrateLegacyKey == nil {
			return Response{ID: req.ID, Error: "Method not implemented"}
		}
		var p MigrateKeyParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return Response{ID: req.ID, Error: "Invalid MigrateLegacyKey parameters"}
		}
		res, err := s.handlers.MigrateLegacyKey(p.PrivateKey)
		return s.formatResponse(req.ID, res, err)

	case MethodStopService:
		if s.handlers.StopService == nil {
			return Response{ID: req.ID, Error: "Method not implemented"}
		}
		res, err := s.handlers.StopService()
		return s.formatResponse(req.ID, res, err)

	default:
		// Strict allowlist: reject any unrecognized or unpermitted methods
		return Response{
			ID:    req.ID,
			Error: fmt.Sprintf("Forbidden or unknown method: %s", req.Method),
		}
	}
}

func (s *Server) formatResponse(id string, result interface{}, err error) Response {
	if err != nil {
		return Response{
			ID:    id,
			Error: err.Error(),
		}
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return Response{
			ID:    id,
			Error: "Failed to serialize response",
		}
	}
	return Response{
		ID:     id,
		Result: raw,
	}
}
