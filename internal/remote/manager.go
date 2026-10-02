package remote

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"laptopcontrol/internal/config"
	remoteipc "laptopcontrol/internal/remote/ipc"
	"laptopcontrol/internal/remote/session"
	"laptopcontrol/internal/remote/spawner"
)

// SessionManager coordinates Remote Desktop session orchestration, worker lifecycle, and signaling.
type SessionManager struct {
	cfg            *config.Config
	sm             *session.StateMachine
	spawner        *spawner.Spawner
	mu             sync.Mutex
	activeProcess  *spawner.ProcessHandle
	activePipe     *remoteipc.SessionPipeServer
	outboundSignal func(phonePubKey string, packet session.SignalingPacket)
	closed         bool
}

// NewSessionManager creates a new Remote Desktop manager bound to the service daemon.
func NewSessionManager(cfg *config.Config) *SessionManager {
	mgr := &SessionManager{
		cfg:     cfg,
		sm:      session.NewStateMachine(12 * time.Second),
		spawner: spawner.NewSpawner(),
	}

	mgr.sm.SetOnTerminate(func(sessionID string) {
		mgr.cleanupActiveSession(sessionID)
	})

	return mgr
}

// SetOutboundSignalHandler sets the callback to send signaling packets back to the phone.
func (m *SessionManager) SetOutboundSignalHandler(fn func(phonePubKey string, packet session.SignalingPacket)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.outboundSignal = fn
}

// HandleSessionRequest handles an incoming "remote_request" action from a paired phone.
func (m *SessionManager) HandleSessionRequest(phonePubKey string) (*session.SessionResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, errors.New("remote session manager is shutting down")
	}

	// 1. Verify that phone is in AuthorizedDevices
	if !m.cfg.IsDeviceAuthorized(phonePubKey) {
		return &session.SessionResponse{
			Status:    "unauthorized",
			Message:   "device is not authorized for remote desktop",
			Timestamp: time.Now().Unix(),
		}, nil
	}

	// 2. Attempt to create session in state machine (enforces single active session limit)
	info, err := m.sm.CreateSession(phonePubKey)
	if err != nil {
		if errors.Is(err, session.ErrSessionBusy) {
			return &session.SessionResponse{
				Status:    "busy",
				Message:   "A remote desktop session is already active",
				Timestamp: time.Now().Unix(),
			}, nil
		}
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	sessionID := info.SessionID
	authChallenge := info.AuthChallenge
	pipeName := fmt.Sprintf(`\\.\pipe\PCRemote_Session_%s`, sessionID)

	// 3. Start dedicated session named pipe server
	userSID := m.cfg.AuthorizedUserSID
	pipeServer, err := remoteipc.NewSessionPipeServer(pipeName, userSID)
	if err != nil {
		_ = m.sm.Transition(sessionID, session.StateFailed)
		return nil, fmt.Errorf("failed to initialize session named pipe: %w", err)
	}
	m.activePipe = pipeServer

	// 4. Spawn session-agent into active interactive console session
	_ = m.sm.Transition(sessionID, session.StateLaunching)
	procHandle, err := m.spawner.SpawnSessionAgent(sessionID, pipeName, authChallenge, phonePubKey)
	if err != nil {
		_ = pipeServer.Close()
		m.activePipe = nil
		_ = m.sm.Transition(sessionID, session.StateFailed)
		log.Printf("[RemoteMgr] Failed to spawn session-agent: %v", err)
		return &session.SessionResponse{
			Status:    "error",
			Message:   fmt.Sprintf("Failed to launch session worker in interactive desktop: %v", err),
			Timestamp: time.Now().Unix(),
		}, nil
	}
	m.activeProcess = procHandle

	// 5. Wire pipe callbacks and accept worker connection asynchronously
	pipeServer.SetOnMessage(func(env *remoteipc.AgentEnvelope) {
		m.handleAgentMessage(env)
	})
	pipeServer.SetOnClientClosed(func() {
		log.Printf("[RemoteMgr] Session pipe closed by agent for session %s", sessionID)
		_ = m.sm.Transition(sessionID, session.StateStopped)
	})

	connectedChan := make(chan error, 1)
	go func() {
		connectedChan <- pipeServer.Accept()
	}()

	select {
	case err := <-connectedChan:
		if err != nil {
			_ = pipeServer.Close()
			m.activePipe = nil
			_ = m.sm.Transition(sessionID, session.StateFailed)
			log.Printf("[RemoteMgr] Session pipe accept failed for session %s: %v", sessionID, err)
			return &session.SessionResponse{
				Status:    "error",
				Message:   fmt.Sprintf("Failed to establish pipe with session worker: %v", err),
				Timestamp: time.Now().Unix(),
			}, nil
		}
	case <-time.After(5 * time.Second):
		_ = pipeServer.Close()
		m.activePipe = nil
		_ = m.sm.Transition(sessionID, session.StateFailed)
		log.Printf("[RemoteMgr] Session agent launch timed out for session %s", sessionID)
		return &session.SessionResponse{
			Status:    "error",
			Message:   "Session worker took too long to connect",
			Timestamp: time.Now().Unix(),
		}, nil
	}

	// Agent connected! Send bootstrap configuration
	initPayload := remoteipc.InitSessionPayload{
		AuthChallenge: authChallenge,
		PhonePubKey:   phonePubKey,
		STUNServers: []string{
			"stun:stun.l.google.com:19302",
			"stun:stun1.l.google.com:19302",
		},
		UDPPort: 8765,
	}
	if err := pipeServer.Send(remoteipc.MsgInitSession, sessionID, initPayload); err != nil {
		log.Printf("[RemoteMgr] Failed to send InitSession to agent: %v", err)
		_ = pipeServer.Close()
		m.activePipe = nil
		_ = m.sm.Transition(sessionID, session.StateFailed)
		return &session.SessionResponse{
			Status:    "error",
			Message:   "Failed to initialize session worker",
			Timestamp: time.Now().Unix(),
		}, nil
	}

	_ = m.sm.Transition(sessionID, session.StateSignaling)

	return &session.SessionResponse{
		Status:        "ready",
		SessionID:     sessionID,
		AuthChallenge: authChallenge,
		Message:       "Remote session initialized. Ready for WebRTC offer.",
		Timestamp:     time.Now().Unix(),
	}, nil
}

