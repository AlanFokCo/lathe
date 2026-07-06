// Package subagent tracks the lifecycle of Task-tool subagents so the TUI /agents
// slash and future observability can report what the parent has dispatched
// without dumping the subagent's transcript. Kept in its own package so agent
// (producer) and tui (consumer) can share the type without introducing a
// package cycle (M7e).
package subagent

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// SubagentInfo is a snapshot of one Task-tool dispatch.
type SubagentInfo struct {
	ID          string
	Description string
	Status      string // "running", "completed", "error"
	OutputBytes int
	StartedAt   time.Time
	Duration    time.Duration
}

// Tracker records subagent lifecycle events. Safe for concurrent use.
type Tracker struct {
	mu    sync.Mutex
	items []SubagentInfo
}

// NewTracker returns an empty Tracker.
func NewTracker() *Tracker { return &Tracker{} }

// Start records a new subagent dispatch and returns its id. Description is a
// short human-readable label (the Task tool's "description" arg).
func (t *Tracker) Start(description string) string {
	id := genID()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.items = append(t.items, SubagentInfo{
		ID:          id,
		Description: description,
		Status:      "running",
		StartedAt:   time.Now(),
	})
	return id
}

// Complete marks the subagent identified by id as finished with the given
// status ("completed" or "error") and output size. No-op for unknown ids.
func (t *Tracker) Complete(id, status string, outputBytes int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := range t.items {
		if t.items[i].ID == id {
			t.items[i].Status = status
			t.items[i].OutputBytes = outputBytes
			t.items[i].Duration = time.Since(t.items[i].StartedAt)
			return
		}
	}
}

// List returns a snapshot copy of all recorded subagents (oldest first).
func (t *Tracker) List() []SubagentInfo {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.items) == 0 {
		return nil
	}
	out := make([]SubagentInfo, len(t.items))
	copy(out, t.items)
	return out
}

func genID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b)
}
