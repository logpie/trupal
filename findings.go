package main

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// BrainFinding represents a single finding produced by the brain.
type BrainFinding struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Severity  string    `json:"severity"` // "warn" or "error"
	Nudge     string    `json:"nudge"`
	Reasoning string    `json:"reasoning"`
	Status    string    `json:"status"` // "new" / "shown" / "resolved" / "waived"
}

// FindingStore is a mutex-protected store of BrainFindings.
type FindingStore struct {
	mu       sync.Mutex
	findings []BrainFinding
	nextID   int
}

// NewFindingStore returns an empty FindingStore.
func NewFindingStore() *FindingStore {
	return &FindingStore{}
}

// Add creates a new finding with an auto-incremented ID, status "shown", and timestamp now.
// Returns the new finding's ID.
func (fs *FindingStore) Add(severity, nudge, reasoning string) string {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	fs.nextID++
	id := fmt.Sprintf("f-%03d", fs.nextID)
	fs.findings = append(fs.findings, BrainFinding{
		ID:        id,
		Timestamp: time.Now(),
		Severity:  severity,
		Nudge:     nudge,
		Reasoning: reasoning,
		Status:    "shown",
	})
	return id
}

func (fs *FindingStore) Count() (active, resolved int) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	for _, f := range fs.findings {
		switch f.Status {
		case "shown":
			active++
		case "resolved":
			resolved++
		}
	}
	return
}

func (fs *FindingStore) GetByID(id string) *BrainFinding {
	for _, f := range fs.findings {
		if f.ID == id {
			return &f
		}
	}
	return nil
}

func (fs *FindingStore) RemoveOlderThan(age time.Duration) int {
	cutoff := time.Now().Add(-age)
	removed := 0
	var keep []BrainFinding
	for _, f := range fs.findings {
		if f.Timestamp.Before(cutoff) {
			removed++
		} else {
			keep = append(keep, f)
		}
	}
	fs.findings = keep
	return removed
}

func (fs *FindingStore) Export() []byte {
	data, _ := json.Marshal(fs.findings)
	return data
}

func (fs *FindingStore) Import(data []byte) {
	var imported []BrainFinding
	json.Unmarshal(data, &imported)
	fs.findings = append(fs.findings, imported...)
}

func (fs *FindingStore) Clear() {
	fs.mu.Lock()
	fs.findings = nil
	fs.nextID = 0
	fs.mu.Unlock()
}

// Resolve marks matching findings as "resolved" if they are currently "shown".
func (fs *FindingStore) Resolve(ids []string) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	for i := range fs.findings {
		if _, ok := set[fs.findings[i].ID]; ok && fs.findings[i].Status == "shown" {
			fs.findings[i].Status = "resolved"
		}
	}
}

// Active returns all findings with status "shown".
func (fs *FindingStore) Active() []BrainFinding {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	var out []BrainFinding
	for _, f := range fs.findings {
		if f.Status == "shown" {
			out = append(out, f)
		}
	}
	return out
}

// activeJSON is the internal (unlocked) version used by ActiveJSON.
type activeJSONEntry struct {
	ID       string `json:"id"`
	Severity string `json:"severity"`
	Nudge    string `json:"nudge"`
}

// ActiveJSON returns active findings as a compact JSON array of {id, nudge} objects
// suitable for inclusion in a brain prompt. Returns "[]" when there are no active findings.
func (fs *FindingStore) ActiveJSON() string {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	var entries []activeJSONEntry
	for _, f := range fs.findings {
		if f.Status == "shown" {
			entries = append(entries, activeJSONEntry{
				ID:       f.ID,
				Severity: f.Severity,
				Nudge:    f.Nudge,
			})
		}
	}
	if len(entries) == 0 {
		return "[]"
	}
	b, err := json.Marshal(entries)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// Recent returns the last n findings regardless of status, for display purposes.
func (fs *FindingStore) Recent(n int) []BrainFinding {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	total := len(fs.findings)
	if n <= 0 || total == 0 {
		return nil
	}
	start := total - n
	if start < 0 {
		start = 0
	}
	out := make([]BrainFinding, total-start)
	copy(out, fs.findings[start:])
	return out
}
