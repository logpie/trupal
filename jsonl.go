package main

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// JSONLEntry represents a parsed JSONL line from CC's session file.
type JSONLEntry struct {
	Type      string          `json:"type"` // "user", "assistant", "attachment", etc.
	Timestamp string          `json:"timestamp"`
	SessionID string          `json:"sessionId"`
	Message   json.RawMessage `json:"message"`
	// Parsed from message:
	Role        string   // "user" or "assistant"
	HasText     bool     // message contains a text block
	HasTool     bool     // assistant message contains a tool_use block
	ToolNames   []string // names of tools used (e.g. "Edit", "Bash", "Write")
	ToolFiles   []string // aligned with ToolNames; empty when the tool has no file path
	ToolDetails []string // aligned with ToolNames; human-readable details (description/command/file)
	TextSnip    string   // first 200 chars of text content
}

// JSONLWatcher watches CC's session JSONL file for new entries.
type JSONLWatcher struct {
	Provider   string
	Path       string
	offset     int64
	fsWatcher  *fsnotify.Watcher
	Events     chan struct{} // signals that new entries are available
	lastMtime  time.Time
	hotMode    bool
	lastActive time.Time
}

var applyPatchFilePattern = regexp.MustCompile(`(?m)^\*\*\* (?:Add|Update|Delete) File: (.+)$`)

// FindSessionJSONL locates the most recently modified Claude Code session JSONL
// file for the given project directory.
func FindSessionJSONL(projectDir string) string {
	return FindSessionJSONLForProvider(projectDir, ProviderClaude)
}

// FindSessionJSONLForProvider locates the most recently modified session JSONL
// file for the given provider and project directory.
func FindSessionJSONLForProvider(projectDir, provider string) string {
	switch normalizeProvider(provider, ProviderClaude) {
	case ProviderCodex:
		return findCodexSessionJSONL(projectDir)
	default:
		return findClaudeSessionJSONL(projectDir)
	}
}

func findClaudeSessionJSONL(projectDir string) string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	var bestFile string
	var bestTime time.Time

	for _, targetDir := range sessionSearchDirs(projectDir) {
		encoded := strings.ReplaceAll(targetDir, string(os.PathSeparator), "-")
		candidates := []string{
			filepath.Join(homeDir, ".claude", "projects", encoded),
			filepath.Join(homeDir, ".config", "claude", "projects", encoded),
		}

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
	}

	return bestFile
}

func findCodexSessionJSONL(projectDir string) string {
	sessionsRoot := filepath.Join(codexHomeDir(), "sessions")
	var bestFile string
	var bestTime time.Time
	targetDirs := sessionSearchDirs(projectDir)

	_ = filepath.WalkDir(sessionsRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}

		cwd, ok := codexSessionCWD(path)
		if !ok {
			return nil
		}
		if !codexSessionMatchesTargets(cwd, targetDirs) {
			return nil
		}

		info, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		if info.ModTime().After(bestTime) {
			bestTime = info.ModTime()
			bestFile = path
		}
		return nil
	})

	return bestFile
}

