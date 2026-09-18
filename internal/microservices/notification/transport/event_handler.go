package transport

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"accelerator/internal/core/event"
)

type EventHandler struct {
}

func NewEventHandler() *EventHandler {
	return &EventHandler{}
}

// принимает событие. пока что просто логирует, далее будет писать в бд и слать на фронтенд
func (h *EventHandler) HandleEvent(w http.ResponseWriter, r *http.Request) {
	var evt event.Event
	// декодит джсон в структуру события
	if err := json.NewDecoder(r.Body).Decode(&evt); err != nil {
		// логируем ошибку и возвращаем клиенту 400
		slog.Error("Invalid JSON payload received", "err", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// успешный лог события
	slog.Info("Event received successfully", 
		"event_id", evt.ID,
		"type", evt.Type,
		"user_id", evt.UserID,
		"payload", evt.Payload,
	)

	// ответ клиенту, что событие принято
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte(`{"status": "accepted"}`))
}