package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

type Session struct {
	ID        string    `json:"id"`
	User      string    `json:"user"`
	ExpiresAt time.Time `json:"expires_at"`
}

var sessions = map[string]Session{
	"live":    {ID: "live", User: "alice", ExpiresAt: time.Now().Add(30 * time.Minute)},
	"expired": {ID: "expired", User: "bob", ExpiresAt: time.Now().Add(-5 * time.Minute)},
}

func main() {
	log.Println("TODO: implement /state and /refresh")
	_ = json.NewEncoder
	_ = http.ErrBodyNotAllowed
	_ = time.Second
}
