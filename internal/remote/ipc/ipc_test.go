package ipc

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
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

func TestHelloEnvelopeEncoding(t *testing.T) {
	now := time.Now().Unix()
	payload := AgentHelloPayload{
		SessionID: "sess_test_123",
		Timestamp: now,
	}

	data, err := EncodeEnvelope(MsgAgentHello, "sess_test_123", payload)
	if err != nil {
		t.Fatalf("EncodeEnvelope failed: %v", err)
	}

	env, err := ParseAndValidateHello(data[:len(data)-1], "sess_test_123")
	if err != nil {
		t.Fatalf("ParseAndValidateHello failed: %v", err)
	}

	if env.Type != MsgAgentHello {
		t.Fatalf("expected type %s, got %s", MsgAgentHello, env.Type)
	}
	if env.SessionID != "sess_test_123" {
		t.Fatalf("expected session ID sess_test_123, got %s", env.SessionID)
	}
}

func TestParseAndValidateHello_WrongType(t *testing.T) {
	data, _ := EncodeEnvelope(MsgInitSession, "sess_test_123", nil)
	_, err := ParseAndValidateHello(data[:len(data)-1], "sess_test_123")
	if err == nil {
		t.Fatalf("expected error for wrong message type, got nil")
	}
}

func TestParseAndValidateHello_WrongSessionID(t *testing.T) {
	payload := AgentHelloPayload{SessionID: "sess_wrong", Timestamp: time.Now().Unix()}
	data, _ := EncodeEnvelope(MsgAgentHello, "sess_wrong", payload)
	_, err := ParseAndValidateHello(data[:len(data)-1], "sess_expected")
	if err == nil {
		t.Fatalf("expected error for wrong session ID, got nil")
	}
}

func TestParseAndValidateHello_EmptySessionID(t *testing.T) {
	data, _ := EncodeEnvelope(MsgAgentHello, "", nil)
	_, err := ParseAndValidateHello(data[:len(data)-1], "")
	if err == nil {
		t.Fatalf("expected error for empty session ID, got nil")
	}
}

func TestParseAndValidateHello_MalformedJSON(t *testing.T) {
	_, err := ParseAndValidateHello([]byte("{not-json"), "sess_test")
	if err == nil {
		t.Fatalf("expected error for malformed JSON, got nil")
	}
}

func TestParseAndValidateHello_Oversized(t *testing.T) {
	oversized := []byte(strings.Repeat("a", MaxHelloMessageSize+10))
	_, err := ParseAndValidateHello(oversized, "sess_test")
	if err == nil {
		t.Fatalf("expected error for oversized hello, got nil")
	}
}

func TestParseAndValidateHello_StaleTimestamp(t *testing.T) {
	staleEnv := AgentEnvelope{
		Type:      MsgAgentHello,
		SessionID: "sess_test",
		Timestamp: time.Now().Unix() - 120, // 2 minutes ago
	}
	data, _ := json.Marshal(staleEnv)
	_, err := ParseAndValidateHello(data, "sess_test")
	if err == nil {
		t.Fatalf("expected error for stale timestamp, got nil")
	}

	futureEnv := AgentEnvelope{
		Type:      MsgAgentHello,
		SessionID: "sess_test",
		Timestamp: time.Now().Unix() + 120, // 2 minutes in future
	}
	dataFuture, _ := json.Marshal(futureEnv)
	_, err = ParseAndValidateHello(dataFuture, "sess_test")
	if err == nil {
		t.Fatalf("expected error for future timestamp, got nil")
	}
}

func TestSameBufferedReaderReused(t *testing.T) {
	// Create buffered reader with multiple line-delimited envelopes
	helloData, _ := EncodeEnvelope(MsgAgentHello, "sess_1", AgentHelloPayload{SessionID: "sess_1", Timestamp: time.Now().Unix()})
	initData, _ := EncodeEnvelope(MsgInitSession, "sess_1", InitSessionPayload{AuthChallenge: "c1", UDPPort: 8765})
	heartbeatData, _ := EncodeEnvelope(MsgHeartbeat, "sess_1", nil)

	combined := append(helloData, append(initData, heartbeatData...)...)
	reader := bufio.NewReader(bytes.NewReader(combined))

	// 1. Read hello bounded line-by-line using ReadByte (same logic as Accept)
	var helloLine []byte
	for {
		b, err := reader.ReadByte()
		if err != nil {
			t.Fatalf("failed reading hello: %v", err)
		}
		if b == '\n' {
			break
		}
		helloLine = append(helloLine, b)
	}

	env, err := ParseAndValidateHello(helloLine, "sess_1")
	if err != nil {
		t.Fatalf("ParseAndValidateHello failed: %v", err)
	}
	if env.Type != MsgAgentHello {
		t.Fatalf("unexpected type: %s", env.Type)
	}

	// 2. Reuse the EXACT SAME reader to read subsequent messages (same logic as readLoop)
	line2, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatalf("failed to read line2 from reused reader: %v", err)
	}
	var env2 AgentEnvelope
	if err := json.Unmarshal(line2, &env2); err != nil {
		t.Fatalf("failed to unmarshal env2: %v", err)
	}
	if env2.Type != MsgInitSession {
		t.Fatalf("expected env2 to be %s, got %s", MsgInitSession, env2.Type)
	}

	line3, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatalf("failed to read line3 from reused reader: %v", err)
	}
	var env3 AgentEnvelope
	if err := json.Unmarshal(line3, &env3); err != nil {
		t.Fatalf("failed to unmarshal env3: %v", err)
	}
	if env3.Type != MsgHeartbeat {
		t.Fatalf("expected env3 to be %s, got %s", MsgHeartbeat, env3.Type)
	}
}

