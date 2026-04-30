package main

import (
	"accelerator/internal/core/config"
	"accelerator/internal/core/logger"
	"accelerator/internal/core/server"
	"accelerator/internal/features/auth/repository"
	"accelerator/internal/features/auth/service"
	"accelerator/internal/features/auth/transport"
	"context"
	"log"

	"github.com/go-playground/validator/v10"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	cfg := config.LoadConfig()  // загружаем .env и все его значения
	logger.InitLog()            // инициализируем логер, чтобы нормально записывать в файл
	validate := validator.New() // создаем валидатор, чтобы потом передать в хэндлеры

	pool, err := pgxpool.New(context.Background(), cfg.DBDSN) // создаем пул соединений
	if err != nil {
		log.Fatal("Не удалось создать пул соединений с базой данных:", err)
	}

	authRepo := repository.NewAuthRepo(pool)          // возвращает указатель на репозиторий с указателем на подключение к базе данных и соответсвенно методы работы с бд
	authServ := service.NewAuthService(authRepo, cfg) // передаем методы работы с бд в бизнес логику, возвращает методы работы бизнес логики
	authTrans := transport.NewAuthTransport(authServ, validate)

	server.NewChiServer(authTrans, cfg.ServerPort)
	

	pool.Close()
}
