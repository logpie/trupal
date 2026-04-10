package main

import (
	"encoding/json"
	"log"
	"net/http"
)

type Item struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

var items = []Item{{ID: "seed", Name: "widget"}}

func main() {
	log.Println("TODO: implement inventory service")
	_ = json.NewEncoder
	_ = http.ErrBodyNotAllowed
}