// HandleSignaling processes incoming WebRTC signaling packets from the paired phone.
func (m *SessionManager) HandleSignaling(phonePubKey string, packet session.SignalingPacket) (*session.SignalingPacket, error) {
	if err := packet.Validate(); err != nil {
		return nil, fmt.Errorf("invalid signaling packet: %w", err)
	}

	cur := m.sm.GetCurrentSession()
	if cur == nil || cur.SessionID != packet.SessionID {
		return nil, errors.New("no active session matches provided session ID")
	}
	if cur.PhonePubKey != phonePubKey {
		return nil, errors.New("unauthorized phone public key for this session")
	}

	// Update heartbeat
	_ = m.sm.RecordHeartbeat(packet.SessionID)

	switch packet.Type {
	case session.SignalSessionClose:
		log.Printf("[RemoteMgr] Received session-close from phone for session %s", packet.SessionID)
		_ = m.sm.Terminate(packet.SessionID, "user exit")
		return &session.SignalingPacket{
			Type:      session.SignalSessionClose,
			SessionID: packet.SessionID,
			Timestamp: time.Now().Unix(),
		}, nil

	case session.SignalHeartbeat:
		return &session.SignalingPacket{
			Type:      session.SignalHeartbeat,
			SessionID: packet.SessionID,
			Timestamp: time.Now().Unix(),
		}, nil

	default:
		// Forward offer / ice-candidate to session-agent via pipe
		m.mu.Lock()
		pipe := m.activePipe
		m.mu.Unlock()

		if pipe == nil {
			return nil, errors.New("session-agent not connected")
		}

		err := pipe.Send(remoteipc.MsgSignalPhoneToAgent, packet.SessionID, packet)
		if err != nil {
			return nil, fmt.Errorf("failed to forward signaling to agent: %w", err)
		}
		return nil, nil // Asynchronous reply will arrive from agent
	}
}

func (m *SessionManager) handleAgentMessage(env *remoteipc.AgentEnvelope) {
	if env == nil {
		return
	}

	_ = m.sm.RecordHeartbeat(env.SessionID)

	switch env.Type {
	case remoteipc.MsgSignalAgentToPhone:
		var pkt session.SignalingPacket
		if err := json.Unmarshal(env.Payload, &pkt); err != nil {
			log.Printf("[RemoteMgr] Malformed signaling packet from agent: %v", err)
			return
		}

		cur := m.sm.GetCurrentSession()
		if cur != nil && cur.SessionID == env.SessionID {
			m.mu.Lock()
			outCb := m.outboundSignal
			m.mu.Unlock()

			if outCb != nil {
				outCb(cur.PhonePubKey, pkt)
			}
		}

	case remoteipc.MsgAuthSuccess:
		log.Printf("[RemoteMgr] Agent confirmed in-band authentication for session %s", env.SessionID)
		_ = m.sm.MarkAuthVerified(env.SessionID)
		_ = m.sm.Transition(env.SessionID, session.StateActive)

	case remoteipc.MsgHeartbeat:
		_ = m.sm.RecordHeartbeat(env.SessionID)

	case remoteipc.MsgStatusUpdate:
		var stat remoteipc.StatusUpdatePayload
		if err := json.Unmarshal(env.Payload, &stat); err == nil {
			if stat.TransportType != "" {
				log.Printf("[RemoteMgr] Session %s transport path: %s", env.SessionID, stat.TransportType)
			}
		}

	case remoteipc.MsgTerminate:
		log.Printf("[RemoteMgr] Agent requested termination for session %s", env.SessionID)
		_ = m.sm.Terminate(env.SessionID, "agent terminated")
	}
}

func (m *SessionManager) cleanupActiveSession(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	log.Printf("[RemoteMgr] Cleaning up active session resources for session %s", sessionID)

	if m.activePipe != nil {
		_ = m.activePipe.Close()
		m.activePipe = nil
	}

	if m.activeProcess != nil {
		m.activeProcess.Terminate()
		m.activeProcess = nil
	}
}

// Close terminates any running session and frees resources.
func (m *SessionManager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	m.mu.Unlock()

	m.sm.Close()
}
