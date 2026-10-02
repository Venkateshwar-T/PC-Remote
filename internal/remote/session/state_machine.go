package session

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	// DefaultHeartbeatTimeout is the grace period before an inactive session is forcibly closed.
	DefaultHeartbeatTimeout = 12 * time.Second

	// DefaultSessionLifetime is the maximum allowable lifetime of any single remote session.
	DefaultSessionLifetime = 4 * time.Hour
)

var (
	ErrSessionBusy       = errors.New("a remote desktop session is already active")
	ErrNoSuchSession     = errors.New("no active session matches the provided session ID")
	ErrInvalidTransition = errors.New("invalid session state transition")
	ErrSessionExpired    = errors.New("session has expired due to missed heartbeats")
)

// StateMachine manages the lifecycle, concurrency, and health of Remote Desktop sessions.
type StateMachine struct {
	mu               sync.RWMutex
	currentSession   *SessionInfo
	heartbeatTimeout time.Duration
	onStateChange    func(oldState, newState SessionState, info *SessionInfo)
	onTerminate      func(sessionID string)
	stopChan         chan struct{}
	stopOnce         sync.Once
}

// NewStateMachine initializes a new session state machine.
func NewStateMachine(heartbeatTimeout time.Duration) *StateMachine {
	if heartbeatTimeout <= 0 {
		heartbeatTimeout = DefaultHeartbeatTimeout
	}
	sm := &StateMachine{
		heartbeatTimeout: heartbeatTimeout,
		stopChan:         make(chan struct{}),
	}
	go sm.heartbeatWatchdog()
	return sm
}

// SetOnStateChange registers a callback invoked on state transitions.
func (sm *StateMachine) SetOnStateChange(fn func(oldState, newState SessionState, info *SessionInfo)) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.onStateChange = fn
}

// SetOnTerminate registers a callback invoked when a session must be torn down.
func (sm *StateMachine) SetOnTerminate(fn func(sessionID string)) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.onTerminate = fn
}

// CreateSession attempts to initialize a new session for an authorized phone public key.
// Returns ErrSessionBusy if an existing session is currently active.
func (sm *StateMachine) CreateSession(phonePubKey string) (*SessionInfo, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Enforce single active session limit
	if sm.currentSession != nil && sm.isSessionActiveLocked() {
		return nil, ErrSessionBusy
	}

	sessionID, err := GenerateSessionID()
	if err != nil {
		return nil, fmt.Errorf("could not generate session ID: %w", err)
	}

	challenge, err := GenerateAuthChallenge()
	if err != nil {
		return nil, fmt.Errorf("could not generate auth challenge: %w", err)
	}

	now := time.Now()
	info := &SessionInfo{
		SessionID:     sessionID,
		PhonePubKey:   phonePubKey,
		CreatedAt:     now,
		LastHeartbeat: now,
		State:         StateRequesting,
		AuthChallenge: challenge,
		AuthVerified:  false,
	}

	sm.currentSession = info
	if sm.onStateChange != nil {
		sm.onStateChange(StateIdle, StateRequesting, info)
	}

	return info, nil
}

// Transition moves the current session to a new state if valid.
func (sm *StateMachine) Transition(sessionID string, newState SessionState) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.currentSession == nil || sm.currentSession.SessionID != sessionID {
		return ErrNoSuchSession
	}

	oldState := sm.currentSession.State
	if !isValidTransition(oldState, newState) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, oldState, newState)
	}

	sm.currentSession.State = newState
	sm.currentSession.LastHeartbeat = time.Now()

	if sm.onStateChange != nil {
		sm.onStateChange(oldState, newState, sm.currentSession)
	}

	if newState == StateStopped || newState == StateFailed {
		if sm.onTerminate != nil {
			go sm.onTerminate(sessionID)
		}
	}

	return nil
}

// RecordHeartbeat refreshes the last active timestamp of the specified session.
func (sm *StateMachine) RecordHeartbeat(sessionID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.currentSession == nil || sm.currentSession.SessionID != sessionID {
		return ErrNoSuchSession
	}

	sm.currentSession.LastHeartbeat = time.Now()
	return nil
}

// MarkAuthVerified marks that the in-band cryptographic authentication check passed.
func (sm *StateMachine) MarkAuthVerified(sessionID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.currentSession == nil || sm.currentSession.SessionID != sessionID {
		return ErrNoSuchSession
	}

	sm.currentSession.AuthVerified = true
	return nil
}

// GetCurrentSession returns a copy of the current session information, or nil if none.
func (sm *StateMachine) GetCurrentSession() *SessionInfo {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.currentSession == nil {
		return nil
	}
	cpy := *sm.currentSession
	return &cpy
}

// Terminate cleanly halts the current session with an explicit state.
func (sm *StateMachine) Terminate(sessionID string, reason string) error {
	sm.mu.Lock()
	if sm.currentSession == nil || sm.currentSession.SessionID != sessionID {
		sm.mu.Unlock()
		return ErrNoSuchSession
	}

	oldState := sm.currentSession.State
	sm.currentSession.State = StateStopped
	info := sm.currentSession
	sm.mu.Unlock()

	if sm.onStateChange != nil {
		sm.onStateChange(oldState, StateStopped, info)
	}

	if sm.onTerminate != nil {
		sm.onTerminate(sessionID)
	}

	return nil
}

// Close halts background monitors and terminates any active session.
func (sm *StateMachine) Close() {
	sm.stopOnce.Do(func() {
		close(sm.stopChan)
		sm.mu.Lock()
		if sm.currentSession != nil && sm.isSessionActiveLocked() {
			sessID := sm.currentSession.SessionID
			sm.currentSession.State = StateStopped
			sm.mu.Unlock()
			if sm.onTerminate != nil {
				sm.onTerminate(sessID)
			}
			return
		}
		sm.mu.Unlock()
	})
}

func (sm *StateMachine) isSessionActiveLocked() bool {
	if sm.currentSession == nil {
		return false
	}
	s := sm.currentSession.State
	return s != StateIdle && s != StateStopped && s != StateFailed
}

func isValidTransition(from, to SessionState) bool {
	allowed, ok := ValidStateTransitions[from]
	if !ok {
		return false
	}
	for _, a := range allowed {
		if a == to {
			return true
		}
	}
	return false
}

func (sm *StateMachine) heartbeatWatchdog() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-sm.stopChan:
			return
		case <-ticker.C:
			sm.checkHeartbeats()
		}
	}
}

func (sm *StateMachine) checkHeartbeats() {
	sm.mu.Lock()
	if sm.currentSession == nil || !sm.isSessionActiveLocked() {
		sm.mu.Unlock()
		return
	}

	now := time.Now()
	if now.Sub(sm.currentSession.LastHeartbeat) > sm.heartbeatTimeout ||
		now.Sub(sm.currentSession.CreatedAt) > DefaultSessionLifetime {
		sessID := sm.currentSession.SessionID
		oldState := sm.currentSession.State
		sm.currentSession.State = StateFailed
		info := sm.currentSession
		sm.mu.Unlock()

		if sm.onStateChange != nil {
			sm.onStateChange(oldState, StateFailed, info)
		}
		if sm.onTerminate != nil {
			go sm.onTerminate(sessID)
		}
		return
	}
	sm.mu.Unlock()
}
