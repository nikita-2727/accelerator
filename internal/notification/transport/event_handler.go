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

func (h *EventHandler) HandleEvent(w http.ResponseWriter, r *http.Request) {
	var evt event.Event
	if err := json.NewDecoder(r.Body).Decode(&evt); err != nil {
		slog.Warn("Invalid JSON payload received", "err", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	slog.Info("Event received successfully", 
		"event_id", evt.ID,
		"type", evt.Type,
		"user_id", evt.UserID,
		"payload", evt.Payload,
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte(`{"status": "accepted"}`))
}