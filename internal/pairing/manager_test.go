package pairing

import (
	"encoding/hex"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Scenario A: token has 32 hex characters and normal generation succeeds
func TestPairingManager_ScenarioA_ValidTokenGeneration(t *testing.T) {
	mgr, err := NewManager()
	if err != nil {
		t.Fatalf("failed to initialize pairing manager: %v", err)
	}
	token := mgr.GetToken()

	if len(token) != 32 {
		t.Fatalf("expected 32 hex chars (128 bits), got %d (%s)", len(token), token)
	}

	rawBytes, errHex := hex.DecodeString(token)
	if errHex != nil {
		t.Fatalf("expected valid hex string, got error: %v", errHex)
	}
	if len(rawBytes) != 16 {
		t.Fatalf("expected 16 bytes (128 bits) of entropy, got %d", len(rawBytes))
	}

	ok, errMsg := mgr.ValidateAndConsume(token)
	if !ok {
		t.Fatalf("expected valid token to be accepted, failed with: %s", errMsg)
	}
}

// Scenario B: token rotation via Regenerate() succeeds and changes token
func TestPairingManager_ScenarioB_RegenerateRotation(t *testing.T) {
	mgr, err := NewManager()
	if err != nil {
		t.Fatalf("failed to initialize pairing manager: %v", err)
	}
	tokenA := mgr.GetToken()

	tokenB, errRegen := mgr.Regenerate()
	if errRegen != nil {
		t.Fatalf("Regenerate failed: %v", errRegen)
	}
	if len(tokenB) != 32 {
		t.Fatalf("expected rotated token to have 32 hex chars, got %d", len(tokenB))
	}
	if tokenA == tokenB {
		t.Fatal("Regenerate must produce a fresh token distinct from tokenA")
	}

	// Token A should no longer validate
	okA, _ := mgr.Validate(tokenA)
	if okA {
		t.Fatal("tokenA must not validate after Regenerate")
	}

	// Token B should validate
	okB, _ := mgr.Validate(tokenB)
	if !okB {
		t.Fatal("tokenB must validate after Regenerate")
	}
}

// Scenario C: expired token is rejected
func TestPairingManager_ScenarioC_ExpiredToken(t *testing.T) {
	mgr, err := NewManager()
	if err != nil {
		t.Fatalf("failed to initialize pairing manager: %v", err)
	}
	token := mgr.GetToken()

	// Simulate expired token by rewinding expiresAt into the past
	mgr.mu.Lock()
	mgr.expiresAt = time.Now().Add(-1 * time.Second)
	mgr.mu.Unlock()

	ok, errMsg := mgr.ValidateAndConsume(token)
	if ok {
		t.Fatal("expected expired token to be rejected, but it was accepted")
	}
	if errMsg != "Pairing token has expired" {
		t.Fatalf("expected 'Pairing token has expired', got: %s", errMsg)
	}
}

// Scenario D: wrong token is rejected
func TestPairingManager_ScenarioD_WrongToken(t *testing.T) {
	mgr, err := NewManager()
	if err != nil {
		t.Fatalf("failed to initialize pairing manager: %v", err)
	}
	wrongToken := "0123456789abcdef0123456789abcdef"

	ok, errMsg := mgr.ValidateAndConsume(wrongToken)
	if ok {
		t.Fatal("expected wrong token to be rejected, but was accepted")
	}
	if errMsg != "Invalid pairing token" {
		t.Fatalf("expected 'Invalid pairing token', got: %s", errMsg)
	}
}

// Scenario E: token consumed immediately and automatically rotated
func TestPairingManager_ScenarioE_TokenConsumedAndRotated(t *testing.T) {
	mgr, err := NewManager()
	if err != nil {
		t.Fatalf("failed to initialize pairing manager: %v", err)
	}
	initialToken := mgr.GetToken()

	ok, _ := mgr.ValidateAndConsume(initialToken)
	if !ok {
		t.Fatal("initial validation should have succeeded")
	}

	// Verify that the token has been consumed and fresh token was generated
	newToken := mgr.GetToken()
	if newToken == initialToken {
		t.Fatal("new token should not equal the consumed token")
	}
	if len(newToken) != 32 {
		t.Fatalf("expected 32 hex chars for rotated token, got %d", len(newToken))
	}

	// Reuse of old token must be rejected
	okReuse, _ := mgr.ValidateAndConsume(initialToken)
	if okReuse {
		t.Fatal("reuse of consumed token must be rejected")
	}

	// New token must be unconsumed and valid
	okNew, _ := mgr.ValidateAndConsume(newToken)
	if !okNew {
		t.Fatal("new rotated token must be consumable")
	}
}

// Scenario F: concurrent pairing attempts allow exactly 1 consumption
func TestPairingManager_ScenarioF_ConcurrentPairingAttempts(t *testing.T) {
	mgr, err := NewManager()
	if err != nil {
		t.Fatalf("failed to initialize pairing manager: %v", err)
	}
	targetToken := mgr.GetToken()

	concurrency := 20
	var wg sync.WaitGroup
	var successCount int32
	var failureCount int32

	wg.Add(concurrency)
	startSig := make(chan struct{})

	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			<-startSig
			ok, _ := mgr.ValidateAndConsume(targetToken)
			if ok {
				atomic.AddInt32(&successCount, 1)
			} else {
				atomic.AddInt32(&failureCount, 1)
			}
		}()
	}

	// Release all goroutines simultaneously
	close(startSig)
	wg.Wait()

	if successCount != 1 {
		t.Fatalf("CRITICAL RACE: expected exactly 1 successful pairing, got %d", successCount)
	}
	if failureCount != int32(concurrency-1) {
		t.Fatalf("expected %d failures, got %d", concurrency-1, failureCount)
	}
}
