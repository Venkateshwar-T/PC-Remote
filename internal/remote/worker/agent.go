package worker

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"sync"
	"syscall"
	"time"

	"laptopcontrol/internal/remote/capture"
	"laptopcontrol/internal/remote/encoder"
	"laptopcontrol/internal/remote/input"
	remoteipc "laptopcontrol/internal/remote/ipc"
	"laptopcontrol/internal/remote/session"
	"laptopcontrol/internal/remote/webrtc"
)

var hexCodeRegex = regexp.MustCompile(`(?i)\b0x([0-9a-fA-F]{8})\b`)

// AgentConfig holds the parameters passed to the session-agent worker process.
type AgentConfig struct {
	SessionID     string
	PipeName      string
	AuthChallenge string
	PhonePubKey   string
}

// SessionAgent runs the ephemeral interactive-session Remote Desktop worker.
type SessionAgent struct {
	cfg        AgentConfig
	pipeClient *remoteipc.SessionPipeClient
	injector   *input.Injector
	capture    capture.ScreenCapture
	encoder    encoder.VideoEncoder
	peer       *webrtc.SessionPeer
	mu         sync.Mutex
	stopChan   chan struct{}
	stopOnce   sync.Once

	// pendingSignals buffers phone signaling that arrives before the WebRTC peer
	// is initialized. The phone races ahead of us: it sends its offer as soon as
	// the daemon reports "ready", which is before capture/encoder init completes.
	// Dropping those packets silently caused a black screen with no error.
	pendingSignals []session.SignalingPacket
	peerReady      bool
}

const maxPendingSignals = 64

// NewSessionAgent initializes the session-agent worker.
func NewSessionAgent(cfg AgentConfig) *SessionAgent {
	return &SessionAgent{
		cfg:      cfg,
		injector: input.NewInjector(),
		capture:  capture.NewDXGICapture(),
		encoder:  encoder.NewMFTEncoder(),
		stopChan: make(chan struct{}),
	}
}

// Run connects to the daemon session pipe and executes the worker lifecycle.
func (a *SessionAgent) Run() (err error) {
	log.Printf("[Agent] Starting session-agent worker for session %s", a.cfg.SessionID)
	defer func() {
		if err != nil {
			log.Printf("[Agent] [Session: %s] worker exited, exit reason: %v", a.cfg.SessionID, err)
		}
	}()

	// 1. Connect to service daemon session named pipe and transmit handshake hello
	client, err := remoteipc.ConnectSessionPipe(a.cfg.PipeName, a.cfg.SessionID, 10*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon session pipe: %w", err)
	}
	log.Printf("[Agent] [Session: %s] agent pipe connected", a.cfg.SessionID)
	a.pipeClient = client
	defer client.Close()

	// 2. Wire daemon incoming message handler BEFORE starting reader
	initReceived := make(chan remoteipc.InitSessionPayload, 1)
	client.SetOnMessage(func(env *remoteipc.AgentEnvelope) {
		a.handleDaemonMessage(env, initReceived)
	})

	// 3. Start reader loop NOW that callback is registered
	if err := client.StartReadLoop(); err != nil {
		return fmt.Errorf("failed to start session pipe read loop: %w", err)
	}

	// 4. Wait for bootstrap configuration from daemon
	var initPayload remoteipc.InitSessionPayload
	select {
	case initPayload = <-initReceived:
		log.Printf("[Agent] [Session: %s] worker received InitSession (STUN count: %d, UDPPort: %d)", a.cfg.SessionID, len(initPayload.STUNServers), initPayload.UDPPort)
	case <-time.After(10 * time.Second):
		return a.reportStartupError("init_session_timeout", errors.New("timeout waiting for InitSession configuration from daemon"))
	case <-a.stopChan:
		log.Printf("[Agent] [Session: %s] worker exited, exit reason: stop requested before InitSession", a.cfg.SessionID)
		return nil
	}

	// 5. Initialize Desktop Duplication capture and encoder
	if err := a.capture.Init(); err != nil {
		return a.reportStartupError("capture_init", fmt.Errorf("failed to initialize desktop capture: %w", err))
	}
	width, height := a.capture.Dimensions()
	if width <= 0 || height <= 0 {
		return a.reportStartupError("capture_init", fmt.Errorf("invalid screen dimensions from capture: %dx%d", width, height))
	}

	fps := 30
	bitrate := 2500000 // 2.5 Mbps default
	if err := a.encoder.Init(width, height, fps, bitrate); err != nil {
		return a.reportStartupError("encoder_init", fmt.Errorf("failed to initialize video encoder: %w", err))
	}
	defer a.encoder.Close()
	defer a.capture.Close()

	// 6. Initialize WebRTC PeerConnection
	peer, err := webrtc.NewSessionPeer(
		a.cfg.SessionID,
		a.cfg.AuthChallenge,
		a.cfg.PhonePubKey,
		initPayload.STUNServers,
		initPayload.UDPPort,
		a.injector,
	)
	if err != nil {
		return a.reportStartupError("webrtc_init", fmt.Errorf("failed to initialize WebRTC peer: %w", err))
	}
	a.peer = peer
	defer peer.Close()

	// 6. Wire WebRTC callbacks
	peer.SetCallbacks(
		func(candidate *session.ICECandidate) {
			// Forward candidate to phone via daemon
			pkt := session.SignalingPacket{
				Type:      session.SignalCandidate,
				SessionID: a.cfg.SessionID,
				Candidate: candidate,
				Timestamp: time.Now().Unix(),
			}
			_ = a.pipeClient.Send(remoteipc.MsgSignalAgentToPhone, a.cfg.SessionID, pkt)
		},
		func() {
			// In-band authentication succeeded!
			_ = a.pipeClient.Send(remoteipc.MsgAuthSuccess, a.cfg.SessionID, nil)
			go a.captureAndEncodeLoop(fps)
		},
		func() {
			// PLI received: request keyframe from encoder
			a.encoder.RequestKeyFrame()
		},
		func(transportType, selectedPair string) {
			stat := remoteipc.StatusUpdatePayload{
				State:         session.StateActive,
				TransportType: transportType,
				SelectedPair:  selectedPair,
			}
			_ = a.pipeClient.Send(remoteipc.MsgStatusUpdate, a.cfg.SessionID, stat)
		},
		func(reason string) {
			log.Printf("[Agent] Peer connection terminated: %s", reason)
			a.Stop()
		},
	)

	// 7. Peer is fully wired: replay any signaling that arrived during startup.
	a.markPeerReady()

	// 8. Background heartbeat to daemon
	go a.heartbeatLoop()

	// 9. Wait for termination or interrupt
	var exitReason string
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-sigChan:
		exitReason = fmt.Sprintf("OS signal (%v)", sig)
		log.Printf("[Agent] [Session: %s] Received OS signal (%v), stopping...", a.cfg.SessionID, sig)
	case <-a.stopChan:
		exitReason = "stop requested"
		log.Printf("[Agent] [Session: %s] Stop requested, exiting...", a.cfg.SessionID)
	}

	log.Printf("[Agent] [Session: %s] worker exited, exit reason: %s", a.cfg.SessionID, exitReason)
	return nil
}

