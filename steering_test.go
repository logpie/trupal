package main

import (
	"reflect"
	"testing"
)

func TestSendAgentMessageToPaneUsesEnterToSubmit(t *testing.T) {
	var calls [][]string
	prev := runTmuxCommand
	prevDelay := steeringSubmitDelay
	runTmuxCommand = func(args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		return nil, nil
	}
	steeringSubmitDelay = 0
	defer func() {
		runTmuxCommand = prev
		steeringSubmitDelay = prevDelay
	}()

	if err := sendAgentMessageToPane(" %pane ", " hello "); err != nil {
		t.Fatalf("sendAgentMessageToPane() error = %v", err)
	}

	want := [][]string{
		{"send-keys", "-t", "%pane", "-l", "hello"},
		{"send-keys", "-t", "%pane", "Enter"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("tmux calls = %#v, want %#v", calls, want)
	}
}

func TestSendAgentMessageToPaneRejectsEmptyFields(t *testing.T) {
	if err := sendAgentMessageToPane("", "hello"); err == nil {
		t.Fatal("expected empty pane id to fail")
	}
	if err := sendAgentMessageToPane("%pane", "   "); err == nil {
		t.Fatal("expected empty message to fail")
	}
}
