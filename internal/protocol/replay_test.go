package protocol

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// Scenario A: normal command -> accepted
func TestReplayGuard_ScenarioA_NormalCommand(t *testing.T) {
	rg := NewReplayGuard(120)
	now := time.Now().Unix()
	pubkey := "aaaa1111bbbb2222cccc3333dddd4444eeee5555ffff6666aaaa1111bbbb2222"

	ok, errMsg := rg.ValidateAndRecordScoped(pubkey, "req-1", now, now)
	if !ok {
		t.Fatalf("Expected normal command to pass, got error: %s", errMsg)
	}
}

// Scenario B: exact replay -> rejected
func TestReplayGuard_ScenarioB_ExactReplay(t *testing.T) {
	rg := NewReplayGuard(120)
	now := time.Now().Unix()
	pubkey := "aaaa1111bbbb2222cccc3333dddd4444eeee5555ffff6666aaaa1111bbbb2222"

	ok1, _ := rg.ValidateAndRecordScoped(pubkey, "req-dup", now, now)
	if !ok1 {
		t.Fatal("First request should pass")
	}

	ok2, errMsg := rg.ValidateAndRecordScoped(pubkey, "req-dup", now, now)
	if ok2 {
		t.Fatal("Exact replay must be rejected")
	}
	if errMsg != "Duplicate request ID: replay attack detected" {
		t.Fatalf("Unexpected error message: %s", errMsg)
	}
}

// Scenario C: same request ID from different sender -> accepted
func TestReplayGuard_ScenarioC_SameRequestIDDifferentSender(t *testing.T) {
	rg := NewReplayGuard(120)
	now := time.Now().Unix()
	sender1 := "1111111111111111111111111111111111111111111111111111111111111111"
	sender2 := "2222222222222222222222222222222222222222222222222222222222222222"

	ok1, _ := rg.ValidateAndRecordScoped(sender1, "req-shared-id", now, now)
	if !ok1 {
		t.Fatal("First sender should pass")
	}

	// Sender 2 uses same request ID -> must NOT be blocked by sender 1's request ID
	ok2, errMsg := rg.ValidateAndRecordScoped(sender2, "req-shared-id", now, now)
	if !ok2 {
		t.Fatalf("Same request ID from different sender should be accepted, got error: %s", errMsg)
	}
}

// Scenario D: stale event -> rejected
func TestReplayGuard_ScenarioD_StaleEvent(t *testing.T) {
	rg := NewReplayGuard(120)
	staleTime := time.Now().Unix() - 150 // 150 seconds ago (> 120s max age)
	sender := "sender-d"

	ok, errMsg := rg.ValidateAndRecordScoped(sender, "req-stale", staleTime, staleTime)
	if ok {
		t.Fatal("Stale event must be rejected")
	}
	if errMsg != "Event expired (stale timestamp)" {
		t.Fatalf("Expected stale timestamp error, got: %s", errMsg)
	}
}

// Scenario E: future event -> rejected
func TestReplayGuard_ScenarioE_FutureEvent(t *testing.T) {
	rg := NewReplayGuard(120)
	futureTime := time.Now().Unix() + 60 // 60 seconds into future (> 30s max tolerance)
	sender := "sender-e"

	ok, errMsg := rg.ValidateAndRecordScoped(sender, "req-future", futureTime, futureTime)
	if ok {
		t.Fatal("Far-future event must be rejected")
	}
	if errMsg != "Event timestamp is too far in the future" {
		t.Fatalf("Expected future timestamp error, got: %s", errMsg)
	}
}

// Scenario F: mismatched inner timestamp vs event timestamp -> rejected
func TestReplayGuard_ScenarioF_MismatchedInnerVsEventTimestamp(t *testing.T) {
	rg := NewReplayGuard(120)
	now := time.Now().Unix()
	eventTime := now
	innerTime := now - 45 // 45s difference (> 30s tolerance)
	sender := "sender-f"

	ok, errMsg := rg.ValidateAndRecordScoped(sender, "req-mismatch", eventTime, innerTime)
	if ok {
		t.Fatal("Mismatched inner vs event timestamp must be rejected")
	}
	if errMsg == "" {
		t.Fatal("Expected error message for mismatched timestamps")
	}
	t.Logf("Got expected error: %s", errMsg)
}

// Scenario G: large number of replay entries (bounded memory)
func TestReplayGuard_ScenarioG_BoundedMemory(t *testing.T) {
	rg := NewReplayGuard(120)
	smallCap := 50
	rg.SetCapacity(smallCap)
	now := time.Now().Unix()
	sender := "sender-g"

	// Insert 200 entries into a cache of capacity 50
	for i := 0; i < 200; i++ {
		reqID := fmt.Sprintf("req-flood-%d", i)
		ok, _ := rg.ValidateAndRecordScoped(sender, reqID, now, now)
		if !ok {
			t.Fatalf("Insertion %d failed", i)
		}
	}

	count := rg.EntryCount()
	if count > smallCap {
		t.Fatalf("Cache exceeded capacity limit! Expected <= %d, got %d", smallCap, count)
	}
	t.Logf("Bounded memory verified: 200 entries inserted, cache held to %d items", count)
}

// Scenario H: concurrent replay checks -> thread safety
func TestReplayGuard_ScenarioH_ConcurrentReplayChecks(t *testing.T) {
	rg := NewReplayGuard(120)
	now := time.Now().Unix()
	concurrency := 30
	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := 0; i < concurrency; i++ {
		sender := fmt.Sprintf("sender-h-%d", i)
		go func(s string) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				reqID := fmt.Sprintf("req-%d", j)
				rg.ValidateAndRecordScoped(s, reqID, now, now)
			}
		}(sender)
	}

	wg.Wait()
}
