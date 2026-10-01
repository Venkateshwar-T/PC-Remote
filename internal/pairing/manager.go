package pairing

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
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

// generateSecureToken derives a 128-bit pairing token exclusively from OS cryptographically secure entropy.
func generateSecureToken() (string, error) {
	b := make([]byte, TokenEntropyBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to read cryptographically secure entropy: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// NewManager initializes a new pairing manager with a cryptographically secure pairing token.
// Fails closed if secure OS randomness is unavailable.
func NewManager() (*Manager, error) {
	token, err := generateSecureToken()
	if err != nil {
		return nil, err
	}
	return &Manager{
		token:     token,
		expiresAt: time.Now().Add(TokenLifetime),
		consumed:  false,
	}, nil
}

// Regenerate generates a fresh 128-bit cryptographically secure pairing token with 5-minute expiry.
// Fails closed if secure OS randomness is unavailable.
func (m *Manager) Regenerate() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	newToken, err := generateSecureToken()
	if err != nil {
		return "", err
	}

	m.token = newToken
	m.expiresAt = time.Now().Add(TokenLifetime)
	m.consumed = false
	return m.token, nil
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

	// Generate NEW secure token first BEFORE mutating state!
	newToken, err := generateSecureToken()
	if err != nil {
		return false, "Internal security failure: cannot generate secure rotation token"
	}

	// Token is valid and new token generated! Mark consumed and install new token
	m.consumed = true
	m.token = newToken
	m.expiresAt = time.Now().Add(TokenLifetime)
	m.consumed = false

	return true, ""
}
