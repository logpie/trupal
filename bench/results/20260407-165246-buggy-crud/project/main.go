package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// User represents a user in the system
type User struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// RateLimit tracks requests per IP
type RateLimit struct {
	count     int
	lastReset time.Time
}

// Server holds application state
type Server struct {
	users      map[int]User
	nextID     int
	mu         sync.RWMutex
	cacheMu    sync.RWMutex
	cacheByID  map[int]User
	rateLimits map[string]*RateLimit
	rateMu     sync.Mutex
}

// NewServer creates a new server instance
func NewServer() *Server {
	return &Server{
		users:      make(map[int]User),
		nextID:     1,
		cacheByID:  make(map[int]User),
		rateLimits: make(map[string]*RateLimit),
	}
}

// authMiddleware checks for valid Bearer token
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer secret-token" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// rateLimitMiddleware enforces rate limiting per IP
func (s *Server) rateLimitMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := getClientIP(r)
		s.rateMu.Lock()

		rl, exists := s.rateLimits[ip]
		if !exists {
			s.rateLimits[ip] = &RateLimit{count: 1, lastReset: time.Now()}
			s.rateMu.Unlock()
		} else {
			// Reset counter if 1 minute has passed
			if time.Since(rl.lastReset) > time.Minute {
				rl.count = 1
				rl.lastReset = time.Now()
				s.rateMu.Unlock()
			} else if rl.count >= 100 {
				s.rateMu.Unlock()
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			} else {
				rl.count++
				s.rateMu.Unlock()
			}
		}

		next(w, r)
	}
}

// getClientIP extracts client IP from request
func getClientIP(r *http.Request) string {
	ip := r.Header.Get("X-Forwarded-For")
	if ip != "" {
		return strings.Split(ip, ",")[0]
	}
	ip = r.Header.Get("X-Real-IP")
	if ip != "" {
		return ip
	}
	return strings.Split(r.RemoteAddr, ":")[0]
}

// writeJSON writes a JSON response
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// GetUsers lists all users
func (s *Server) GetUsers(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	users := make([]User, 0, len(s.users))
	for _, u := range s.users {
		users = append(users, u)
	}
	s.mu.RUnlock()

	writeJSON(w, http.StatusOK, users)
}

// CreateUser creates a new user
func (s *Server) CreateUser(w http.ResponseWriter, r *http.Request) {
	var user User
	if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	user.ID = s.nextID
	s.nextID++
	s.users[user.ID] = user
	s.mu.Unlock()

	s.cacheMu.Lock()
	s.cacheByID[user.ID] = user
	s.cacheMu.Unlock()

	writeJSON(w, http.StatusCreated, user)
}

// GetUser retrieves a user by ID (with caching)
func (s *Server) GetUser(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/users/")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}

	// Check cache first
	s.cacheMu.RLock()
	if user, ok := s.cacheByID[id]; ok {
		s.cacheMu.RUnlock()
		writeJSON(w, http.StatusOK, user)
		return
	}
	s.cacheMu.RUnlock()

	// Look up in main storage
	s.mu.RLock()
	user, ok := s.users[id]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, `{"error":"user not found"}`, http.StatusNotFound)
		return
	}

	// Update cache
	s.cacheMu.Lock()
	s.cacheByID[id] = user
	s.cacheMu.Unlock()

	writeJSON(w, http.StatusOK, user)
}

// UpdateUser updates an existing user
func (s *Server) UpdateUser(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/users/")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}

	var updates User
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	user, ok := s.users[id]
	if !ok {
		s.mu.Unlock()
		http.Error(w, `{"error":"user not found"}`, http.StatusNotFound)
		return
	}

	// Update only provided fields
	if updates.Name != "" {
		user.Name = updates.Name
	}
	if updates.Email != "" {
		user.Email = updates.Email
	}

	s.users[id] = user
	s.mu.Unlock()

	// Invalidate cache
	s.cacheMu.Lock()
	delete(s.cacheByID, id)
	s.cacheMu.Unlock()

	writeJSON(w, http.StatusOK, user)
}

// DeleteUser deletes a user
func (s *Server) DeleteUser(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/users/")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	_, ok := s.users[id]
	if !ok {
		s.mu.Unlock()
		http.Error(w, `{"error":"user not found"}`, http.StatusNotFound)
		return
	}

	delete(s.users, id)
	s.mu.Unlock()

	// Invalidate cache
	s.cacheMu.Lock()
	delete(s.cacheByID, id)
	s.cacheMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNoContent)
}

// router handles routing
func (s *Server) router(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/users":
		s.rateLimitMiddleware(s.authMiddleware(s.GetUsers))(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/users":
		s.rateLimitMiddleware(s.authMiddleware(s.CreateUser))(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/users/"):
		s.rateLimitMiddleware(s.authMiddleware(s.GetUser))(w, r)
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/users/"):
		s.rateLimitMiddleware(s.authMiddleware(s.UpdateUser))(w, r)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/users/"):
		s.rateLimitMiddleware(s.authMiddleware(s.DeleteUser))(w, r)
	default:
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	}
}

func main() {
	server := NewServer()
	http.HandleFunc("/", server.router)

	fmt.Println("Starting server on :8080")
	fmt.Println("Auth: Bearer secret-token")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal(err)
	}
}