func codexHomeDir() string {
	if override := strings.TrimSpace(os.Getenv("CODEX_HOME")); override != "" {
		return override
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(homeDir, ".codex")
}

func codexSessionMatchesTargets(cwd string, targets []string) bool {
	cwd = filepath.Clean(strings.TrimSpace(cwd))
	if cwd == "" {
		return false
	}
	cwdRoot, err := findGitRoot(cwd)
	if err != nil {
		cwdRoot = cwd
	}
	for _, target := range targets {
		target = filepath.Clean(strings.TrimSpace(target))
		if target == "" {
			continue
		}
		targetRoot, err := findGitRoot(target)
		if err != nil {
			targetRoot = target
		}
		if cwd == target || cwd == targetRoot || cwdRoot == target || cwdRoot == targetRoot {
			return true
		}
	}
	return false
}

func sessionSearchDirs(projectDir string) []string {
	projectDir = filepath.Clean(projectDir)
	dirs := []string{projectDir}
	if gitRoot, err := findGitRoot(projectDir); err == nil && gitRoot != projectDir {
		dirs = append(dirs, gitRoot)
	}
	return dirs
}

// NewJSONLWatcher creates a watcher for the given JSONL file.
// Starts at the end of the file (only watches new entries).
func NewJSONLWatcher(path string) (*JSONLWatcher, error) {
	return NewJSONLWatcherForProvider(path, ProviderClaude)
}

// NewJSONLWatcherForProvider creates a watcher for the given provider-specific
// session JSONL file. Starts at the end of the file (only watches new entries).
func NewJSONLWatcherForProvider(path, provider string) (*JSONLWatcher, error) {
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
		Provider:   normalizeProvider(provider, ProviderClaude),
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

	bytesRead := int64(0)
	for scanner.Scan() {
		line := scanner.Bytes()
		bytesRead += int64(len(line)) + 1 // +1 for newline
		if len(line) == 0 {
			continue
		}

		if entry, ok := parseJSONLEntryForProvider(w.Provider, line); ok {
			entries = append(entries, entry)
		}
	}

	// Only advance offset if scanner succeeded.
	if scanner.Err() != nil {
		Debugf("[jsonl] scanner error: %v", scanner.Err())
		return entries // don't advance offset — retry next time
	}

	w.offset += bytesRead

	return entries
}

// ReadRecentJSONLEntries parses the file and returns the last maxEntries parsed
// entries. This is used when attaching to an existing session so TruPal can
// seed recent context instead of starting with an empty view at EOF.
func ReadRecentJSONLEntries(path string, maxEntries int) []JSONLEntry {
	return ReadRecentJSONLEntriesForProvider(path, maxEntries, ProviderClaude)
}

func ReadRecentJSONLEntriesForProvider(path string, maxEntries int, provider string) []JSONLEntry {
	if maxEntries <= 0 {
		return nil
	}

	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	entries := make([]JSONLEntry, 0, maxEntries)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		entry, ok := parseJSONLEntryForProvider(provider, line)
		if !ok {
			continue
		}

		if len(entries) == maxEntries {
			copy(entries, entries[1:])
			entries[len(entries)-1] = entry
			continue
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		Debugf("[jsonl] recent scanner error: %v", err)
	}

	return entries
}

func parseJSONLEntryForProvider(provider string, line []byte) (JSONLEntry, bool) {
	switch normalizeProvider(provider, ProviderClaude) {
	case ProviderCodex:
		return parseCodexEntry(line)
	default:
		var entry JSONLEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			return JSONLEntry{}, false
		}
		classifyEntry(&entry)
		return entry, true
	}
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

	var textContent string
	if err := json.Unmarshal(msg.Content, &textContent); err == nil {
		if textContent != "" {
			e.HasText = true
			e.TextSnip = truncateText(textContent, 200)
		}
		return
	}

	// Check content for text/tool_use blocks with details.
	var blocks []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		// Content might be a string (user messages).
		return
	}
	for _, b := range blocks {
		if b.Type == "text" {
			e.HasText = true
			if e.TextSnip == "" && len(b.Text) > 0 {
				e.TextSnip = truncateText(b.Text, 200)
			}
		}
		if b.Type == "tool_use" {
			e.HasTool = true
			filePath, detail := extractToolMetadata(b.Name, b.Input)
			if b.Name != "" {
				e.ToolNames = append(e.ToolNames, b.Name)
			}
			e.ToolFiles = append(e.ToolFiles, filePath)
			e.ToolDetails = append(e.ToolDetails, detail)
		}
	}
}