func (a *SessionAgent) reportStartupError(stage string, err error) error {
	log.Printf("[Agent] [Session: %s] startup failed at stage %q: %v", a.cfg.SessionID, stage, err)
	payload := remoteipc.WorkerErrorPayload{
		Stage: stage,
		Error: err.Error(),
	}

	var sysErr syscall.Errno
	if errors.As(err, &sysErr) {
		payload.Win32Code = uint32(sysErr)
	}

	if matches := hexCodeRegex.FindStringSubmatch(err.Error()); len(matches) > 1 {
		if val, parseErr := strconv.ParseUint(matches[1], 16, 32); parseErr == nil {
			payload.HResult = int32(val)
			if payload.Win32Code == 0 {
				payload.Win32Code = uint32(val)
			}
		}
	}

	if a.pipeClient != nil {
		if sendErr := a.pipeClient.Send(remoteipc.MsgWorkerError, a.cfg.SessionID, payload); sendErr != nil {
			log.Printf("[Agent] [Session: %s] failed to send worker error over pipe: %v", a.cfg.SessionID, sendErr)
		}
	}

	return fmt.Errorf("%s failed: %w", stage, err)
}

// Stop cleanly terminates the worker.
func (a *SessionAgent) Stop() {
	a.stopOnce.Do(func() {
		close(a.stopChan)
		if a.peer != nil {
			_ = a.peer.Close()
		}
		if a.capture != nil {
			_ = a.capture.Close()
		}
		if a.encoder != nil {
			_ = a.encoder.Close()
		}
		if a.pipeClient != nil {
			_ = a.pipeClient.Close()
		}
	})
}

