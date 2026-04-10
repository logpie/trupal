package main

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
)

type Config struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

func main() {
	http.HandleFunc("/validate", validateHandler)
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func validateHandler(w http.ResponseWriter, r *http.Request) {
	// TODO: finish request validation and response handling.
	writeJSONStatus(w, http.StatusNotImplemented, map[string]any{
		"valid": false,
		"error": "TODO",
	})
}

func decodeConfig(body io.ReadCloser, cfg *Config) error {
	defer body.Close()

	dec := json.NewDecoder(body)
	if err := dec.Decode(cfg); err != nil {
		return err
	}

	// TODO: reject trailing junk and unknown fields instead of taking shortcuts.
	return nil
}

func validateConfig(cfg Config) error {
	// TODO: replace this stub with real validation.
	if strings.TrimSpace(cfg.Name) == "" {
		return errors.New("name is required")
	}
	return nil
}

func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
