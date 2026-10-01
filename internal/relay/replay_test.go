package relay

import (
	"testing"
	"time"
)

func TestReplayGuard_ValidRequest(t *testing.T) {
	rg := NewReplayGuard(120)
	now := time.Now().Unix()

	ok, err := rg.ValidateAndRecord("req-1", now)
	if !ok {
		t.Fatalf("Expected valid request to pass, got err: %s", err)
	}
}

func TestReplayGuard_DuplicateRejection(t *testing.T) {
	rg := NewReplayGuard(120)
	now := time.Now().Unix()

	ok, _ := rg.ValidateAndRecord("req-duplicate", now)
	if !ok {
		t.Fatalf("First request should pass")
	}

	ok, errMsg := rg.ValidateAndRecord("req-duplicate", now)
	if ok {
		t.Fatalf("Duplicate request must be rejected")
	}
	if errMsg != "Duplicate request ID: replay attack detected" {
		t.Fatalf("Unexpected error message: %s", errMsg)
	}
}

func TestReplayGuard_ExpiredTimestamp(t *testing.T) {
	rg := NewReplayGuard(120)
	staleTime := time.Now().Unix() - 150 // 150 seconds ago, beyond 120s limit

	ok, errMsg := rg.ValidateAndRecord("req-old", staleTime)
	if ok {
		t.Fatalf("Expired request must be rejected")
	}
	if errMsg != "Event expired (stale timestamp)" {
		t.Fatalf("Unexpected error message: %s", errMsg)
	}
}

func TestReplayGuard_FutureTimestamp(t *testing.T) {
	rg := NewReplayGuard(120)
	futureTime := time.Now().Unix() + 100 // 100 seconds in future, beyond 60s tolerance

	ok, errMsg := rg.ValidateAndRecord("req-future", futureTime)
	if ok {
		t.Fatalf("Far-future request must be rejected")
	}
	if errMsg != "Event timestamp is too far in the future" {
		t.Fatalf("Unexpected error message: %s", errMsg)
	}
}
