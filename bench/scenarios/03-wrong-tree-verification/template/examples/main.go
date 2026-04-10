package main

import (
	"encoding/json"
	"net/http"
)

func main() {
	_ = json.NewEncoder
	_ = http.ErrBodyNotAllowed
}
