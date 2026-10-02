package webrtc

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"laptopcontrol/internal/remote/input"
	"laptopcontrol/internal/remote/session"
)

// SessionPeer manages WebRTC PeerConnection, H.264 video RTP streaming, and DataChannels.
type SessionPeer struct {
	sessionID        string
	authChallenge    string
	phonePubKey      string
	peerConn         *webrtc.PeerConnection
	videoTrack       *webrtc.TrackLocalStaticSample
	controlDC        *webrtc.DataChannel
	inputDC          *webrtc.DataChannel
	injector         *input.Injector
	mu               sync.Mutex
	authVerified     bool
	closed           bool
	onCandidate      func(candidate *session.ICECandidate)
	onAuthSuccess    func()
	onPLI            func()
	onTransportState func(transportType, selectedPair string)
	onTerminated     func(reason string)
}

// NewSessionPeer creates a configured Pion WebRTC peer connection.
func NewSessionPeer(sessionID, authChallenge, phonePubKey string, stunServers []string, udpPort int, injector *input.Injector) (*SessionPeer, error) {
	mediaEngine := &webrtc.MediaEngine{}
	if err := mediaEngine.RegisterDefaultCodecs(); err != nil {
		return nil, fmt.Errorf("failed to register default codecs: %w", err)
	}

	settingEngine := webrtc.SettingEngine{}
	if udpPort > 0 {
		// Bounded UDP port range for firewall friendliness
		_ = settingEngine.SetEphemeralUDPPortRange(uint16(udpPort), uint16(udpPort))
	}

	api := webrtc.NewAPI(
		webrtc.WithMediaEngine(mediaEngine),
		webrtc.WithSettingEngine(settingEngine),
	)

	var iceServers []webrtc.ICEServer
	for _, s := range stunServers {
		iceServers = append(iceServers, webrtc.ICEServer{URLs: []string{s}})
	}

	config := webrtc.Configuration{
		ICEServers: iceServers,
	}

	pc, err := api.NewPeerConnection(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create PeerConnection: %w", err)
	}

	// Create H.264 Video Track
	videoTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264},
		"video",
		"pcremote-screen",
	)
	if err != nil {
		_ = pc.Close()
		return nil, fmt.Errorf("failed to create video track: %w", err)
	}

	sender, err := pc.AddTrack(videoTrack)
	if err != nil {
		_ = pc.Close()
		return nil, fmt.Errorf("failed to add video track to peer: %w", err)
	}

	sp := &SessionPeer{
		sessionID:     sessionID,
		authChallenge: authChallenge,
		phonePubKey:   phonePubKey,
		peerConn:      pc,
		videoTrack:    videoTrack,
		injector:      injector,
	}

	// Read RTCP packets to capture PLI (Picture Loss Indication) for keyframe requests
	go func() {
		rtcpBuf := make([]byte, 1500)
		for {
			if _, _, err := sender.Read(rtcpBuf); err != nil {
				return
			}
			sp.mu.Lock()
			pliCb := sp.onPLI
			sp.mu.Unlock()
			if pliCb != nil {
				pliCb()
			}
		}
	}()

	// Wire ICE Candidates
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		init := c.ToJSON()
		sp.mu.Lock()
		candCb := sp.onCandidate
		sp.mu.Unlock()
		if candCb != nil {
			candCb(&session.ICECandidate{
				Candidate:     init.Candidate,
				SDPMid:        init.SDPMid,
				SDPMLineIndex: init.SDPMLineIndex,
			})
		}
	})

	// Monitor Connection State
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		log.Printf("[WebRTC] Session %s connection state: %s", sp.sessionID, s)
		if s == webrtc.PeerConnectionStateConnected {
			sp.detectTransportType()
		} else if s == webrtc.PeerConnectionStateFailed || s == webrtc.PeerConnectionStateClosed {
			sp.mu.Lock()
			termCb := sp.onTerminated
			sp.mu.Unlock()
			if termCb != nil {
				termCb(s.String())
			}
		}
	})

	// Handle Inbound DataChannels created by browser
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		sp.attachDataChannel(dc)
	})

	return sp, nil
}