func extractToolMetadata(toolName string, raw json.RawMessage) (string, string) {
	if len(raw) == 0 {
		return "", ""
	}

	var input struct {
		FilePath    string `json:"file_path"`
		Path        string `json:"path"`
		Command     string `json:"command"`
		Description string `json:"description"`
		Pattern     string `json:"pattern"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", ""
	}

	filePath := strings.TrimSpace(input.FilePath)
	if filePath == "" {
		filePath = strings.TrimSpace(input.Path)
	}

	switch {
	case strings.TrimSpace(input.Description) != "":
		return filePath, truncateText(input.Description, 100)
	case strings.TrimSpace(input.Command) != "":
		command := strings.ReplaceAll(strings.TrimSpace(input.Command), "\n", " ")
		return filePath, truncateText(command, 100)
	case strings.TrimSpace(input.Pattern) != "":
		return filePath, truncateText(input.Pattern, 100)
	case filePath != "":
		return filePath, filepath.Base(filePath)
	case toolName != "":
		return "", toolName
	default:
		return "", ""
	}
}

func parseCodexEntry(line []byte) (JSONLEntry, bool) {
	var raw struct {
		Timestamp string          `json:"timestamp"`
		Type      string          `json:"type"`
		Payload   json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(line, &raw); err != nil {
		return JSONLEntry{}, false
	}

	switch raw.Type {
	case "session_meta":
		return JSONLEntry{}, false
	case "event_msg":
		return parseCodexEventMsg(raw.Timestamp, raw.Payload)
	case "response_item":
		return parseCodexResponseItem(raw.Timestamp, raw.Payload)
	default:
		return JSONLEntry{}, false
	}
}

func parseCodexEventMsg(timestamp string, payload json.RawMessage) (JSONLEntry, bool) {
	var msg struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		return JSONLEntry{}, false
	}
	switch msg.Type {
	case "user_message":
		return JSONLEntry{
			Type:      "user",
			Timestamp: timestamp,
			Role:      "user",
			HasText:   strings.TrimSpace(msg.Message) != "",
			TextSnip:  truncateText(msg.Message, 200),
		}, strings.TrimSpace(msg.Message) != ""
	case "agent_message":
		return JSONLEntry{
			Type:      "assistant",
			Timestamp: timestamp,
			Role:      "assistant",
			HasText:   strings.TrimSpace(msg.Message) != "",
			TextSnip:  truncateText(msg.Message, 200),
		}, strings.TrimSpace(msg.Message) != ""
	default:
		return JSONLEntry{}, false
	}
}

func parseCodexResponseItem(timestamp string, payload json.RawMessage) (JSONLEntry, bool) {
	var item struct {
		Type      string          `json:"type"`
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
		Input     json.RawMessage `json:"input"`
		Role      string          `json:"role"`
		Content   json.RawMessage `json:"content"`
		Item      struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"item"`
	}
	if err := json.Unmarshal(payload, &item); err != nil {
		return JSONLEntry{}, false
	}

	switch item.Type {
	case "message":
		text := extractCodexMessageText(item.Content)
		role := strings.TrimSpace(item.Role)
		if role == "" {
			role = "assistant"
		}
		if strings.TrimSpace(text) == "" {
			return JSONLEntry{}, false
		}
		return JSONLEntry{
			Type:      role,
			Timestamp: timestamp,
			Role:      role,
			HasText:   true,
			TextSnip:  truncateText(text, 200),
		}, true
	case "function_call":
		entry := JSONLEntry{
			Type:      "assistant",
			Timestamp: timestamp,
			Role:      "assistant",
			HasTool:   true,
		}
		appendCodexToolCall(&entry, item.Name, item.Arguments)
		return entry, len(entry.ToolNames) > 0
	case "custom_tool_call":
		entry := JSONLEntry{
			Type:      "assistant",
			Timestamp: timestamp,
			Role:      "assistant",
			HasTool:   true,
		}
		appendCodexToolCall(&entry, item.Name, item.Input)
		return entry, len(entry.ToolNames) > 0
	case "item.completed":
		if item.Item.Type == "agent_message" && strings.TrimSpace(item.Item.Text) != "" {
			return JSONLEntry{
				Type:      "assistant",
				Timestamp: timestamp,
				Role:      "assistant",
				HasText:   true,
				TextSnip:  truncateText(item.Item.Text, 200),
			}, true
		}
	}

	return JSONLEntry{}, false
}

func extractCodexMessageText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var plain string
	if err := json.Unmarshal(raw, &plain); err == nil {
		return plain
	}

	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}

	var parts []string
	for _, block := range blocks {
		if strings.TrimSpace(block.Text) == "" {
			continue
		}
		parts = append(parts, block.Text)
	}
	return strings.Join(parts, "\n")
}

func appendCodexToolCall(entry *JSONLEntry, name string, raw json.RawMessage) {
	name = strings.TrimSpace(name)
	raw = normalizedCodexToolArguments(raw)
	switch name {
	case "multi_tool_use.parallel":
		var wrapper struct {
			ToolUses []struct {
				RecipientName string          `json:"recipient_name"`
				Parameters    json.RawMessage `json:"parameters"`
			} `json:"tool_uses"`
		}
		if err := json.Unmarshal(raw, &wrapper); err != nil {
			appendTool(entry, "Parallel", "", "")
			return
		}
		for _, tool := range wrapper.ToolUses {
			childName := tool.RecipientName
			if idx := strings.LastIndex(childName, "."); idx >= 0 && idx < len(childName)-1 {
				childName = childName[idx+1:]
			}
			appendCodexToolCall(entry, childName, tool.Parameters)
		}
	case "exec_command":
		var args struct {
			Cmd string `json:"cmd"`
		}
		_ = json.Unmarshal(raw, &args)
		appendTool(entry, "Bash", "", truncateText(args.Cmd, 100))
	case "shell":
		var args struct {
			Cmd     string   `json:"cmd"`
			Command []string `json:"command"`
		}
		_ = json.Unmarshal(raw, &args)
		command := strings.TrimSpace(args.Cmd)
		if command == "" && len(args.Command) > 0 {
			command = strings.Join(args.Command, " ")
		}
		appendTool(entry, "Bash", "", truncateText(command, 100))
	case "write_stdin":
		appendTool(entry, "Bash", "", "write_stdin")
	case "apply_patch":
		files := applyPatchFiles(string(raw))
		if len(files) == 0 {
			appendTool(entry, "Edit", "", "apply_patch")
			return
		}
		for _, file := range files {
			appendTool(entry, "Edit", strings.TrimSpace(file), filepath.Base(strings.TrimSpace(file)))
		}
	case "view_image":
		var args struct {
			Path string `json:"path"`
		}
		_ = json.Unmarshal(raw, &args)
		appendTool(entry, "Read", args.Path, filepath.Base(args.Path))
	default:
		appendTool(entry, name, "", name)
	}
}

