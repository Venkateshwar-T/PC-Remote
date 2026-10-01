package relay

import (
	"sync"
	"time"
)

const (
	DefaultMaxSkewSeconds = 120 // Max acceptable age of an event in seconds
	MaxFutureSkewSeconds  = 60  // Max clock drift into future in seconds
	DefaultCacheCapacity  = 2048
)

type ReplayGuard struct {
	mu             sync.Mutex
	seenIDs        map[string]time.Time
	maxAge         time.Duration
	maxSkewSeconds int64
}

// NewReplayGuard creates a thread-safe replay protection engine
func NewReplayGuard(maxSkewSeconds int64) *ReplayGuard {
	if maxSkewSeconds <= 0 {
		maxSkewSeconds = DefaultMaxSkewSeconds
	}
	return &ReplayGuard{
		seenIDs:        make(map[string]time.Time),
		maxAge:         time.Duration(maxSkewSeconds*2) * time.Second,
		maxSkewSeconds: maxSkewSeconds,
	}
}

// ValidateAndRecord checks timestamp freshness, detects replays, and records valid IDs.
// Returns (ok, errMsg).
func (rg *ReplayGuard) ValidateAndRecord(id string, eventTimestamp int64) (bool, string) {
	if id == "" {
		return false, "Missing request ID"
	}

	now := time.Now().Unix()
	skew := now - eventTimestamp

	// Reject events from the future (beyond clock drift allowance)
	if skew < -MaxFutureSkewSeconds {
		return false, "Event timestamp is too far in the future"
	}

	// Reject expired events
	if skew > rg.maxSkewSeconds {
		return false, "Event expired (stale timestamp)"
	}

	rg.mu.Lock()
	defer rg.mu.Unlock()

	// Check for replay
	if exp, exists := rg.seenIDs[id]; exists {
		if time.Now().Before(exp) {
			return false, "Duplicate request ID: replay attack detected"
		}
	}

	// Prune if cache gets large
	if len(rg.seenIDs) >= DefaultCacheCapacity {
		rg.pruneLocked()
	}

	// Record ID with expiration
	rg.seenIDs[id] = time.Now().Add(rg.maxAge)
	return true, ""
}

func (rg *ReplayGuard) pruneLocked() {
	now := time.Now()
	for id, exp := range rg.seenIDs {
		if now.After(exp) {
			delete(rg.seenIDs, id)
		}
	}
}
