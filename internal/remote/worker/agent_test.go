package worker

import (
	"encoding/json"
	"testing"

	remoteipc "laptopcontrol/internal/remote/ipc"
	"laptopcontrol/internal/remote/session"
)

// newSignalEnvelope builds an AgentEnvelope carrying a phone->agent signaling packet.
func newSignalEnvelope(t *testing.T, pkt session.SignalingPacket) *remoteipc.AgentEnvelope {
	t.Helper()
	raw, err := json.Marshal(pkt)
	if err != nil {
		t.Fatalf("marshal packet: %v", err)
	}
	return &remoteipc.AgentEnvelope{
		Type:      remoteipc.MsgSignalPhoneToAgent,
		SessionID: pkt.SessionID,
		Payload:   json.RawMessage(raw),
	}
}

// TestSignalingBufferedBeforePeerReady verifies the fix for the startup race that
// caused a black screen: the phone sends its offer before the worker finishes
// initializing capture/encoder/WebRTC. Those packets must be buffered, not dropped.
func TestSignalingBufferedBeforePeerReady(t *testing.T) {
	a := &SessionAgent{
		cfg:      AgentConfig{SessionID: "sess-1"},
		stopChan: make(chan struct{}),
	}
	initChan := make(chan remoteipc.InitSessionPayload, 1)

	offer := session.SignalingPacket{Type: session.SignalOffer, SessionID: "sess-1", SDP: "v=0"}
	a.handleDaemonMessage(newSignalEnvelope(t, offer), initChan)

	if a.peerReady {
		t.Fatal("peer should not be ready yet")
	}
	if len(a.pendingSignals) != 1 {
		t.Fatalf("expected 1 buffered signal, got %d", len(a.pendingSignals))
	}
	if a.pendingSignals[0].Type != session.SignalOffer {
		t.Fatalf("expected buffered offer, got %s", a.pendingSignals[0].Type)
	}
}

// TestPendingSignalBufferIsBounded ensures a flood of early packets cannot grow
// memory without bound.
func TestPendingSignalBufferIsBounded(t *testing.T) {
	a := &SessionAgent{
		cfg:      AgentConfig{SessionID: "sess-2"},
		stopChan: make(chan struct{}),
	}
	initChan := make(chan remoteipc.InitSessionPayload, 1)

	cand := "candidate:1 1 udp 2130706431 192.168.1.2 5000 typ host"
	for i := 0; i < maxPendingSignals+25; i++ {
		pkt := session.SignalingPacket{
			Type:      session.SignalCandidate,
			SessionID: "sess-2",
			Candidate: &session.ICECandidate{Candidate: cand},
		}
		a.handleDaemonMessage(newSignalEnvelope(t, pkt), initChan)
	}

	if len(a.pendingSignals) != maxPendingSignals {
		t.Fatalf("expected buffer capped at %d, got %d", maxPendingSignals, len(a.pendingSignals))
	}
}

// TestMarkPeerReadyDrainsBuffer verifies buffered packets are flushed and the
// buffer cleared once the peer is ready. With a nil peer, replay is a safe no-op.
func TestMarkPeerReadyDrainsBuffer(t *testing.T) {
	a := &SessionAgent{
		cfg:      AgentConfig{SessionID: "sess-3"},
		stopChan: make(chan struct{}),
	}
	initChan := make(chan remoteipc.InitSessionPayload, 1)

	offer := session.SignalingPacket{Type: session.SignalOffer, SessionID: "sess-3", SDP: "v=0"}
	a.handleDaemonMessage(newSignalEnvelope(t, offer), initChan)
	if len(a.pendingSignals) != 1 {
		t.Fatalf("precondition: expected 1 buffered signal, got %d", len(a.pendingSignals))
	}

	a.markPeerReady()

	if !a.peerReady {
		t.Fatal("peer should be marked ready")
	}
	if len(a.pendingSignals) != 0 {
		t.Fatalf("expected buffer drained, got %d", len(a.pendingSignals))
	}

	// A packet arriving after ready must not be buffered (it routes straight through).
	a.handleDaemonMessage(newSignalEnvelope(t, offer), initChan)
	if len(a.pendingSignals) != 0 {
		t.Fatalf("post-ready signal should not be buffered, got %d", len(a.pendingSignals))
	}
}