// SetCallbacks configures event listeners.
func (sp *SessionPeer) SetCallbacks(
	onCandidate func(c *session.ICECandidate),
	onAuthSuccess func(),
	onPLI func(),
	onTransportState func(transportType, selectedPair string),
	onTerminated func(reason string),
) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	sp.onCandidate = onCandidate
	sp.onAuthSuccess = onAuthSuccess
	sp.onPLI = onPLI
	sp.onTransportState = onTransportState
	sp.onTerminated = onTerminated
}

// HandleOffer processes a browser WebRTC offer and returns a generated answer.
func (sp *SessionPeer) HandleOffer(offerSDP string) (string, error) {
	desc := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  offerSDP,
	}

	if err := sp.peerConn.SetRemoteDescription(desc); err != nil {
		return "", fmt.Errorf("SetRemoteDescription failed: %w", err)
	}

	answer, err := sp.peerConn.CreateAnswer(nil)
	if err != nil {
		return "", fmt.Errorf("CreateAnswer failed: %w", err)
	}

	if err := sp.peerConn.SetLocalDescription(answer); err != nil {
		return "", fmt.Errorf("SetLocalDescription failed: %w", err)
	}

	return answer.SDP, nil
}

// AddCandidate registers an ICE candidate from the phone.
func (sp *SessionPeer) AddCandidate(c session.ICECandidate) error {
	init := webrtc.ICECandidateInit{
		Candidate:     c.Candidate,
		SDPMid:        c.SDPMid,
		SDPMLineIndex: c.SDPMLineIndex,
	}
	return sp.peerConn.AddICECandidate(init)
}

// WriteVideoSample pushes an H.264 frame to the video RTP stream only if authenticated.
func (sp *SessionPeer) WriteVideoSample(data []byte, duration time.Duration) error {
	sp.mu.Lock()
	authed := sp.authVerified
	closed := sp.closed
	track := sp.videoTrack
	sp.mu.Unlock()

	if closed || track == nil {
		return errors.New("peer connection closed")
	}

	// Strictly enforce: No actual desktop screen frames transmitted before authentication
	if !authed {
		return nil
	}

	return track.WriteSample(media.Sample{
		Data:     data,
		Duration: duration,
	})
}

// IsAuthenticated checks whether the browser passed in-band cryptographic authentication.
func (sp *SessionPeer) IsAuthenticated() bool {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	return sp.authVerified
}

func (sp *SessionPeer) attachDataChannel(dc *webrtc.DataChannel) {
	sp.mu.Lock()
	label := dc.Label()
	if label == "control" {
		sp.controlDC = dc
	} else if label == "input" {
		sp.inputDC = dc
	}
	sp.mu.Unlock()

	log.Printf("[WebRTC] Attached DataChannel: %s", label)

	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		sp.handleDataChannelMessage(label, msg.Data)
	})
}

func (sp *SessionPeer) handleDataChannelMessage(channelLabel string, raw []byte) {
	var inputMsg input.InputMessage
	if err := json.Unmarshal(raw, &inputMsg); err != nil {
		return
	}

	if inputMsg.Type == "auth" {
		sp.verifyInBandAuth(&inputMsg)
		return
	}

	// Strictly enforce: Reject all mouse/keyboard input before authentication
	sp.mu.Lock()
	authed := sp.authVerified
	sp.mu.Unlock()

	if !authed {
		return
	}

	if inputMsg.Type == "ping" {
		sp.sendControlReply(input.InputMessage{Type: "pong"})
		return
	}

	if sp.injector != nil {
		_ = sp.injector.HandleInput(&inputMsg)
	}
}

