package ipc

import (
	"encoding/json"
	"fmt"
	"laptopcontrol/internal/remote/session"
)

type AgentMessageType string

const (
	MsgInitSession  AgentMessageType = "init_session"
	MsgSignalPhoneToAgent AgentMessageType = "signal_to_agent"
	MsgSignalAgentToPhone AgentMessageType = "signal_to_phone"
	MsgAuthSuccess  AgentMessageType = "auth_success"
	MsgHeartbeat    AgentMessageType = "heartbeat"
	MsgTerminate    AgentMessageType = "terminate"
	MsgStatusUpdate AgentMessageType = "status_update"
)

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
