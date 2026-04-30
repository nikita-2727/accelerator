package repository

import (
	"accelerator/internal/core/error_type"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AuthRepo struct {
	pool *pgxpool.Pool
}

func NewAuthRepo(pool *pgxpool.Pool) *AuthRepo {
	return &AuthRepo{
		pool: pool,
	}
}

// принимает email и хеш пароля
// сохраняем пользователя в бд, получаем его id, возвращаем ошибки
// возвращаем id пользователя для генерации jwt токена
func (repo *AuthRepo) CreateUser(ctx context.Context, email, passwordHash string) (string, error) {
	sqlQuery := `
	INSERT INTO users (email, password_hash) 
	VALUES ($1, $2)
	RETURNING id;
	`
	var id string // обработка ошибки откладывается до момента получения id, pgx.Row закроется самостоятельно после Scan
	err := repo.pool.QueryRow(ctx, sqlQuery, email, passwordHash).Scan(&id)
	/*
		errors.As идёт по цепочке ошибок (распаковывая их через методы Unwrap(), Unwrap() []error и т.д.) и пытается присвоить первой же ошибке, которая может быть присвоена типу target.
		Если находит — записывает её в переменную, на которую указывает target, и возвращает true
		Если не находит — возвращает false, а target остаётся без изменений
	*/
	// !!!!! теперь я понял, из интерфейса error, который возвращается из Scan вытаскиваем внутреннюю кастомную ошибку типа *pgconn.PgError, такое нужно когда кастомную ошибку вернули из функцию как обычную error 
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" { // Это код, когда запись уже существует в бд, т епользователь с такой почтой уже существует
		return "", error_type.NewConflict("Пользователь с такой почтой уже существует")
	} else if err != nil { // в id может вернуться значение по умолчанию или остаться пустая строка
		return "", error_type.NewInternal(fmt.Errorf("create user: %w", err)) // иначе другая любая ошибка базы данных
	}

	return id, nil
}

// принимает хеш, время создания и время исхода рефреш токена
// создает новую сессию в бд
// возвращает ошибки
func (repo *AuthRepo) CreateSession(ctx context.Context, refreshHash string, refreshCreate, refreshExpire time.Time) error {
	sqlQuery := `
	INSERT INTO sessions (token_hash, created_at, expires_at) 
	VALUES ($1, $2, $3);
	`
	_, err := repo.pool.Exec(ctx, sqlQuery, refreshHash, refreshCreate, refreshExpire)
	if err != nil {
		return error_type.NewInternal(fmt.Errorf("create session: %w", err))
	}

	return nil
}
