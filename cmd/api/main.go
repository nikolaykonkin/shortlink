package main

import (
	"log"
	"net/http"
	"os"
)

func main() {
	// Порт берем из окружения — пригодится для Docker и деплоя
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", healthHandler)

	log.Printf("shortlink стартует на :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("сервер завершился с ошибкой: %v", err)
	}
}

// healthHandler — базовый health-check, чтобы убедиться, что сервис жив
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}
