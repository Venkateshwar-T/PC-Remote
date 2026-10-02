package session

import (
	"fmt"
	"time"
)

// SessionState represents the explicit lifecycle states of a Remote Desktop session.
type SessionState string

const (
	StateIdle           SessionState = "idle"
	StateRequesting     SessionState = "requesting"
	StateLaunching      SessionState = "launching"
	StateSignaling      SessionState = "signaling"
	StateConnecting     SessionState = "connecting"
	StateAuthenticating SessionState = "authenticating"
	StateActive         SessionState = "active"
	StateStopping       SessionState = "stopping"
	StateStopped        SessionState = "stopped"
	StateFailed         SessionState = "failed"
)

// ValidStateTransitions defines the permitted state graph to prevent illegal transitions.
var ValidStateTransitions = map[SessionState][]SessionState{
	StateIdle:           {StateRequesting},
	StateRequesting:     {StateLaunching, StateFailed, StateStopped},
	StateLaunching:      {StateSignaling, StateFailed, StateStopped},
	StateSignaling:      {StateConnecting, StateStopping, StateFailed, StateStopped},
	StateConnecting:     {StateAuthenticating, StateStopping, StateFailed, StateStopped},
	StateAuthenticating: {StateActive, StateStopping, StateFailed, StateStopped},
	StateActive:         {StateStopping, StateFailed, StateStopped},
	StateStopping:       {StateStopped, StateFailed},
	StateStopped:        {StateIdle, StateRequesting},
	StateFailed:         {StateIdle, StateRequesting},
}

// SessionInfo holds the metadata for an active or past session.
type SessionInfo struct {
	SessionID       string       `json:"sessionId"`
	PhonePubKey     string       `json:"phonePubKey"`
	CreatedAt       time.Time    `json:"createdAt"`
	LastHeartbeat   time.Time    `json:"lastHeartbeat"`
	State           SessionState `json:"state"`
	AuthChallenge   string       `json:"authChallenge,omitempty"`
	AuthVerified    bool         `json:"authVerified"`
	ClientCandidate string       `json:"clientCandidate,omitempty"`
	TransportType   string       `json:"transportType,omitempty"` // "local" or "remote"
}

// SessionRequest is the initial session initiation packet from a paired phone.
type SessionRequest struct {
	SessionID  string `json:"sessionId"`
	Action     string `json:"action"` // "remote_request"
	Timestamp  int64  `json:"timestamp"`
	DeviceName string `json:"deviceName,omitempty"`
}

// SessionResponse is the service reply to a SessionRequest.
type SessionResponse struct {
	Status        string `json:"status"` // "accepted", "busy", "unauthorized", "error"
	SessionID     string `json:"sessionId,omitempty"`
	AuthChallenge string `json:"authChallenge,omitempty"`
	Message       string `json:"message,omitempty"`
	Timestamp     int64  `json:"timestamp"`
}

// SignalingMessageType defines the supported WebRTC signaling envelopes.
type SignalingMessageType string

const (
	SignalOffer         SignalingMessageType = "offer"
	SignalAnswer        SignalingMessageType = "answer"
	SignalCandidate     SignalingMessageType = "ice-candidate"
	SignalSessionAuth   SignalingMessageType = "session-auth"
	SignalAuthChallenge SignalingMessageType = "session-challenge"
	SignalSessionAccept SignalingMessageType = "session-accepted"
	SignalSessionReject SignalingMessageType = "session-rejected"
	SignalSessionClose  SignalingMessageType = "session-close"
	SignalHeartbeat     SignalingMessageType = "heartbeat"
	SignalError         SignalingMessageType = "error"
)

// SignalingPacket is the typed envelope exchanged over Nostr or LAN HTTP for WebRTC negotiation.
type SignalingPacket struct {
	Type      SignalingMessageType `json:"type"`
	SessionID string               `json:"sessionId"`
	SDP       string               `json:"sdp,omitempty"`
	Candidate *ICECandidate        `json:"candidate,omitempty"`
	AuthData  string               `json:"authData,omitempty"`
	Error     string               `json:"error,omitempty"`
	Message   string               `json:"message,omitempty"`
	Timestamp int64                `json:"timestamp"`
}

// ICECandidate represents an individual WebRTC ICE candidate payload.
type ICECandidate struct {
	Candidate     string  `json:"candidate"`
	SDPMid        *string `json:"sdpMid,omitempty"`
	SDPMLineIndex *uint16 `json:"sdpMLineIndex,omitempty"`
}

// Validate checks the signaling packet for schema correctness and bounds.
func (p *SignalingPacket) Validate() error {
	if p.SessionID == "" || len(p.SessionID) > 128 {
		return fmt.Errorf("invalid or missing sessionId")
	}

	// Canonicalize signal type aliases
	switch p.Type {
	case "candidate":
		p.Type = SignalCandidate
	case "close":
		p.Type = SignalSessionClose
	}

	switch p.Type {
	case SignalOffer, SignalAnswer:
		if p.SDP == "" {
			return fmt.Errorf("missing SDP in %s packet", p.Type)
		}
		if len(p.SDP) > 65536 {
			return fmt.Errorf("SDP payload exceeds maximum size")
		}
	case SignalCandidate:
		if p.Candidate == nil || p.Candidate.Candidate == "" {
			return fmt.Errorf("missing candidate data in ice-candidate packet")
		}
		if len(p.Candidate.Candidate) > 2048 {
			return fmt.Errorf("ICE candidate exceeds maximum size")
		}
	case SignalSessionAuth:
		if p.AuthData == "" {
			return fmt.Errorf("missing authData in session-auth packet")
		}
		if len(p.AuthData) > 2048 {
			return fmt.Errorf("authData exceeds maximum size")
		}
	case SignalSessionClose, SignalHeartbeat:
		// No extra payload required
	case SignalError:
		// Optional error string
	default:
		return fmt.Errorf("unsupported signaling message type: %s", p.Type)
	}
	return nil
}
