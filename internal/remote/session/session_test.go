package session

import (
	"testing"
	"time"
)

func TestSessionIDGeneration(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id, err := GenerateSessionID()
		if err != nil {
			t.Fatalf("unexpected error generating session ID: %v", err)
		}
		if len(id) != 64 {
			t.Fatalf("expected 64-hex char session ID, got %d chars: %s", len(id), id)
		}
		if seen[id] {
			t.Fatalf("collision detected on session ID: %s", id)
		}
		seen[id] = true
	}
}

func TestAuthChallengeGeneration(t *testing.T) {
	c1, err := GenerateAuthChallenge()
	if err != nil {
		t.Fatalf("failed to generate challenge: %v", err)
	}
	c2, _ := GenerateAuthChallenge()
	if c1 == c2 {
		t.Fatalf("expected different auth challenges, got identical")
	}
	if len(c1) != 64 {
		t.Fatalf("expected 64 chars, got %d", len(c1))
	}
}

func TestStateMachineLifecycle(t *testing.T) {
	sm := NewStateMachine(5 * time.Second)
	defer sm.Close()

	// 1. Create first session
	info, err := sm.CreateSession("test_phone_pubkey_123")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	if info.State != StateRequesting {
		t.Fatalf("expected initial state %s, got %s", StateRequesting, info.State)
	}

	// 2. Reject second session while first is active
	_, err = sm.CreateSession("another_phone_pubkey")
	if err != ErrSessionBusy {
		t.Fatalf("expected ErrSessionBusy, got %v", err)
	}

	// 3. Valid transition sequence: Requesting -> Launching -> Signaling -> Connecting -> Authenticating -> Active
	steps := []SessionState{
		StateLaunching,
		StateSignaling,
		StateConnecting,
		StateAuthenticating,
		StateActive,
	}
	for _, next := range steps {
		if err := sm.Transition(info.SessionID, next); err != nil {
			t.Fatalf("failed transition to %s: %v", next, err)
		}
	}

	// 4. Invalid transition: Active cannot directly transition to Requesting
	if err := sm.Transition(info.SessionID, StateRequesting); err == nil {
		t.Fatalf("expected error on invalid transition Active -> Requesting")
	}

	// 5. Terminate cleanly
	if err := sm.Terminate(info.SessionID, "user exit"); err != nil {
		t.Fatalf("failed to terminate session: %v", err)
	}

	cur := sm.GetCurrentSession()
	if cur.State != StateStopped {
		t.Fatalf("expected state Stopped, got %s", cur.State)
	}

	// 6. Now a new session CAN be created since previous is Stopped
	info2, err := sm.CreateSession("test_phone_pubkey_456")
	if err != nil {
		t.Fatalf("expected to be able to create new session after stop, got: %v", err)
	}
	if info2.SessionID == info.SessionID {
		t.Fatalf("expected new session ID")
	}
}

func TestHeartbeatExpiration(t *testing.T) {
	// Create state machine with short 100ms timeout
	sm := NewStateMachine(150 * time.Millisecond)
	defer sm.Close()

	terminated := make(chan string, 1)
	sm.SetOnTerminate(func(sessID string) {
		select {
		case terminated <- sessID:
		default:
		}
	})

	info, err := sm.CreateSession("phone_123")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Wait for watchdog to trigger expiration
	select {
	case id := <-terminated:
		if id != info.SessionID {
			t.Fatalf("expected terminated ID %s, got %s", info.SessionID, id)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for watchdog termination")
	}

	cur := sm.GetCurrentSession()
	if cur.State != StateFailed {
		t.Fatalf("expected state Failed on expiration, got %s", cur.State)
	}
}

func TestSignalingPacketValidation(t *testing.T) {
	validOffer := SignalingPacket{
		Type:      SignalOffer,
		SessionID: "valid_session_123",
		SDP:       "v=0\r\no=- 0 0 IN IP4 127.0.0.1...",
	}
	if err := validOffer.Validate(); err != nil {
		t.Fatalf("expected valid offer to pass, got: %v", err)
	}

	missingSDP := SignalingPacket{
		Type:      SignalOffer,
		SessionID: "valid_session_123",
	}
	if err := missingSDP.Validate(); err == nil {
		t.Fatalf("expected error on missing SDP")
	}

	missingSession := SignalingPacket{
		Type: SignalOffer,
		SDP:  "v=0...",
	}
	if err := missingSession.Validate(); err == nil {
		t.Fatalf("expected error on missing session ID")
	}

	candidate := SignalingPacket{
		Type:      SignalCandidate,
		SessionID: "sess_1",
		Candidate: &ICECandidate{Candidate: "candidate:1 1 UDP 2130706431 192.168.1.10 8765 typ host"},
	}
	if err := candidate.Validate(); err != nil {
		t.Fatalf("expected valid candidate to pass, got: %v", err)
	}

	emptyCandidate := SignalingPacket{
		Type:      SignalCandidate,
		SessionID: "sess_1",
	}
	if err := emptyCandidate.Validate(); err == nil {
		t.Fatalf("expected error on empty candidate")
	}
}
