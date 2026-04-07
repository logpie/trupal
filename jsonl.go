package main

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// JSONLEntry represents a parsed JSONL line from CC's session file.
type JSONLEntry struct {
	Type      string          `json:"type"`      // "user", "assistant", "attachment", etc.
	Timestamp string          `json:"timestamp"`
	SessionID string          `json:"sessionId"`
	Message   json.RawMessage `json:"message"`
	// Parsed from message:
	Role    string // "user" or "assistant"
	HasText bool   // assistant message contains a text block
	HasTool bool   // assistant message contains a tool_use block
}

// JSONLWatcher watches CC's session JSONL file for new entries.
type JSONLWatcher struct {
	Path       string
	offset     int64
	fsWatcher  *fsnotify.Watcher
	Events     chan struct{} // signals that new entries are available
	lastMtime  time.Time
	hotMode    bool
	lastActive time.Time
}

// FindSessionJSONL locates the most recently modified CC session JSONL file
// for the given project directory.
func FindSessionJSONL(projectDir string) string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	// CC encodes project path: /home/user/work/project → -home-user-work-project
	encoded := strings.ReplaceAll(projectDir, string(os.PathSeparator), "-")
	if strings.HasPrefix(encoded, "-") {
		// keep the leading dash
	}

	// Check both legacy and XDG paths.
	candidates := []string{
		filepath.Join(homeDir, ".claude", "projects", encoded),
		filepath.Join(homeDir, ".config", "claude", "projects", encoded),
	}

	var bestFile string
	var bestTime time.Time

	for _, dir := range candidates {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			if info.ModTime().After(bestTime) {
				bestTime = info.ModTime()
				bestFile = filepath.Join(dir, e.Name())
			}
		}
	}

	return bestFile
}

// NewJSONLWatcher creates a watcher for the given JSONL file.
// Starts at the end of the file (only watches new entries).
func NewJSONLWatcher(path string) (*JSONLWatcher, error) {
	// Start at end of file — we only care about new activity.
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	// Watch the directory (not the file) to handle file replacements.
	dir := filepath.Dir(path)
	if err := fsw.Add(dir); err != nil {
		fsw.Close()
		return nil, err
	}

	w := &JSONLWatcher{
		Path:       path,
		offset:     info.Size(),
		fsWatcher:  fsw,
		Events:     make(chan struct{}, 1),
		lastActive: time.Now(),
		hotMode:    true,
	}

	go w.watchLoop()

	return w, nil
}

// watchLoop reads fsnotify events and signals when the JSONL file changes.
func (w *JSONLWatcher) watchLoop() {
	for {
		select {
		case event, ok := <-w.fsWatcher.Events:
			if !ok {
				return
			}
			base := filepath.Base(event.Name)
			if base != filepath.Base(w.Path) {
				continue
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) {
				w.lastActive = time.Now()
				// Signal only — offset resets are handled in ReadNew() to avoid races.
				select {
				case w.Events <- struct{}{}:
				default:
				}
			}
		case _, ok := <-w.fsWatcher.Errors:
			if !ok {
				return
			}
		}
	}
}

// ReadNew reads all new JSONL entries since the last read.
func (w *JSONLWatcher) ReadNew() []JSONLEntry {
	f, err := os.Open(w.Path)
	if err != nil {
		return nil
	}
	defer f.Close()

	// Reset offset if file was truncated.
	info, err := f.Stat()
	if err != nil {
		return nil
	}
	if info.Size() < w.offset {
		w.offset = 0
	}

	if _, err := f.Seek(w.offset, io.SeekStart); err != nil {
		return nil
	}

	var entries []JSONLEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB lines

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var entry JSONLEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}

		// Parse message role and content types.
		classifyEntry(&entry)
		entries = append(entries, entry)
	}

	// Only advance offset if scanner succeeded.
	if scanner.Err() != nil {
		Debugf("[jsonl] scanner error: %v", scanner.Err())
		return entries // don't advance offset — retry next time
	}

	pos, _ := f.Seek(0, io.SeekCurrent)
	w.offset = pos

	return entries
}

// classifyEntry extracts role and content type flags from the raw message.
func classifyEntry(e *JSONLEntry) {
	if e.Message == nil {
		return
	}

	var msg struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(e.Message, &msg); err != nil {
		return
	}
	e.Role = msg.Role

	// Check content for text/tool_use blocks.
	var blocks []struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		// Content might be a string (user messages).
		return
	}
	for _, b := range blocks {
		if b.Type == "text" {
			e.HasText = true
		}
		if b.Type == "tool_use" {
			e.HasTool = true
		}
	}
}

// DetectCCStatus returns the current CC status: "active", "thinking", or "idle".
func DetectCCStatus(jsonlPath string) string {
	// Fast path: check mtime.
	info, err := os.Stat(jsonlPath)
	if err != nil {
		return "idle"
	}
	if time.Since(info.ModTime()) < 30*time.Second {
		return "active"
	}

	// Check sub-agent files.
	sessionDir := strings.TrimSuffix(jsonlPath, filepath.Ext(jsonlPath))
	subagentDir := filepath.Join(sessionDir, "subagents")
	if entries, err := os.ReadDir(subagentDir); err == nil {
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".jsonl") {
				if info, err := e.Info(); err == nil {
					if time.Since(info.ModTime()) < 30*time.Second {
						return "active"
					}
				}
			}
		}
	}

	// Slow path: scan last entry in JSONL.
	lastType := scanLastEntryType(jsonlPath)
	switch lastType {
	case "user":
		return "thinking"
	case "assistant":
		return "idle"
	default:
		return "idle"
	}
}

// scanLastEntryType reads the last ~64KB of the JSONL file and returns the
// type of the last user/assistant entry.
func scanLastEntryType(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return ""
	}

	// Read last 64KB.
	readSize := int64(64 * 1024)
	offset := info.Size() - readSize
	if offset < 0 {
		offset = 0
	}
	f.Seek(offset, io.SeekStart)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	var lastType string
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(line, &entry) == nil {
			if entry.Type == "user" || entry.Type == "assistant" {
				lastType = entry.Type
			}
		}
	}
	return lastType
}

// CheckForNewSession checks if a newer session file exists and returns its path.
// Returns "" if the current session is still the most recent.
func CheckForNewSession(projectDir, currentPath string) string {
	newPath := FindSessionJSONL(projectDir)
	if newPath != "" && newPath != currentPath {
		return newPath
	}
	return ""
}

// Close stops the JSONL watcher.
func (w *JSONLWatcher) Close() {
	w.fsWatcher.Close()
}

// SubagentFiles returns paths to any active sub-agent JSONL files.
func SubagentFiles(jsonlPath string) []string {
	sessionID := strings.TrimSuffix(filepath.Base(jsonlPath), ".jsonl")
	dir := filepath.Join(filepath.Dir(jsonlPath), sessionID, "subagents")

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var files []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".jsonl") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}

	sort.Slice(files, func(i, j int) bool {
		iInfo, _ := os.Stat(files[i])
		jInfo, _ := os.Stat(files[j])
		if iInfo == nil || jInfo == nil {
			return false
		}
		return iInfo.ModTime().After(jInfo.ModTime())
	})

	return files
}
