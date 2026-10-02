package ipc

import (
	"encoding/json"
	"testing"
)

func TestEncodeEnvelope(t *testing.T) {
	initPayload := InitSessionPayload{
		AuthChallenge: "challenge_hex_123",
		PhonePubKey:   "phone_pub_456",
		UDPPort:       8765,
	}

	data, err := EncodeEnvelope(MsgInitSession, "session_789", initPayload)
	if err != nil {
		t.Fatalf("failed to encode envelope: %v", err)
	}

	if data[len(data)-1] != '\n' {
		t.Fatalf("expected trailing newline delimiter")
	}

	var env AgentEnvelope
	if err := json.Unmarshal(data[:len(data)-1], &env); err != nil {
		t.Fatalf("failed to decode envelope: %v", err)
	}

	if env.Type != MsgInitSession {
		t.Fatalf("expected type %s, got %s", MsgInitSession, env.Type)
	}
	if env.SessionID != "session_789" {
		t.Fatalf("expected session ID session_789, got %s", env.SessionID)
	}

	var decodedPayload InitSessionPayload
	if err := json.Unmarshal(env.Payload, &decodedPayload); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	if decodedPayload.AuthChallenge != "challenge_hex_123" {
		t.Fatalf("expected challenge challenge_hex_123, got %s", decodedPayload.AuthChallenge)
	}
	if decodedPayload.UDPPort != 8765 {
		t.Fatalf("expected port 8765, got %d", decodedPayload.UDPPort)
	}
}
