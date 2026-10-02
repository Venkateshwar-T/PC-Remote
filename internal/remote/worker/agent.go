package worker

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
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
}

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
func (a *SessionAgent) Run() error {
	log.Printf("[Agent] Starting session-agent worker for session %s", a.cfg.SessionID)

	// 1. Connect to service daemon session named pipe and transmit handshake hello
	client, err := remoteipc.ConnectSessionPipe(a.cfg.PipeName, a.cfg.SessionID, 10*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon session pipe: %w", err)
	}
	a.pipeClient = client
	defer client.Close()

	// 2. Wire daemon incoming message handler
	initReceived := make(chan remoteipc.InitSessionPayload, 1)
	client.SetOnMessage(func(env *remoteipc.AgentEnvelope) {
		a.handleDaemonMessage(env, initReceived)
	})

	// 3. Wait for bootstrap configuration from daemon
	var initPayload remoteipc.InitSessionPayload
	select {
	case initPayload = <-initReceived:
		log.Printf("[Agent] Received session bootstrap configuration from daemon")
	case <-time.After(10 * time.Second):
		return errors.New("timeout waiting for InitSession configuration from daemon")
	case <-a.stopChan:
		return nil
	}

	// 4. Initialize Desktop Duplication capture and encoder
	if err := a.capture.Init(); err != nil {
		log.Printf("[Agent] Warning: initial capture init error: %v", err)
	}
	width, height := a.capture.Dimensions()

	fps := 30
	bitrate := 2500000 // 2.5 Mbps default
	if err := a.encoder.Init(width, height, fps, bitrate); err != nil {
		return fmt.Errorf("failed to initialize video encoder: %w", err)
	}
	defer a.encoder.Close()
	defer a.capture.Close()

	// 5. Initialize WebRTC PeerConnection
	peer, err := webrtc.NewSessionPeer(
		a.cfg.SessionID,
		a.cfg.AuthChallenge,
		a.cfg.PhonePubKey,
		initPayload.STUNServers,
		initPayload.UDPPort,
		a.injector,
	)
	if err != nil {
		return fmt.Errorf("failed to initialize WebRTC peer: %w", err)
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

	// 7. Background heartbeat to daemon
	go a.heartbeatLoop()

	// 8. Wait for termination or interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case <-sigChan:
		log.Println("[Agent] Received OS signal, stopping...")
	case <-a.stopChan:
		log.Println("[Agent] Stop requested, exiting...")
	}

	return nil
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

		if pkt.Type == session.SignalOffer && a.peer != nil {
			log.Printf("[Agent] Session %s received offer", a.cfg.SessionID)
			// Process offer and generate answer
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

		} else if (pkt.Type == session.SignalCandidate || pkt.Type == "candidate") && a.peer != nil && pkt.Candidate != nil {
			log.Printf("[Agent] Session %s received ice-candidate", a.cfg.SessionID)
			_ = a.peer.AddCandidate(*pkt.Candidate)

		} else if pkt.Type == session.SignalSessionClose || pkt.Type == "close" {
			log.Printf("[Agent] Session %s received session-close instruction from daemon", a.cfg.SessionID)
			a.Stop()
		}

	case remoteipc.MsgTerminate:
		log.Printf("[Agent] Session %s received terminate instruction from daemon", a.cfg.SessionID)
		a.Stop()
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
