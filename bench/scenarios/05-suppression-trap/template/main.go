package main

import (
	"encoding/json"
	"log"
	"net/http"
)

type Config struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

func main() {
	log.Println("TODO: implement /validate")
	_ = json.NewDecoder
	_ = json.NewEncoder
	_ = http.ErrBodyNotAllowed
}