func normalizedCodexToolArguments(raw json.RawMessage) json.RawMessage {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return raw
	}

	var encoded string
	if err := json.Unmarshal(raw, &encoded); err == nil {
		return json.RawMessage(encoded)
	}
	return raw
}

func applyPatchFiles(raw string) []string {
	matches := applyPatchFilePattern.FindAllStringSubmatch(raw, -1)
	var files []string
	seen := make(map[string]bool)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		file := strings.TrimSpace(match[1])
		if file == "" || seen[file] {
			continue
		}
		seen[file] = true
		files = append(files, file)
	}
	return files
}

func appendTool(entry *JSONLEntry, name, filePath, detail string) {
	entry.ToolNames = append(entry.ToolNames, name)
	entry.ToolFiles = append(entry.ToolFiles, filePath)
	entry.ToolDetails = append(entry.ToolDetails, detail)
}

func truncateText(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max]
}

// DetectCCStatus returns the current CC status: "active", "thinking", or "idle".
func DetectCCStatus(jsonlPath string) string {
	return DetectAgentStatus(jsonlPath, ProviderClaude)
}

func DetectAgentStatus(jsonlPath, provider string) string {
	// Fast path: check mtime.
	info, err := os.Stat(jsonlPath)
	if err != nil {
		return "idle"
	}
	if time.Since(info.ModTime()) < 30*time.Second {
		return "active"
	}

	if normalizeProvider(provider, ProviderClaude) == ProviderClaude {
		// Check Claude sub-agent files.
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
	}

	// Slow path: scan last entry in JSONL.
	lastType := scanLastEntryType(jsonlPath, provider)
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
func scanLastEntryType(path, provider string) string {
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
		switch normalizeProvider(provider, ProviderClaude) {
		case ProviderCodex:
			if entryType := codexEntryRole(line); entryType != "" {
				lastType = entryType
			}
		default:
			var entry struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(line, &entry) == nil {
				if entry.Type == "user" || entry.Type == "assistant" {
					lastType = entry.Type
				}
			}
		}
	}
	return lastType
}

func codexEntryRole(line []byte) string {
	var raw struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(line, &raw); err != nil {
		return ""
	}

	switch raw.Type {
	case "event_msg":
		var payload struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(raw.Payload, &payload) != nil {
			return ""
		}
		switch payload.Type {
		case "user_message":
			return "user"
		case "agent_message", "task_complete":
			return "assistant"
		}
	case "response_item":
		var payload struct {
			Type string `json:"type"`
			Role string `json:"role"`
			Item struct {
				Type string `json:"type"`
			} `json:"item"`
			Message struct {
				Role string `json:"role"`
			} `json:"message"`
		}
		if json.Unmarshal(raw.Payload, &payload) != nil {
			return ""
		}
		switch payload.Type {
		case "function_call", "function_call_output", "reasoning", "custom_tool_call":
			return "user"
		case "message":
			if strings.TrimSpace(payload.Role) != "" {
				return payload.Role
			}
			return payload.Message.Role
		case "item.completed":
			if payload.Item.Type == "agent_message" {
				return "assistant"
			}
		}
	}

	return ""
}

// CheckForNewSession checks if a newer session file exists and returns its path.
// Returns "" if the current session is still the most recent.
func CheckForNewSession(projectDir, currentPath string) string {
	return CheckForNewSessionForProvider(projectDir, currentPath, ProviderClaude)
}

func CheckForNewSessionForProvider(projectDir, currentPath, provider string) string {
	newPath := FindSessionJSONLForProvider(projectDir, provider)
	if newPath != "" && newPath != currentPath {
		return newPath
	}
	return ""
}

func (w *JSONLWatcher) IsActive() bool {
	return time.Since(w.lastActive) < 5*time.Minute
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
		if iInfo == nil && jInfo == nil {
			return i < j
		}
		if iInfo == nil {
			return false
		}
		if jInfo == nil {
			return true
		}
		return iInfo.ModTime().After(jInfo.ModTime())
	})

	return files
}

func codexSessionCWD(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for i := 0; i < 5 && scanner.Scan(); i++ {
		line := scanner.Bytes()
		var raw struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if json.Unmarshal(line, &raw) != nil || raw.Type != "session_meta" {
			continue
		}

		var payload struct {
			CWD string `json:"cwd"`
		}
		if json.Unmarshal(raw.Payload, &payload) != nil {
			continue
		}
		cwd := strings.TrimSpace(payload.CWD)
		if cwd != "" {
			return filepath.Clean(cwd), true
		}
	}
	return "", false
}
