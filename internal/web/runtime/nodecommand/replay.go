package nodecommand

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"
)

type ReplayGuard interface {
	Begin(ctx context.Context, req Request) (*Response, bool, error)
	Commit(ctx context.Context, req Request, resp Response) error
	Abort(ctx context.Context, req Request) error
}

type MemoryReplayGuard struct {
	mu       sync.Mutex
	capacity int
	ttl      time.Duration
	now      func() time.Time
	entries  map[string]*replayEntry
	order    []string
}

type replayEntry struct {
	hash      string
	expiresAt time.Time
	inflight  bool
	response  *Response
}

func NewMemoryReplayGuard(capacity int, ttl time.Duration, now func() time.Time) *MemoryReplayGuard {
	if capacity <= 0 {
		capacity = 1024
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	if now == nil {
		now = time.Now
	}
	return &MemoryReplayGuard{
		capacity: capacity,
		ttl:      ttl,
		now:      now,
		entries:  make(map[string]*replayEntry),
	}
}

func (g *MemoryReplayGuard) Begin(ctx context.Context, req Request) (*Response, bool, error) {
	if ctx == nil {
		return nil, false, ErrInvalidContext
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	hash, err := requestReplayHashErr(req)
	if err != nil {
		return nil, false, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.pruneLocked()
	key := req.IdempotencyKey
	if entry, ok := g.entries[key]; ok {
		if entry.hash != hash {
			return nil, false, ErrReplayKeyConflict
		}
		if entry.response != nil {
			resp := *entry.response
			return &resp, true, nil
		}
		if entry.inflight {
			return nil, false, ErrReplayInProgress
		}
		entry.inflight = true
		return nil, false, nil
	}
	if err := req.Validate(g.now()); err != nil {
		return nil, false, err
	}
	expiresAt := req.ExpiresAt
	ttlExpiry := g.now().Add(g.ttl)
	if expiresAt.IsZero() || ttlExpiry.Before(expiresAt) {
		expiresAt = ttlExpiry
	}
	if len(g.entries) >= g.capacity && !g.evictCompletedLocked() {
		return nil, false, ErrReplayCapacity
	}
	g.entries[key] = &replayEntry{hash: hash, expiresAt: expiresAt, inflight: true}
	g.order = append(g.order, key)
	return nil, false, nil
}

func (g *MemoryReplayGuard) Commit(ctx context.Context, req Request, resp Response) error {
	if ctx == nil {
		return ErrInvalidContext
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := resp.ValidateFor(req); err != nil {
		return err
	}
	hash, err := requestReplayHashErr(req)
	if err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	entry, ok := g.entries[req.IdempotencyKey]
	if !ok {
		return ErrReplayMissingEntry
	}
	if entry.hash != hash {
		return ErrReplayKeyConflict
	}
	entry.inflight = false
	copied := resp
	entry.response = &copied
	return nil
}

func (g *MemoryReplayGuard) Abort(ctx context.Context, req Request) error {
	if ctx == nil {
		return ErrInvalidContext
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	hash, err := requestReplayHashErr(req)
	if err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	entry, ok := g.entries[req.IdempotencyKey]
	if !ok {
		return ErrReplayMissingEntry
	}
	if entry.hash != hash {
		return ErrReplayKeyConflict
	}
	if entry.response == nil {
		delete(g.entries, req.IdempotencyKey)
		g.removeOrderLocked(req.IdempotencyKey)
	}
	return nil
}

func (g *MemoryReplayGuard) pruneLocked() {
	now := g.now()
	for key, entry := range g.entries {
		if !entry.inflight && !now.Before(entry.expiresAt) {
			delete(g.entries, key)
			g.removeOrderLocked(key)
		}
	}
}

func (g *MemoryReplayGuard) evictCompletedLocked() bool {
	for len(g.entries) >= g.capacity && len(g.order) > 0 {
		key := g.order[0]
		g.order = g.order[1:]
		entry, ok := g.entries[key]
		if !ok {
			continue
		}
		if entry.inflight {
			g.order = append(g.order, key)
			if allEntriesInflight(g.entries) {
				return false
			}
			continue
		}
		delete(g.entries, key)
	}
	return len(g.entries) < g.capacity
}

func (g *MemoryReplayGuard) removeOrderLocked(key string) {
	for i, existing := range g.order {
		if existing == key {
			copy(g.order[i:], g.order[i+1:])
			g.order = g.order[:len(g.order)-1]
			return
		}
	}
}

func allEntriesInflight(entries map[string]*replayEntry) bool {
	for _, entry := range entries {
		if !entry.inflight {
			return false
		}
	}
	return true
}

func requestReplayHashErr(req Request) (string, error) {
	if err := validateSecretInput(req.SecretInput); err != nil {
		return "", err
	}
	wire, err := req.MarshalJSON()
	if err != nil {
		return "", err
	}
	canonical := struct {
		Wire              json.RawMessage `json:"wire"`
		SecretInputDigest string          `json:"secretInputDigest"`
	}{
		Wire:              json.RawMessage(wire),
		SecretInputDigest: secretInputDigest(req.SecretInput),
	}
	raw, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
