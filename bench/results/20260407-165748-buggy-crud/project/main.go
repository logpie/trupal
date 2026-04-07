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

const (
	bearerToken = "secret-token"
	rateLimit   = 100 // requests per minute per IP
)

type User struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type RateLimitEntry struct {
	count     int
	resetTime time.Time
}

type Server struct {
	users     map[int]*User
	nextID    int
	userMu    sync.RWMutex
	rateLimit map[string]*RateLimitEntry
	rateMu    sync.RWMutex
	cacheMu   sync.RWMutex
	cacheByID map[int]*User
}

func NewServer() *Server {
	return &Server{
		users:     make(map[int]*User),
		nextID:    1,
		rateLimit: make(map[string]*RateLimitEntry),
		cacheByID: make(map[int]*User),
	}
}

func (s *Server) rateLimitMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clientIP := r.RemoteAddr

		s.rateMu.Lock()
		entry, exists := s.rateLimit[clientIP]

		if !exists || time.Now().After(entry.resetTime) {
			s.rateLimit[clientIP] = &RateLimitEntry{
				count:     1,
				resetTime: time.Now().Add(time.Minute),
			}
			s.rateMu.Unlock()
			next(w, r)
			return
		}

		if entry.count >= rateLimit {
			s.rateMu.Unlock()
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		entry.count++
		s.rateMu.Unlock()

		next(w, r)
	}
}

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")

		if authHeader == "" {
			http.Error(w, "missing authorization header", http.StatusUnauthorized)
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			http.Error(w, "invalid authorization header", http.StatusUnauthorized)
			return
		}

		if parts[1] != bearerToken {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}

func (s *Server) GetUsers(w http.ResponseWriter, r *http.Request) {
	s.userMu.RLock()
	users := make([]*User, 0, len(s.users))
	for _, user := range s.users {
		users = append(users, user)
	}
	s.userMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(users)
}

func (s *Server) CreateUser(w http.ResponseWriter, r *http.Request) {
	var user User
	if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if user.Name == "" || user.Email == "" {
		http.Error(w, "name and email are required", http.StatusBadRequest)
		return
	}

	s.userMu.Lock()
	user.ID = s.nextID
	s.nextID++
	s.users[user.ID] = &user
	s.userMu.Unlock()

	s.cacheMu.Lock()
	delete(s.cacheByID, user.ID)
	s.cacheMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(user)
}

func (s *Server) GetUser(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/users/")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid user ID", http.StatusBadRequest)
		return
	}

	s.cacheMu.RLock()
	if user, found := s.cacheByID[id]; found {
		s.cacheMu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(user)
		return
	}
	s.cacheMu.RUnlock()

	s.userMu.RLock()
	user, found := s.users[id]
	s.userMu.RUnlock()

	if !found {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	s.cacheMu.Lock()
	s.cacheByID[id] = user
	s.cacheMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

func (s *Server) UpdateUser(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/users/")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid user ID", http.StatusBadRequest)
		return
	}

	var updates User
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	s.userMu.Lock()
	user, found := s.users[id]
	if !found {
		s.userMu.Unlock()
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	if updates.Name != "" {
		user.Name = updates.Name
	}
	if updates.Email != "" {
		user.Email = updates.Email
	}
	s.userMu.Unlock()

	s.cacheMu.Lock()
	delete(s.cacheByID, id)
	s.cacheMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

func (s *Server) DeleteUser(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/users/")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid user ID", http.StatusBadRequest)
		return
	}

	s.userMu.Lock()
	_, found := s.users[id]
	if !found {
		s.userMu.Unlock()
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	delete(s.users, id)
	s.userMu.Unlock()

	s.cacheMu.Lock()
	delete(s.cacheByID, id)
	s.cacheMu.Unlock()

	w.WriteHeader(http.StatusNoContent)
}

func main() {
	server := NewServer()

	http.HandleFunc("/users", server.rateLimitMiddleware(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				authMiddleware(server.GetUsers)(w, r)
			case http.MethodPost:
				authMiddleware(server.CreateUser)(w, r)
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		},
	))

	http.HandleFunc("/users/", server.rateLimitMiddleware(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				authMiddleware(server.GetUser)(w, r)
			case http.MethodPut:
				authMiddleware(server.UpdateUser)(w, r)
			case http.MethodDelete:
				authMiddleware(server.DeleteUser)(w, r)
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		},
	))

	log.Println("Server running on http://localhost:8080")
	log.Println("Use Authorization: Bearer secret-token header for all requests")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
