package main

import (
	"fmt"
	"hash/fnv"
	"time"
)

// Session tracks state across polling cycles.
type Session struct {
	StartTime     time.Time
	ProjectDir    string
	FileEditCount map[string]int    // filename -> number of cycles where diff content changed
	ErrorHistory  []int             // build error count per cycle (append-only)
	LastDiffHash  map[string]uint64 // filename -> fnv hash of diff content
}

// NewSession initializes a new Session for the given project directory.
func NewSession(projectDir string) *Session {
	return &Session{
		StartTime:     time.Now(),
		ProjectDir:    projectDir,
		FileEditCount: make(map[string]int),
		ErrorHistory:  []int{},
		LastDiffHash:  make(map[string]uint64),
	}
}

// UpdateFileEdits hashes each file's diff content and increments FileEditCount
// when the content has changed. Files no longer in the diff are cleaned up.
func (s *Session) UpdateFileEdits(fileDiffs map[string]string) {
	for filename, diff := range fileDiffs {
		h := fnvHash(diff)
		prev, exists := s.LastDiffHash[filename]
		if !exists || h != prev {
			s.FileEditCount[filename]++
			s.LastDiffHash[filename] = h
		}
	}

	// Clean up files no longer in the diff (committed or reset).
	for filename := range s.LastDiffHash {
		if _, ok := fileDiffs[filename]; !ok {
			delete(s.LastDiffHash, filename)
			delete(s.FileEditCount, filename)
		}
	}
}

// AppendErrorCount appends the error count for the current cycle.
func (s *Session) AppendErrorCount(count int) {
	s.ErrorHistory = append(s.ErrorHistory, count)
}

// Finding is a trajectory evaluation result.
type Finding struct {
	Level   string // "warn" or "error"
	Message string
}

// EvalTrajectory checks the session state for known anti-patterns and returns
// any findings.
func (s *Session) EvalTrajectory() []Finding {
	var findings []Finding

	// Whack-a-mole: a single file has been edited >= 3 cycles.
	for filename, count := range s.FileEditCount {
		if count >= 3 {
			findings = append(findings, Finding{
				Level:   "warn",
				Message: fmt.Sprintf("whack-a-mole: %s edited in %d cycles", filename, count),
			})
		}
	}

	// Error trend analysis (need at least 3 data points).
	// Note: stall and fix-then-break are shown in the build trend line (buildTrend in watcher.go).
	// Here we detect trending upward (monotonically non-decreasing with at least one increase).
	history := s.ErrorHistory
	if len(history) >= 3 {
		recent := history
		if len(recent) > 10 {
			recent = recent[len(recent)-10:]
		}
		last := recent[len(recent)-1]
		if last > 0 && len(recent) >= 3 {
			// Trending upward: last 3+ entries are non-decreasing with at least one increase.
			tail := recent[len(recent)-3:]
			if tail[0] <= tail[1] && tail[1] <= tail[2] && tail[2] > tail[0] {
				findings = append(findings, Finding{
					Level:   "warn",
					Message: fmt.Sprintf("error count trending up: %d -> %d -> %d", tail[0], tail[1], tail[2]),
				})
			}
		}
	}

	return findings
}

// Elapsed returns a human-readable string of the session duration.
func (s *Session) Elapsed() string {
	d := time.Since(s.StartTime)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	sec := int(d.Seconds()) % 60

	if h > 0 {
		if m > 0 {
			return fmt.Sprintf("%dh%dm", h, m)
		}
		return fmt.Sprintf("%dh", h)
	}
	if m > 0 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%ds", sec)
}

// fnvHash returns the FNV-64a hash of s.
func fnvHash(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}
