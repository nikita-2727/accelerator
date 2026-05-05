package main

import (
	"accelerator/internal/core/config"
	"accelerator/internal/core/logger"
	"accelerator/internal/core/server"
	 authRepository "accelerator/internal/features/auth/repository"
	 tasksRepository "accelerator/internal/features/tasks/repository"

	 authService "accelerator/internal/features/auth/service"
	 tasksService"accelerator/internal/features/tasks/service"

	 authTransport "accelerator/internal/features/auth/transport"
	 tasksTransport "accelerator/internal/features/tasks/transport"
	"context"
	"log/slog"

	"github.com/go-playground/validator/v10"

	"github.com/jackc/pgx/v5/pgxpool"
)





// что можно добавить в будущем для безопасности?
// 2) хранение в сессии еще и fingerprint, чтобы привязывать сессию к определенному устройству чтобы обрабатывать подозрения на угон аккаунта


func main() {
	cfg := config.LoadConfig()  // загружаем .env и все его значения
	logger.InitLog()            // инициализируем логер, чтобы нормально записывать в файл
	validate := validator.New() // создаем валидатор, чтобы потом передать в хэндлеры

	pool, err := pgxpool.New(context.Background(), cfg.DBDSN) // создаем пул соединений
	defer func() { pool.Close() }() // перед завершением работы закрываем соединение с базой данных
	if err != nil {
		slog.Error("Не удалось создать пул соединений с базой данных:", "err", err)
	}

	authRepo := authRepository.NewAuthRepo(pool)          // возвращает указатель на репозиторий с указателем на подключение к базе данных и соответсвенно методы работы с бд
	authServ := authService.NewAuthService(authRepo, cfg) // передаем методы работы с бд в бизнес логику, возвращает методы работы бизнес логики
	authTrans := authTransport.NewAuthTransport(authServ, validate) // передаем нашу методы из бизнес логики и созданный валидатор

	TasksRepo := tasksRepository.NewTasksRepo(pool)          
	TasksServ := tasksService.NewTasksService(TasksRepo, cfg)
	TasksTrans := tasksTransport.NewTasksTransport(TasksServ, validate, cfg)


	if err := server.StartNewChiServer(authTrans, TasksTrans, cfg.ServerPort); err != nil {
		slog.Error("Ошибка при работе HTTP сервера:", "err", err)
	} else {
		slog.Info("Сервер завершился успешно")
	}
}
