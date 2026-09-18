package main

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"

	"accelerator/internal/core/config"
	"accelerator/internal/core/server/middleware"
	"accelerator/internal/microservices/notification/gateway"
	"accelerator/internal/microservices/notification/repository"
	"accelerator/internal/microservices/notification/service"
	"accelerator/internal/microservices/notification/transport"
)

func main() {
	cfg := config.LoadConfig()

	hub := gateway.NewHub()
	go hub.Run()

	repo := repository.NewRepository()
	svc := service.NewService(repo, hub)
	handler := transport.NewHandler(svc)
	wsHandler := transport.NewWSHandler(hub)

	// подключаем роутер и мидлвары
	r := chi.NewRouter()
	r.Use(chiMiddleware.Logger)
	r.Use(chiMiddleware.Recoverer)

	// внутренний эндпоинт для получения событий от монолита. 
	// не требует авторизации, так как монолит доверяет микросервису
	r.Post("/internal/events", handler.HandleInternalEvent)

	// публичные эндпоинты для работы с уведомлениями, 
	// требуют авторизации
	r.Group(func(r chi.Router) {
		r.Use(middleware.AuthMiddleware(cfg))

		r.Get("/ws/notifications", wsHandler.HandleWebSocketUpgrade)
		r.Get("/api/notifications", handler.HandleGetHistory)
		r.Get("/api/notifications/unread-count", handler.HandleGetUnreadCount)
		r.Post("/api/notifications/{id}/read", handler.HandleMarkAsRead)
	})

	// стартуем сервер
	port := cfg.NotificationServicePort
	slog.Info("Notification microservice starting", "port", port)
	
	if err := http.ListenAndServe(port, r); err != nil {
		slog.Error("Failed to start server", "err", err)
	}
}