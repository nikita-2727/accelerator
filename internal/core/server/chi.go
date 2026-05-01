package server

import (
	"accelerator/internal/features/auth/transport"
	"net/http"

	"github.com/go-chi/chi/middleware"
	"github.com/go-chi/chi/v5"
)

func NewChiServer(trans *transport.AuthTransport, port string) {
	router := chi.NewRouter() // используем chi, он легковесный , в нем есть встроенные обработчики переменных в паттерне и нормальный роутинг

	router.Use(middleware.RequestID) // генерирует для каждого запроса уникальный ID
	router.Use(middleware.RealIP)    // позволяет видеть реальный IP пользователя для логера
	router.Use(middleware.Logger)    // Потом, чтобы логировать всё, включая айдишник

	// создаем группу api с префиксом
	router.Route("/api/v1", func(router chi.Router) {
		router.Post("/register", trans.RegisterHandle)
		router.Post("/login", trans.LoginHandle)
		router.Post("/refresh", trans.RefreshHandle)
	})

	http.ListenAndServe(port, router)
	
}
