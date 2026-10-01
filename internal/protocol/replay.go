package protocol

import (
	"fmt"
	"math"
	"sync"
	"time"
)

const (
	DefaultMaxSkewSeconds      = 120 // Max acceptable age of an event in seconds
	MaxFutureSkewSeconds       = 30  // Max clock drift into future in seconds
	MaxInnerEventSkewTolerance = 30  // Max difference between event.CreatedAt and inner timestamp in seconds
	DefaultCacheCapacity       = 2048
)

type ReplayGuard struct {
	mu             sync.Mutex
	seenEntries    map[string]time.Time // key: senderPubKey + ":" + reqID -> expiresAt
	maxAge         time.Duration
	maxSkewSeconds int64
	capacity       int
}

// NewReplayGuard creates a thread-safe sender-scoped replay protection engine
func NewReplayGuard(maxSkewSeconds int64) *ReplayGuard {
	if maxSkewSeconds <= 0 {
		maxSkewSeconds = DefaultMaxSkewSeconds
	}
	return &ReplayGuard{
		seenEntries:    make(map[string]time.Time),
		maxAge:         time.Duration(maxSkewSeconds*2) * time.Second,
		maxSkewSeconds: maxSkewSeconds,
		capacity:       DefaultCacheCapacity,
	}
}

// SetCapacity sets the maximum capacity for entries in the replay cache (useful for bounded memory testing).
func (rg *ReplayGuard) SetCapacity(cap int) {
	rg.mu.Lock()
	defer rg.mu.Unlock()
	if cap > 0 {
		rg.capacity = cap
	}
}

// EntryCount returns the current number of tracked entries.
func (rg *ReplayGuard) EntryCount() int {
	rg.mu.Lock()
	defer rg.mu.Unlock()
	return len(rg.seenEntries)
}

// ValidateAndRecordScoped checks timestamp freshness, event vs inner skew, detects per-sender replays,
// and records valid IDs in a strictly bounded memory store.
func (rg *ReplayGuard) ValidateAndRecordScoped(senderPubKey, id string, eventTimestamp int64, innerTimestamp int64) (bool, string) {
	if id == "" {
		return false, "Missing request ID"
	}
	if senderPubKey == "" {
		return false, "Missing sender public key"
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

	// Reject mismatched inner timestamp vs Nostr event timestamp
	if innerTimestamp > 0 {
		diff := int64(math.Abs(float64(eventTimestamp - innerTimestamp)))
		if diff > MaxInnerEventSkewTolerance {
			return false, fmt.Sprintf("Mismatched inner timestamp (%d) vs event timestamp (%d): exceeds %ds tolerance", innerTimestamp, eventTimestamp, MaxInnerEventSkewTolerance)
		}
	}

	compositeKey := senderPubKey + ":" + id

	rg.mu.Lock()
	defer rg.mu.Unlock()

	// Check for replay scoped to sender
	if exp, exists := rg.seenEntries[compositeKey]; exists {
		if time.Now().Before(exp) {
			return false, "Duplicate request ID: replay attack detected"
		}
	}

	// Bound memory: prune expired items when capacity is reached
	if len(rg.seenEntries) >= rg.capacity {
		rg.pruneLocked()
		// If still at capacity after pruning expired items, evict oldest entries
		if len(rg.seenEntries) >= rg.capacity {
			rg.evictBatchLocked(rg.capacity / 4)
		}
	}

	// Record ID with expiration
	rg.seenEntries[compositeKey] = time.Now().Add(rg.maxAge)
	return true, ""
}

func (rg *ReplayGuard) pruneLocked() {
	now := time.Now()
	for key, exp := range rg.seenEntries {
		if now.After(exp) {
			delete(rg.seenEntries, key)
		}
	}
}

func (rg *ReplayGuard) evictBatchLocked(count int) {
	if count <= 0 {
		count = 1
	}
	evicted := 0
	for key := range rg.seenEntries {
		delete(rg.seenEntries, key)
		evicted++
		if evicted >= count {
			break
		}
	}
}
