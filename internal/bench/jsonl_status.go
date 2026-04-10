package bench

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"
)

func detectBenchAgentStatus(jsonlPath string) string {
	info, err := os.Stat(jsonlPath)
	if err != nil {
		return "idle"
	}
	if time.Since(info.ModTime()) < 30*time.Second {
		return "active"
	}
	switch scanBenchLastEntryType(jsonlPath) {
	case "user":
		return "thinking"
	case "assistant":
		return "idle"
	default:
		return "idle"
	}
}

func scanBenchLastEntryType(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return ""
	}
	readSize := int64(64 * 1024)
	offset := info.Size() - readSize
	if offset < 0 {
		offset = 0
	}
	_, _ = f.Seek(offset, io.SeekStart)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	lastType := ""
	for scanner.Scan() {
		var entry struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		switch entry.Type {
		case "event_msg":
			var payload struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(entry.Payload, &payload) == nil && payload.Type == "user_message" {
				lastType = "user"
			}
		case "response_item":
			var payload struct {
				Type string `json:"type"`
				Role string `json:"role"`
			}
			if json.Unmarshal(entry.Payload, &payload) == nil && payload.Type == "message" {
				role := strings.TrimSpace(strings.ToLower(payload.Role))
				if role == "assistant" || role == "user" {
					lastType = role
				}
			}
		}
	}
	return lastType
}
