package main

import "strings"

type CurrentIssue struct {
	ID       string
	Severity string
	Status   string
	Nudge    string
	Why      string
	Ref      string
}

func (i CurrentIssue) Key() string {
	if strings.TrimSpace(i.ID) != "" {
		return i.ID
	}
	return strings.TrimSpace(i.Nudge)
}

func (i CurrentIssue) Message() string {
	return strings.TrimSpace(i.Nudge)
}
