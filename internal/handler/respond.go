package handler

import (
	"encoding/json"
	"log"
	"net/http"
)

// writeJSON сериализует payload в тело ответа с заданным статусом
func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("запись JSON-ответа: %v", err)
	}
}
