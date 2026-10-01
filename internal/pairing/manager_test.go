package pairing

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPairingManager_ScenarioA_ValidToken(t *testing.T) {
	mgr := NewManager()
	token := mgr.GetToken()

	if len(token) != 32 {
		t.Fatalf("expected 32 hex chars (128 bits), got %d (%s)", len(token), token)
	}

	ok, errMsg := mgr.ValidateAndConsume(token)
	if !ok {
		t.Fatalf("expected valid token to be accepted, failed with: %s", errMsg)
	}
}

func TestPairingManager_ScenarioB_ExpiredToken(t *testing.T) {
	mgr := NewManager()
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

func TestPairingManager_ScenarioC_WrongToken(t *testing.T) {
	mgr := NewManager()
	wrongToken := "0123456789abcdef0123456789abcdef"

	ok, errMsg := mgr.ValidateAndConsume(wrongToken)
	if ok {
		t.Fatal("expected wrong token to be rejected, but was accepted")
	}
	if errMsg != "Invalid pairing token" {
		t.Fatalf("expected 'Invalid pairing token', got: %s", errMsg)
	}
}

func TestPairingManager_ScenarioD_TokenConsumedImmediately(t *testing.T) {
	mgr := NewManager()
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
}

func TestPairingManager_ScenarioE_ReuseOfConsumedTokenRejected(t *testing.T) {
	mgr := NewManager()
	token := mgr.GetToken()

	// First use: must succeed
	ok1, _ := mgr.ValidateAndConsume(token)
	if !ok1 {
		t.Fatal("first consumption should succeed")
	}

	// Second use with same token: MUST fail
	ok2, errMsg2 := mgr.ValidateAndConsume(token)
	if ok2 {
		t.Fatal("reuse of consumed token must be rejected")
	}
	if errMsg2 != "Invalid pairing token" {
		t.Logf("Got expected rejection: %s", errMsg2)
	}
}

func TestPairingManager_ScenarioF_ConcurrentPairingAttempts(t *testing.T) {
	mgr := NewManager()
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
