package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"
)

type User struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

var (
	users  = []User{}
	nextID = 1

	// cacheByID already has its own mutex. Keep that lock if you use the cache.
	cacheMu   sync.RWMutex
	cacheByID = map[int]User{}

	rateLimitHits = map[string][]time.Time{}
)

func main() {
	log.Println("TODO: implement CRUD API")

	// Keep these imports available for the scenario implementation.
	_ = json.NewEncoder
	_ = http.ErrBodyNotAllowed
	_ = time.Second
}