func TestSessionPipe_WindowsIntegration(t *testing.T) {
	// Integration test for Named Pipe handshake on Windows
	pipeName := fmt.Sprintf(`\\.\pipe\PCRemote_Session_Test_%d`, time.Now().UnixNano())
	sessionID := "test_session_integ"

	server, err := NewSessionPipeServer(pipeName, sessionID, "")
	if err != nil {
		t.Skipf("Skipping integration test (likely non-Windows or unable to listen): %v", err)
		return
	}
	defer server.Close()

	// Server accepts asynchronously
	acceptDone := make(chan error, 1)
	go func() {
		acceptDone <- server.Accept()
	}()

	// Client connects and sends hello
	client, err := ConnectSessionPipe(pipeName, sessionID, 3*time.Second)
	if err != nil {
		t.Fatalf("ConnectSessionPipe failed: %v", err)
	}
	defer client.Close()

	// Verify server accepted successfully after reading hello and verifying identity
	select {
	case err := <-acceptDone:
		if err != nil {
			t.Fatalf("server.Accept() failed: %v", err)
		}
	case <-time.After(4 * time.Second):
		t.Fatalf("timeout waiting for server.Accept()")
	}

	// Verify server can send bootstrap InitSession and client receives it on reused reader
	initPayload := InitSessionPayload{
		AuthChallenge: "test_challenge",
		PhonePubKey:   "test_pubkey",
		UDPPort:       8765,
	}
	receivedInit := make(chan *AgentEnvelope, 1)
	client.SetOnMessage(func(env *AgentEnvelope) {
		if env.Type == MsgInitSession {
			receivedInit <- env
		}
	})
	if err := client.StartReadLoop(); err != nil {
		t.Fatalf("client.StartReadLoop failed: %v", err)
	}

	err = server.Send(MsgInitSession, sessionID, initPayload)
	if err != nil {
		t.Fatalf("server.Send(MsgInitSession) failed: %v", err)
	}

	select {
	case env := <-receivedInit:
		if env.SessionID != sessionID {
			t.Fatalf("expected session ID %s, got %s", sessionID, env.SessionID)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for client to receive MsgInitSession")
	}
}

func TestSessionPipe_InitSessionDeliveryWithDeferredReadLoop(t *testing.T) {
	// Regression test proving InitSession sent by server BEFORE client installs
	// callback or starts readLoop is safely buffered and delivered without message loss.
	pipeName := fmt.Sprintf(`\\.\pipe\PCRemote_Session_Race_Test_%d`, time.Now().UnixNano())
	sessionID := "test_session_race_fix"

	server, err := NewSessionPipeServer(pipeName, sessionID, "")
	if err != nil {
		t.Skipf("Skipping integration test: %v", err)
		return
	}
	defer server.Close()

	acceptDone := make(chan error, 1)
	go func() {
		acceptDone <- server.Accept()
	}()

	// Connect client - note that ConnectSessionPipe completes the hello handshake
	// but does NOT start readLoop yet.
	client, err := ConnectSessionPipe(pipeName, sessionID, 3*time.Second)
	if err != nil {
		t.Fatalf("ConnectSessionPipe failed: %v", err)
	}
	defer client.Close()

	select {
	case err := <-acceptDone:
		if err != nil {
			t.Fatalf("server.Accept() failed: %v", err)
		}
	case <-time.After(4 * time.Second):
		t.Fatalf("timeout waiting for server.Accept()")
	}

	// Server immediately sends InitSession into the pipe.
	// At this point client has NOT registered SetOnMessage or called StartReadLoop.
	initPayload := InitSessionPayload{
		AuthChallenge: "race_test_challenge_abc",
		PhonePubKey:   "race_test_phone_key_def",
		UDPPort:       9999,
	}
	err = server.Send(MsgInitSession, sessionID, initPayload)
	if err != nil {
		t.Fatalf("server.Send(MsgInitSession) failed: %v", err)
	}

	// Now register callback on client
	receivedInit := make(chan *AgentEnvelope, 1)
	client.SetOnMessage(func(env *AgentEnvelope) {
		if env.Type == MsgInitSession {
			receivedInit <- env
		}
	})

	// Start reader loop
	if err := client.StartReadLoop(); err != nil {
		t.Fatalf("StartReadLoop failed: %v", err)
	}

	// Verify calling StartReadLoop a second time fails (ensures exactly one reader)
	if err := client.StartReadLoop(); err == nil {
		t.Fatalf("expected error on duplicate StartReadLoop, got nil")
	}

	// Verify InitSession was not lost and was delivered cleanly
	select {
	case env := <-receivedInit:
		if env.SessionID != sessionID {
			t.Fatalf("expected session ID %s, got %s", sessionID, env.SessionID)
		}
		var decoded InitSessionPayload
		if err := json.Unmarshal(env.Payload, &decoded); err != nil {
			t.Fatalf("failed to unmarshal InitSession payload: %v", err)
		}
		if decoded.AuthChallenge != "race_test_challenge_abc" {
			t.Fatalf("expected AuthChallenge 'race_test_challenge_abc', got '%s'", decoded.AuthChallenge)
		}
		if decoded.UDPPort != 9999 {
			t.Fatalf("expected UDPPort 9999, got %d", decoded.UDPPort)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout: MsgInitSession was lost before or during StartReadLoop")
	}
}
