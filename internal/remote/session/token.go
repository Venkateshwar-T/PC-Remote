package session

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// GenerateSessionID produces a cryptographically secure 256-bit (64 hex characters) unpredictable session ID.
func GenerateSessionID() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate secure session ID: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

// GenerateAuthChallenge produces a cryptographically secure 256-bit random challenge for in-band auth.
func GenerateAuthChallenge() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate secure auth challenge: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}