func (a *SessionAgent) handleDaemonMessage(env *remoteipc.AgentEnvelope, initChan chan<- remoteipc.InitSessionPayload) {
	if env == nil {
		return
	}

	switch env.Type {
	case remoteipc.MsgInitSession:
		var initPayload remoteipc.InitSessionPayload
		if err := json.Unmarshal(env.Payload, &initPayload); err == nil {
			select {
			case initChan <- initPayload:
			default:
			}
		}

	case remoteipc.MsgSignalPhoneToAgent:
		var pkt session.SignalingPacket
		if err := json.Unmarshal(env.Payload, &pkt); err != nil {
			return
		}

		// If the WebRTC peer is not initialized yet, buffer the packet instead of
		// dropping it. The phone reliably wins the race against capture/encoder init.
		a.mu.Lock()
		if !a.peerReady {
			if len(a.pendingSignals) < maxPendingSignals {
				a.pendingSignals = append(a.pendingSignals, pkt)
				log.Printf("[Agent] Session %s buffered early %s (peer not ready, %d queued)", a.cfg.SessionID, pkt.Type, len(a.pendingSignals))
			} else {
				log.Printf("[Agent] Session %s dropping %s: signaling buffer full", a.cfg.SessionID, pkt.Type)
			}
			a.mu.Unlock()
			return
		}
		a.mu.Unlock()

		a.processSignaling(pkt)

	case remoteipc.MsgTerminate:
		log.Printf("[Agent] Session %s received terminate instruction from daemon", a.cfg.SessionID)
		a.Stop()
	}
}

// processSignaling routes a single signaling packet to the WebRTC peer. Must only
// be called once the peer has been initialized.
func (a *SessionAgent) processSignaling(pkt session.SignalingPacket) {
	if a.peer == nil {
		return
	}

	switch pkt.Type {
	case session.SignalOffer:
		log.Printf("[Agent] Session %s received offer", a.cfg.SessionID)
		answerSDP, err := a.peer.HandleOffer(pkt.SDP)
		if err != nil {
			log.Printf("[Agent] Error handling offer: %v", err)
			return
		}
		log.Printf("[Agent] Session %s generated answer", a.cfg.SessionID)

		ansPkt := session.SignalingPacket{
			Type:      session.SignalAnswer,
			SessionID: a.cfg.SessionID,
			SDP:       answerSDP,
			Timestamp: time.Now().Unix(),
		}
		_ = a.pipeClient.Send(remoteipc.MsgSignalAgentToPhone, a.cfg.SessionID, ansPkt)

	case session.SignalCandidate:
		if pkt.Candidate != nil {
			log.Printf("[Agent] Session %s received ice-candidate", a.cfg.SessionID)
			_ = a.peer.AddCandidate(*pkt.Candidate)
		}

	case session.SignalSessionClose:
		log.Printf("[Agent] Session %s received session-close instruction from daemon", a.cfg.SessionID)
		a.Stop()
	}
}

// markPeerReady flips the peer-ready flag and replays any signaling packets that
// arrived while we were still initializing capture/encoder/WebRTC.
func (a *SessionAgent) markPeerReady() {
	a.mu.Lock()
	a.peerReady = true
	pending := a.pendingSignals
	a.pendingSignals = nil
	a.mu.Unlock()

	if len(pending) == 0 {
		return
	}
	log.Printf("[Agent] Session %s replaying %d buffered signaling packet(s)", a.cfg.SessionID, len(pending))
	for _, pkt := range pending {
		a.processSignaling(pkt)
	}
}

func (a *SessionAgent) captureAndEncodeLoop(fps int) {
	log.Printf("[Agent] Session %s starting capture and H.264 encode loop at %d FPS", a.cfg.SessionID, fps)

	frameDuration := time.Second / time.Duration(fps)
	ticker := time.NewTicker(frameDuration)
	defer ticker.Stop()

	startTime := time.Now()

	for {
		select {
		case <-a.stopChan:
			return
		case <-ticker.C:
			if !a.peer.IsAuthenticated() {
				continue
			}

			frame, err := a.capture.AcquireFrame(20)
			if err != nil {
				if errors.Is(err, capture.ErrTimeout) {
					continue // Nothing changed on screen, skip encode!
				}
				if errors.Is(err, capture.ErrAccessLost) {
					time.Sleep(100 * time.Millisecond)
					_ = a.capture.Init()
					continue
				}
				continue
			}

			pts := time.Since(startTime)
			nalus, _, encErr := a.encoder.Encode(frame.Data, pts)
			a.capture.ReleaseFrame()

			if encErr != nil {
				continue
			}

			for _, nalu := range nalus {
				if len(nalu) > 0 {
					_ = a.peer.WriteVideoSample(nalu, frameDuration)
				}
			}
		}
	}
}

func (a *SessionAgent) heartbeatLoop() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-a.stopChan:
			return
		case <-ticker.C:
			if a.pipeClient != nil {
				_ = a.pipeClient.Send(remoteipc.MsgHeartbeat, a.cfg.SessionID, nil)
			}
		}
	}
}