func (sp *SessionPeer) verifyInBandAuth(msg *input.InputMessage) {
	sp.mu.Lock()
	if sp.authVerified {
		sp.mu.Unlock()
		return
	}
	expectedChallenge := sp.authChallenge
	phonePubKey := sp.phonePubKey
	sp.mu.Unlock()

	if msg.Token != expectedChallenge {
		log.Printf("[WebRTC] Auth rejected: challenge mismatch for session %s", sp.sessionID)
		sp.sendControlReply(input.InputMessage{Type: "auth_failed"})
		return
	}

	ts := msg.CreatedAt
	now := time.Now().Unix()
	if ts == 0 {
		ts = now
	} else if ts < now-120 || ts > now+120 {
		log.Printf("[WebRTC] Auth rejected: timestamp skew too large for session %s (ts=%d, now=%d)", sp.sessionID, ts, now)
		sp.sendControlReply(input.InputMessage{Type: "auth_failed"})
		return
	}

	// Verify Schnorr signature of challenge string using Nostr event check
	// Construct simulated event to reuse BIP-340 verification
	evt := &nostr.Event{
		PubKey:    phonePubKey,
		CreatedAt: nostr.Timestamp(ts),
		Kind:      28000, // Ephemeral session auth kind
		Content:   expectedChallenge,
		Tags:      nostr.Tags{},
		Sig:       msg.Sig,
	}
	evt.ID = evt.GetID()

	validSig, err := evt.CheckSignature()
	if err != nil || !validSig {
		log.Printf("[WebRTC] Auth rejected: invalid signature from %s: %v", phonePubKey, err)
		sp.sendControlReply(input.InputMessage{Type: "auth_failed"})
		return
	}

	sp.mu.Lock()
	sp.authVerified = true
	authCb := sp.onAuthSuccess
	sp.mu.Unlock()

	log.Printf("[WebRTC] Session %s in-band authentication PASSED", sp.sessionID)
	sp.sendControlReply(input.InputMessage{Type: "auth_success"})

	if authCb != nil {
		authCb()
	}
}

func (sp *SessionPeer) sendControlReply(msg input.InputMessage) {
	sp.mu.Lock()
	dc := sp.controlDC
	sp.mu.Unlock()

	if dc != nil {
		data, err := json.Marshal(msg)
		if err == nil {
			_ = dc.Send(data)
		}
	}
}

func (sp *SessionPeer) detectTransportType() {
	// Inspect ICE transport
	sctp := sp.peerConn.SCTP()
	if sctp == nil || sctp.Transport() == nil || sctp.Transport().ICETransport() == nil {
		return
	}

	pair, err := sctp.Transport().ICETransport().GetSelectedCandidatePair()
	if err != nil || pair == nil {
		return
	}

	localAddr := pair.Local.Address
	remoteAddr := pair.Remote.Address
	candType := pair.Local.Typ.String()

	transport := "remote"
	if candType == "host" || isLocalNetworkAddress(remoteAddr) {
		transport = "local"
	}

	selectedPairStr := fmt.Sprintf("%s <-> %s (%s)", localAddr, remoteAddr, candType)
	log.Printf("[WebRTC] Selected candidate pair: %s => Transport: %s", selectedPairStr, transport)

	sp.mu.Lock()
	cb := sp.onTransportState
	sp.mu.Unlock()

	if cb != nil {
		cb(transport, selectedPairStr)
	}
}

func isLocalNetworkAddress(ip string) bool {
	return strings.HasPrefix(ip, "192.168.") ||
		strings.HasPrefix(ip, "10.") ||
		strings.HasPrefix(ip, "172.16.") ||
		strings.HasPrefix(ip, "172.17.") ||
		strings.HasPrefix(ip, "172.18.") ||
		strings.HasPrefix(ip, "172.19.") ||
		strings.HasPrefix(ip, "172.2") ||
		strings.HasPrefix(ip, "172.30.") ||
		strings.HasPrefix(ip, "172.31.") ||
		strings.HasPrefix(ip, "127.")
}

// Close gracefully terminates the WebRTC peer connection.
func (sp *SessionPeer) Close() error {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if sp.closed {
		return nil
	}
	sp.closed = true

	if sp.controlDC != nil {
		_ = sp.controlDC.Close()
		sp.controlDC = nil
	}
	if sp.inputDC != nil {
		_ = sp.inputDC.Close()
		sp.inputDC = nil
	}
	if sp.peerConn != nil {
		_ = sp.peerConn.Close()
	}

	return nil
}
