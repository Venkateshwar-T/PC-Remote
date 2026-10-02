package remote

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"syscall"
	"time"

	"laptopcontrol/internal/config"
	remoteipc "laptopcontrol/internal/remote/ipc"
	"laptopcontrol/internal/remote/session"
	"laptopcontrol/internal/remote/spawner"
)

// outboundSignal publishes a signaling packet back to the paired phone.
type outboundSignalFn func(phonePubKey string, packet session.SignalingPacket)

// SessionManager coordinates Remote Desktop session orchestration, worker lifecycle, and signaling.
type SessionManager struct {
	cfg            *config.Config
	sm             *session.StateMachine
	spawner        *spawner.Spawner
	mu             sync.Mutex
	activeProcess  *spawner.ProcessHandle
	activePipe      *remoteipc.SessionPipeServer
	outboundSignal  outboundSignalFn
	lastWorkerError string
	closed          bool
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
func (m *SessionManager) SetOutboundSignalHandler(fn outboundSignalFn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.outboundSignal = fn
}

// HandleSessionRequest handles an incoming "remote_request" action from a paired phone.
func (m *SessionManager) HandleSessionRequest(reqID, phonePubKey string) (*session.SessionResponse, error) {
	reqStart := time.Now()
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, errors.New("remote session manager is shutting down")
	}
	m.mu.Unlock()

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

	m.mu.Lock()
	m.lastWorkerError = ""
	m.mu.Unlock()

	// 3. Start dedicated session named pipe server
	userSID := m.cfg.AuthorizedUserSID
	pipeServer, err := remoteipc.NewSessionPipeServer(pipeName, sessionID, userSID)
	if err != nil {
		_ = m.sm.Transition(sessionID, session.StateFailed)
		return nil, fmt.Errorf("failed to initialize session named pipe: %w", err)
	}

	m.mu.Lock()
	m.activePipe = pipeServer
	m.mu.Unlock()

	// 4. Spawn session-agent into active interactive console session
	_ = m.sm.Transition(sessionID, session.StateLaunching)
	spawnStart := time.Now()
	procHandle, err := m.spawner.SpawnSessionAgent(sessionID, pipeName, authChallenge, phonePubKey)
	if err != nil {
		m.mu.Lock()
		if m.activePipe == pipeServer {
			_ = pipeServer.Close()
			m.activePipe = nil
		}
		m.mu.Unlock()
		_ = m.sm.Transition(sessionID, session.StateFailed)
		log.Printf("[RemoteReq Timing] [Req: %s] [Session: %s] Failed to spawn session-agent after %v: %v", reqID, sessionID, time.Since(spawnStart), err)
		return &session.SessionResponse{
			Status:    "error",
			Message:   fmt.Sprintf("Failed to launch session worker in interactive desktop: %v", err),
			Timestamp: time.Now().Unix(),
		}, nil
	}
	log.Printf("[RemoteMgr] [Req: %s] [Session: %s] agent process spawned (PID: %d, took %v, elapsed: %v)", reqID, sessionID, procHandle.PID(), time.Since(spawnStart), time.Since(reqStart))

	m.mu.Lock()
	m.activeProcess = procHandle
	m.mu.Unlock()

	// 5. Wire pipe callbacks and accept worker connection asynchronously
	pipeServer.SetOnMessage(func(env *remoteipc.AgentEnvelope) {
		m.handleAgentMessage(env)
	})
	pipeServer.SetOnClientClosed(func() {
		m.mu.Lock()
		activeProc := m.activeProcess
		m.mu.Unlock()

		exitReason := "session pipe closed by agent"
		if activeProc != nil {
			if code, running := activeProc.GetExitCode(); !running {
				exitReason = fmt.Sprintf("session worker process exited with code %d (0x%X)", code, code)
			} else {
				exitReason = fmt.Sprintf("session worker process (PID %d) still active but closed pipe", activeProc.PID())
			}
		}
		log.Printf("[RemoteMgr] [Session: %s] worker exited, exit reason: %s", sessionID, exitReason)
		_ = m.sm.Transition(sessionID, session.StateStopped)
	})

	connectedChan := make(chan error, 1)
	acceptStart := time.Now()
	log.Printf("[RemoteMgr] [Req: %s] [Session: %s] pipe Accept started (pipe: %s, elapsed: %v)", reqID, sessionID, pipeName, time.Since(reqStart))

	go func() {
		connectedChan <- pipeServer.Accept()
	}()

	select {
	case err := <-connectedChan:
		if err != nil {
			m.mu.Lock()
			activeProc := m.activeProcess
			if m.activePipe == pipeServer {
				_ = pipeServer.Close()
				m.activePipe = nil
			}
			m.mu.Unlock()
			_ = m.sm.Transition(sessionID, session.StateFailed)

			exitReason := err.Error()
			if activeProc != nil {
				if code, running := activeProc.GetExitCode(); !running {
					exitReason = fmt.Sprintf("worker process (PID %d) exited with code %d (0x%X), accept error: %v", activeProc.PID(), code, code, err)
				}
			}
			log.Printf("[RemoteMgr] [Req: %s] [Session: %s] worker exited, exit reason: %s (took %v)", reqID, sessionID, exitReason, time.Since(acceptStart))
			return &session.SessionResponse{
				Status:    "error",
				Message:   fmt.Sprintf("Failed to establish pipe with session worker: %s", exitReason),
				Timestamp: time.Now().Unix(),
			}, nil
		}
		log.Printf("[RemoteMgr] [Req: %s] [Session: %s] agent pipe connected (wait took %v, elapsed: %v)", reqID, sessionID, time.Since(acceptStart), time.Since(reqStart))
	case <-time.After(5 * time.Second):
		m.mu.Lock()
		activeProc := m.activeProcess
		if m.activePipe == pipeServer {
			_ = pipeServer.Close()
			m.activePipe = nil
		}
		m.mu.Unlock()
		_ = m.sm.Transition(sessionID, session.StateFailed)

		exitReason := "worker did not connect within 5s"
		if activeProc != nil {
			if code, running := activeProc.GetExitCode(); !running {
				exitReason = fmt.Sprintf("worker process (PID %d) exited prematurely with code %d (0x%X)", activeProc.PID(), code, code)
			}
		}
		log.Printf("[RemoteMgr] [Req: %s] [Session: %s] worker exited, exit reason: %s (elapsed: %v)", reqID, sessionID, exitReason, time.Since(reqStart))
		return &session.SessionResponse{
			Status:    "error",
			Message:   fmt.Sprintf("Session worker failed to connect: %s", exitReason),
			Timestamp: time.Now().Unix(),
		}, nil
	}

	// Agent connected! Send bootstrap configuration
	initStart := time.Now()
	initPayload := remoteipc.InitSessionPayload{
		AuthChallenge: authChallenge,
		PhonePubKey:   phonePubKey,
		STUNServers: []string{
			"stun:stun.l.google.com:19302",
			"stun:stun1.l.google.com:19302",
		},
		UDPPort: 8765,
	}

	log.Printf("[RemoteMgr] [Req: %s] [Session: %s] InitSession send attempted", reqID, sessionID)
	if err := pipeServer.Send(remoteipc.MsgInitSession, sessionID, initPayload); err != nil {
		m.mu.Lock()
		activeProc := m.activeProcess
		if m.activePipe == pipeServer {
			_ = pipeServer.Close()
			m.activePipe = nil
		}
		m.mu.Unlock()
		_ = m.sm.Transition(sessionID, session.StateFailed)

		exitReason := err.Error()
		if activeProc != nil {
			if code, running := activeProc.GetExitCode(); !running {
				exitReason = fmt.Sprintf("worker process (PID %d) exited with code %d (0x%X), write error: %v", activeProc.PID(), code, code, err)
			}
		}

		var sysErr syscall.Errno
		var winErrStr string
		if errors.As(err, &sysErr) {
			winErrStr = fmt.Sprintf(" (Win32 error: %d / 0x%X)", uint32(sysErr), uint32(sysErr))
		}

		log.Printf("[RemoteMgr] [Req: %s] [Session: %s] InitSession send failed after %v: %v%s", reqID, sessionID, time.Since(initStart), err, winErrStr)
		log.Printf("[RemoteMgr] [Req: %s] [Session: %s] worker exited, exit reason: %s", reqID, sessionID, exitReason)
		return &session.SessionResponse{
			Status:    "error",
			Message:   fmt.Sprintf("Failed to initialize session worker: %v%s", err, winErrStr),
			Timestamp: time.Now().Unix(),
		}, nil
	}
	log.Printf("[RemoteMgr] [Req: %s] [Session: %s] InitSession send succeeded (took %v, elapsed: %v)", reqID, sessionID, time.Since(initStart), time.Since(reqStart))

	// 6. Start server background reader loop now that InitSession bootstrap has been sent
	if err := pipeServer.StartReadLoop(); err != nil {
		log.Printf("[RemoteMgr] [Req: %s] [Session: %s] Failed to start pipe server read loop: %v", reqID, sessionID, err)
	}

	_ = m.sm.Transition(sessionID, session.StateSignaling)

	log.Printf("[RemoteMgr] [Req: %s] [Session: %s] HandleSessionRequest completed (status: ready, total elapsed: %v)", reqID, sessionID, time.Since(reqStart))

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

	log.Printf("[RemoteMgr] Session %s received %s", packet.SessionID, packet.Type)

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

		log.Printf("[RemoteMgr] Session %s forwarded %s to agent", packet.SessionID, packet.Type)
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

	case remoteipc.MsgWorkerError:
		var errPayload remoteipc.WorkerErrorPayload
		if err := json.Unmarshal(env.Payload, &errPayload); err != nil {
			log.Printf("[RemoteMgr] [Session: %s] Malformed worker error payload: %v", env.SessionID, err)
			_ = m.sm.Transition(env.SessionID, session.StateFailed)
			return
		}

		var codeInfo string
		if errPayload.Win32Code != 0 {
			codeInfo += fmt.Sprintf(" (Win32: %d / 0x%08X)", errPayload.Win32Code, errPayload.Win32Code)
		}
		if errPayload.HResult != 0 {
			codeInfo += fmt.Sprintf(" (HRESULT: 0x%08X)", uint32(errPayload.HResult))
		}

		errDesc := fmt.Sprintf("worker error at stage %q: %s%s", errPayload.Stage, errPayload.Error, codeInfo)
		log.Printf("[RemoteMgr] [Session: %s] Worker error received: %s", env.SessionID, errDesc)

		m.mu.Lock()
		m.lastWorkerError = errDesc
		outCb := m.outboundSignal
		m.mu.Unlock()

		// Surface the failure to the phone. Without this the client sees a silent
		// black screen when capture/encoder/WebRTC initialization fails on the host.
		cur := m.sm.GetCurrentSession()
		if cur != nil && cur.SessionID == env.SessionID && outCb != nil {
			outCb(cur.PhonePubKey, session.SignalingPacket{
				Type:      session.SignalError,
				SessionID: env.SessionID,
				Error:     errDesc,
				Message:   "Remote desktop failed to start on the host",
				Timestamp: time.Now().Unix(),
			})
		}

		_ = m.sm.Transition(env.SessionID, session.StateFailed)

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
		code, running := m.activeProcess.GetExitCode()
		if running {
			log.Printf("[RemoteMgr] Terminating session worker process (PID %d) for session %s", m.activeProcess.PID(), sessionID)
			m.activeProcess.Terminate()
		} else {
			exitDetail := fmt.Sprintf("session worker process (PID %d) exited with code %d (0x%X)", m.activeProcess.PID(), code, code)
			if m.lastWorkerError != "" {
				exitDetail = fmt.Sprintf("%s; underlying failure: %s", exitDetail, m.lastWorkerError)
			}
			log.Printf("[RemoteMgr] [Session: %s] worker exited, exit reason: %s", sessionID, exitDetail)
		}
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
