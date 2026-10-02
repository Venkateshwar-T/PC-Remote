package ipc

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"laptopcontrol/internal/remote/session"
)

type AgentMessageType string

const (
	MsgAgentHello         AgentMessageType = "agent_hello"
	MsgInitSession        AgentMessageType = "init_session"
	MsgSignalPhoneToAgent AgentMessageType = "signal_to_agent"
	MsgSignalAgentToPhone AgentMessageType = "signal_to_phone"
	MsgAuthSuccess        AgentMessageType = "auth_success"
	MsgHeartbeat          AgentMessageType = "heartbeat"
	MsgTerminate          AgentMessageType = "terminate"
	MsgStatusUpdate       AgentMessageType = "status_update"
)

const (
	MaxHelloMessageSize = 4096
	HelloTimeout        = 5 * time.Second
)

// AgentHelloPayload is the first payload sent by session-agent to initiate named pipe handshake.
type AgentHelloPayload struct {
	SessionID string `json:"sessionId"`
	Timestamp int64  `json:"timestamp"`
}

// AgentEnvelope is the structured JSON frame exchanged over the session named pipe.
type AgentEnvelope struct {
	Type      AgentMessageType         `json:"type"`
	SessionID string                   `json:"sessionId"`
	Payload   json.RawMessage          `json:"payload,omitempty"`
	Timestamp int64                    `json:"timestamp"`
}

// InitSessionPayload contains the bootstrap configuration passed to session-agent.
type InitSessionPayload struct {
	AuthChallenge string   `json:"authChallenge"`
	PhonePubKey   string   `json:"phonePubKey"`
	STUNServers   []string `json:"stunServers,omitempty"`
	UDPPort       int      `json:"udpPort,omitempty"`
}

// SignalPayload wraps a WebRTC signaling packet between daemon and agent.
type SignalPayload struct {
	Packet session.SignalingPacket `json:"packet"`
}

// StatusUpdatePayload carries health or transport stats.
type StatusUpdatePayload struct {
	State         session.SessionState `json:"state"`
	TransportType string               `json:"transportType,omitempty"`
	SelectedPair  string               `json:"selectedPair,omitempty"`
	Message       string               `json:"message,omitempty"`
}

// EncodeEnvelope serializes an envelope with trailing newline for line-delimited JSON streaming.
func EncodeEnvelope(msgType AgentMessageType, sessionID string, payload interface{}) ([]byte, error) {
	var raw json.RawMessage
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal payload: %w", err)
		}
		raw = b
	}
	env := AgentEnvelope{
		Type:      msgType,
		SessionID: sessionID,
		Payload:   raw,
	}
	data, err := json.Marshal(env)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// ParseAndValidateHello parses a raw line-delimited JSON buffer and validates the agent handshake.
func ParseAndValidateHello(line []byte, expectedSessionID string) (*AgentEnvelope, error) {
	if len(line) == 0 {
		return nil, errors.New("empty handshake hello message")
	}
	if len(line) > MaxHelloMessageSize {
		return nil, errors.New("oversized agent hello handshake")
	}

	var env AgentEnvelope
	if err := json.Unmarshal(line, &env); err != nil {
		return nil, fmt.Errorf("malformed agent hello JSON: %w", err)
	}

	if env.Type != MsgAgentHello {
		return nil, fmt.Errorf("invalid handshake message type: expected %q, got %q", MsgAgentHello, env.Type)
	}

	if env.SessionID == "" || len(env.SessionID) > 128 {
		return nil, errors.New("invalid or empty session ID in agent hello")
	}
	if expectedSessionID != "" && env.SessionID != expectedSessionID {
		return nil, fmt.Errorf("session ID mismatch in agent hello: expected %q, got %q", expectedSessionID, env.SessionID)
	}

	now := time.Now().Unix()
	if env.Timestamp != 0 && (env.Timestamp < now-60 || env.Timestamp > now+60) {
		return nil, fmt.Errorf("stale or invalid timestamp in agent hello: %d (current: %d)", env.Timestamp, now)
	}

	if len(env.Payload) > 0 && string(env.Payload) != "null" {
		var payload AgentHelloPayload
		if err := json.Unmarshal(env.Payload, &payload); err == nil {
			if payload.SessionID != "" && expectedSessionID != "" && payload.SessionID != expectedSessionID {
				return nil, fmt.Errorf("session ID mismatch in hello payload: expected %q, got %q", expectedSessionID, payload.SessionID)
			}
		}
	}

	return &env, nil
}
