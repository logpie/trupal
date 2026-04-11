package main

import (
	"fmt"
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
		{"display-message", "-p", "-t", "%pane", "#{pane_in_mode}"},
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

func TestSendAgentMessageToPaneExitsTmuxModeFirst(t *testing.T) {
	var calls [][]string
	prev := runTmuxCommand
	prevDelay := steeringSubmitDelay
	runTmuxCommand = func(args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		switch {
		case reflect.DeepEqual(args, []string{"display-message", "-p", "-t", "%pane", "#{pane_in_mode}"}):
			return []byte("1\n"), nil
		default:
			return nil, nil
		}
	}
	steeringSubmitDelay = 0
	defer func() {
		runTmuxCommand = prev
		steeringSubmitDelay = prevDelay
	}()

	if err := sendAgentMessageToPane("%pane", "hello"); err != nil {
		t.Fatalf("sendAgentMessageToPane() error = %v", err)
	}

	want := [][]string{
		{"display-message", "-p", "-t", "%pane", "#{pane_in_mode}"},
		{"send-keys", "-X", "-t", "%pane", "cancel"},
		{"send-keys", "-t", "%pane", "-l", "hello"},
		{"send-keys", "-t", "%pane", "Enter"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("tmux calls = %#v, want %#v", calls, want)
	}
}

func TestSendAgentMessageToPaneRetriesAfterModeError(t *testing.T) {
	var calls [][]string
	prev := runTmuxCommand
	prevDelay := steeringSubmitDelay
	runTmuxCommand = func(args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		switch {
		case reflect.DeepEqual(args, []string{"display-message", "-p", "-t", "%pane", "#{pane_in_mode}"}):
			return []byte("0\n"), nil
		case reflect.DeepEqual(args, []string{"send-keys", "-t", "%pane", "-l", "hello"}):
			if len(calls) == 2 {
				return []byte("not in a mode"), fmt.Errorf("exit status 1")
			}
			return nil, nil
		default:
			return nil, nil
		}
	}
	steeringSubmitDelay = 0
	defer func() {
		runTmuxCommand = prev
		steeringSubmitDelay = prevDelay
	}()

	if err := sendAgentMessageToPane("%pane", "hello"); err != nil {
		t.Fatalf("sendAgentMessageToPane() error = %v", err)
	}

	want := [][]string{
		{"display-message", "-p", "-t", "%pane", "#{pane_in_mode}"},
		{"send-keys", "-t", "%pane", "-l", "hello"},
		{"send-keys", "-X", "-t", "%pane", "cancel"},
		{"send-keys", "-t", "%pane", "-l", "hello"},
		{"send-keys", "-t", "%pane", "Enter"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("tmux calls = %#v, want %#v", calls, want)
	}
}

func TestSendAgentMessageToPaneRetriesSubmitAfterModeError(t *testing.T) {
	var calls [][]string
	prev := runTmuxCommand
	prevDelay := steeringSubmitDelay
	runTmuxCommand = func(args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		switch {
		case reflect.DeepEqual(args, []string{"display-message", "-p", "-t", "%pane", "#{pane_in_mode}"}):
			return []byte("0\n"), nil
		case reflect.DeepEqual(args, []string{"send-keys", "-t", "%pane", "Enter"}):
			if len(calls) == 3 {
				return []byte("not in a mode"), fmt.Errorf("exit status 1")
			}
			return nil, nil
		default:
			return nil, nil
		}
	}
	steeringSubmitDelay = 0
	defer func() {
		runTmuxCommand = prev
		steeringSubmitDelay = prevDelay
	}()

	if err := sendAgentMessageToPane("%pane", "hello"); err != nil {
		t.Fatalf("sendAgentMessageToPane() error = %v", err)
	}

	want := [][]string{
		{"display-message", "-p", "-t", "%pane", "#{pane_in_mode}"},
		{"send-keys", "-t", "%pane", "-l", "hello"},
		{"send-keys", "-t", "%pane", "Enter"},
		{"send-keys", "-X", "-t", "%pane", "cancel"},
		{"send-keys", "-t", "%pane", "Enter"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("tmux calls = %#v, want %#v", calls, want)
	}
}
