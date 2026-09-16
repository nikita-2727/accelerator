package main

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"accelerator/internal/notification/transport"
	"accelerator/internal/core/config"
)

func main() {
	cfg := config.LoadConfig()

	r := chi.NewRouter()

	eventHandler := transport.NewEventHandler()

	r.Post("/event", eventHandler.HandleEvent)

	port := cfg.NotificationServicePort
	
	slog.Info("Notification microservice starting", "port", port)
	
	if err := http.ListenAndServe(port, r); err != nil {
		slog.Error("Failed to start notification server", "err", err)
	}
}