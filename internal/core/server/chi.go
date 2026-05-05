package server

import (
	authTransport "accelerator/internal/features/auth/transport"
	tasksTransport "accelerator/internal/features/tasks/transport"
	"errors"
	"net/http"

	"github.com/go-chi/chi/middleware"
	"github.com/go-chi/chi/v5"
)

func StartNewChiServer(authTrans *authTransport.AuthTransport, tasksTrans *tasksTransport.TasksTransport, port string) error {
	router := chi.NewRouter() // используем chi, он легковесный , в нем есть встроенные обработчики переменных в паттерне и нормальный роутинг

	router.Use(middleware.RequestID) // генерирует для каждого запроса уникальный ID
	router.Use(middleware.RealIP)    // позволяет видеть реальный IP пользователя для логера
	router.Use(middleware.Logger)    // Потом, чтобы логировать всё, включая айдишник

	// создаем группу api с префиксом
	router.Route("/api/v1", func(router chi.Router) {
		router.Post("/register", authTrans.RegisterHandle)
		router.Post("/login", authTrans.LoginHandle)
		router.Post("/refresh", authTrans.RefreshHandle)

		router.Post("/upload", tasksTrans.UploadHandle)
	})

	err := http.ListenAndServe(port, router)

	if errors.Is(err, http.ErrServerClosed) { // если ошибка при нормальном завершении сервера. то все ок
		return nil
	} else {
		return err
	}
	
}
