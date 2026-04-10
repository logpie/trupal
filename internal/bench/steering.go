package bench

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"time"
)

type SteeringEvent struct {
	Timestamp time.Time
	FindingID string
	Message   string
	Source    string
	PaneID    string
}

func ParseSteeringEvents(path string) ([]SteeringEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var events []SteeringEvent
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var raw struct {
			Timestamp string `json:"timestamp"`
			FindingID string `json:"finding_id"`
			Message   string `json:"message"`
			Source    string `json:"source"`
			PaneID    string `json:"pane_id"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &raw); err != nil {
			continue
		}
		t, ok := parseFlexibleTime(raw.Timestamp)
		if !ok {
			continue
		}
		events = append(events, SteeringEvent{
			Timestamp: t,
			FindingID: strings.TrimSpace(raw.FindingID),
			Message:   strings.TrimSpace(raw.Message),
			Source:    strings.TrimSpace(raw.Source),
			PaneID:    strings.TrimSpace(raw.PaneID),
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return events, nil
}
