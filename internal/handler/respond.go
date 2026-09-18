package handler

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/nikolaykonkin/shortlink/internal/apperrors"
)

// writeJSON сериализует payload в тело ответа с заданным статусом
func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("запись JSON-ответа: %v", err)
	}
}

// respondError маппит ошибку в HTTP-статус и пишет ответ
//
// Клиенту уходит текст ошибки только для известных доменных случаев — заранее написанные
// безопасные сообщения, для всего остального — общий текст без деталей,
// а исходная ошибка попадает в лог, а не в ответ
func respondError(w http.ResponseWriter, err error) {
	status := apperrors.ToHTTPStatus(err)
	if status == http.StatusInternalServerError {
		log.Printf("внутренняя ошибка: %v", err)
		http.Error(w, "внутренняя ошибка сервера", status)
		return
	}

	http.Error(w, err.Error(), status)
}
