package pairing

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"sync"
	"time"
)

const (
	// TokenLifetime is the strict validity window for a pairing token
	TokenLifetime = 5 * time.Minute
	// TokenEntropyBytes ensures 128-bit cryptographic entropy (16 bytes = 32 hex chars)
	TokenEntropyBytes = 16
)

// Manager handles thread-safe pairing token lifecycle, expiration, and atomic single-use consumption.
type Manager struct {
	mu        sync.Mutex
	token     string
	expiresAt time.Time
	consumed  bool
}

// NewManager initializes a new pairing manager with an active pairing token.
func NewManager() *Manager {
	m := &Manager{}
	m.Regenerate()
	return m
}

// Regenerate generates a fresh 128-bit cryptographically secure pairing token with 5-minute expiry.
func (m *Manager) Regenerate() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	b := make([]byte, TokenEntropyBytes)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp + random if needed, though rand.Read rarely fails
		for i := range b {
			b[i] = byte(time.Now().UnixNano() >> (i * 8))
		}
	}

	m.token = hex.EncodeToString(b)
	m.expiresAt = time.Now().Add(TokenLifetime)
	m.consumed = false
	return m.token
}

// GetToken returns the current pairing token (used for generating QR code and pairing link).
func (m *Manager) GetToken() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.token
}

// GetExpiresAt returns when the current pairing token expires.
func (m *Manager) GetExpiresAt() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.expiresAt
}

// Validate verifies if candidate token is valid, unexpired, and unconsumed without consuming it.
func (m *Manager) Validate(candidate string) (bool, string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if candidate == "" {
		return false, "Missing pairing token"
	}
	if len(candidate) != TokenEntropyBytes*2 {
		return false, "Invalid pairing token format"
	}
	if m.consumed {
		return false, "Pairing token has already been consumed"
	}
	if time.Now().After(m.expiresAt) {
		return false, "Pairing token has expired"
	}
	if subtle.ConstantTimeCompare([]byte(m.token), []byte(candidate)) != 1 {
		return false, "Invalid pairing token"
	}
	return true, ""
}

// ValidateAndConsume validates the candidate token. If valid, unexpired, and unconsumed,
// it consumes the token atomically, regenerates a fresh token, and returns (true, "").
// Once consumed or expired, it returns (false, errorMsg).
// Thread-safe and protected against concurrent pairing races.
func (m *Manager) ValidateAndConsume(candidate string) (bool, string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if candidate == "" {
		return false, "Missing pairing token"
	}

	// Must be exactly 32 hex chars (16 bytes)
	if len(candidate) != TokenEntropyBytes*2 {
		return false, "Invalid pairing token format"
	}

	// Check if already consumed
	if m.consumed {
		return false, "Pairing token has already been consumed"
	}

	// Check if expired
	if time.Now().After(m.expiresAt) {
		return false, "Pairing token has expired"
	}

	// Constant-time equality check to prevent timing attacks
	if subtle.ConstantTimeCompare([]byte(m.token), []byte(candidate)) != 1 {
		return false, "Invalid pairing token"
	}

	// Token is valid! Mark consumed immediately so it can never be used again
	m.consumed = true

	// Automatically regenerate a fresh token for subsequent pairing cycles
	b := make([]byte, TokenEntropyBytes)
	_, _ = rand.Read(b)
	m.token = hex.EncodeToString(b)
	m.expiresAt = time.Now().Add(TokenLifetime)
	m.consumed = false

	return true, ""
}
