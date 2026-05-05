package repository

import (
	"accelerator/internal/core/error_type"
	"accelerator/internal/domains"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// создаем интейрфейс, который удовлетворяет и pgxpool.Pool и tx
// чтобы потом в методах репозитория не зависеть только от типа pool и чтобы можно было выполнять методы и через tx
type executor interface { 
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// передаем экземпляр на пул подключений, чтобы потом юзать методы через единый экземпляр и не передавать постоянно его в функции
type AuthRepo struct {
	pool *pgxpool.Pool
}

// конструктор репозитория, вызываем в main
func NewAuthRepo(pool *pgxpool.Pool) *AuthRepo {
	return &AuthRepo{
		pool: pool,
	}
}

// по сути конструтор для создания транзакции, так как сервис не может иметь напрямую доступ к repo.pool
// то создаем транзакцию через pool здесь, а в сервисе только используем методы транзакций
func (repo *AuthRepo) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	return repo.pool.BeginTx(ctx, opts)
}




// АНОНИМНАЯ ФУНКЦИЯ ДЛЯ РАБОТЫ ЧЕРЕЗ ИНТЕРФЕЙС ПОДКЛЮЧЕНИЯ
// принимает email и хеш пароля, интерфейс работы с бд, через транзакцию или через pool
// сохраняем пользователя в бд, получаем его id, возвращаем ошибки
// возвращаем id пользователя для генерации jwt токена
func (repo *AuthRepo) createUser(ctx context.Context, e executor, email, passwordHash string) (string, error) {
	sqlQuery := `
	INSERT INTO users (email, password_hash) 
	VALUES ($1, $2)
	RETURNING id;
	`
	var id string // обработка ошибки откладывается до момента получения id, pgx.Row закроется самостоятельно после Scan
	err := e.QueryRow(ctx, sqlQuery, email, passwordHash).Scan(&id) // уже работаем либо через tx либо pool
	/*
		errors.As идёт по цепочке ошибок (распаковывая их через методы Unwrap(), Unwrap() []error и т.д.) и пытается присвоить первой же ошибке, которая может быть присвоена типу target.
		Если находит — записывает её в переменную, на которую указывает target, и возвращает true
		Если не находит — возвращает false, а target остаётся без изменений
	*/
	// !!!!! теперь я понял, из интерфейса error, который возвращается из Scan вытаскиваем внутреннюю кастомную ошибку типа *pgconn.PgError, такое нужно когда кастомную ошибку вернули из функцию как обычную error
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" { // Это код, когда запись уже существует в бд, т епользователь с такой почтой уже существует
		return "", error_type.NewConflict("A user with such an email already exists")
	} else if err != nil { // в id может вернуться значение по умолчанию или остаться пустая строка
		return "", error_type.NewInternal(fmt.Errorf("create user: %w", err)) // иначе другая любая ошибка базы данных
	}

	return id, nil
}

// публичная обертка для добавления пользователя через pool
// так как мы не сможем передать в параметр executor - pool из сервиса, потому что он инкапсулирован внутри репозитория
func (repo *AuthRepo) CreateUser(ctx context.Context, email, passwordHash string) (string, error) {
	return repo.createUser(ctx, repo.pool, email, passwordHash)
}
// публичная обертка для добавления пользователя через tx
// сам tx уже передается на уровне бизнес логики, чтобы управлять коммитами и ролбеками в случае ошибок уровня сервиса
func (repo *AuthRepo) CreateUserTx(ctx context.Context, tx pgx.Tx,  email, passwordHash string) (string, error) {
	return repo.createUser(ctx, tx, email, passwordHash)
}



// АНОНИМНАЯ ФУНКЦИЯ ДЛЯ РАБОТЫ ЧЕРЕЗ ИНТЕРФЕЙС ПОДКЛЮЧЕНИЯ
// принимает хеш, уникальный ключ токена, время создания и время исхода рефреш токена
// создает новую сессию в бд
// возвращает ошибки
func (repo *AuthRepo) createSession(
		ctx context.Context, 
		e executor, 
		userID, refreshHash, RefreshJTI string, 
		refreshCreate, refreshExpire time.Time,
	) error {
	sqlQuery := `
	INSERT INTO sessions (user_id, token_hash, jti, created_at, expires_at) 
	VALUES ($1, $2, $3, $4, $5);
	`
	_, err := e.Exec(ctx, sqlQuery, userID, refreshHash, RefreshJTI, refreshCreate, refreshExpire) // тоже самое, унивесальная функция
	if err != nil {
		return error_type.NewInternal(fmt.Errorf("create session: %w", err))
	}

	return nil
}

// публичные обертки
func (repo *AuthRepo) CreateSession(
		ctx context.Context, 
		userID, refreshHash, RefreshJTI string, 
		refreshCreate, refreshExpire time.Time,
	) error {
	return repo.createSession(ctx, repo.pool, userID, refreshHash, RefreshJTI, refreshCreate, refreshExpire)
}

func (repo *AuthRepo) CreateSessionTx(
		ctx context.Context, 
		tx pgx.Tx,  
		userID, refreshHash, RefreshJTI string, 
		refreshCreate, refreshExpire time.Time,
	) error {
	return repo.createSession(ctx, tx, userID, refreshHash, RefreshJTI, refreshCreate, refreshExpire)
}



// ДЛЯ ОБЫЧНЫХ SELECT ЗАПРОСОВ НЕ НУЖНЫ ТРАНЗАКЦИИ, ОНИ НЕ МЕНЯЮТ БАЗУ ДАННЫХ
// проверяет существование пользователя с такой почтой
// возвращает ошибки, полученный hash от пароля и id пользователя
func (repo *AuthRepo) GetAuthCredentials(ctx context.Context, email string) (*domains.UserAuthInfo, error) {
    sqlQuery := `
        SELECT id, password_hash 
        FROM users 
        WHERE email = $1;
    `
	var info domains.UserAuthInfo
    err := repo.pool.QueryRow(ctx, sqlQuery, email).Scan(&info.ID, &info.PasswordHash)
    if errors.Is(err, pgx.ErrNoRows) { // специальный тип ошибки, если ничего не вернулось
        return nil, error_type.NewUnauthorized("Invalid login or password") // пользователь не найден
    } else if err != nil {
        return nil, error_type.NewInternal(fmt.Errorf("get auth credentials: %w", err))
    }
    return &info, nil
}




// принимает jti рефреш токена
// получает информацию о сессии
// возвращает информацию о сессии и ошибку
func (repo *AuthRepo) GetSessionByJTI(ctx context.Context, jti string) (*domains.Session, error) {
	sqlQuery := `
	SELECT user_id, token_hash, jti, revoked_at, created_at, expires_at
	FROM sessions
	WHERE jti = $1;
	`
	var sessionInfo domains.Session // получаем информации о сессии по данному jti токена
	err := repo.pool.QueryRow(ctx, sqlQuery, jti).Scan(
		&sessionInfo.UserID, 
		&sessionInfo.TokenHash,
		&sessionInfo.TokenJTI,
		&sessionInfo.RevokedAt,
		&sessionInfo.CreatedAt,
		&sessionInfo.ExpiresAt,
	)
    if errors.Is(err, pgx.ErrNoRows) { // специальный тип ошибки, если ничего не вернулось
        return nil, error_type.NewUnauthorized("Session not found") // сессия не найдена
    } else if err != nil {
        return nil, error_type.NewInternal(fmt.Errorf("get session info: %w", err))
    }

    return &sessionInfo, nil
}

// транзакции пока не требуются для этой функции, так что не делаем оберток
// отзывает все активные сессии пользователя, устанавливая revoked_at = NOW()
// Принимает userID и ошибку при проблемах с БД
func (repo *AuthRepo) RevokeAllUserSessions(ctx context.Context, userID string) error {
    query := `
        UPDATE sessions
        SET revoked_at = NOW()
        WHERE user_id = $1 AND revoked_at IS NULL
    `
    _, err := repo.pool.Exec(ctx, query, userID)
    if err != nil {
        return error_type.NewInternal(fmt.Errorf("revoke all user sessions: %w", err))
    }
    return nil
}


// АНОНИМНАЯ ФУНКЦИЯ ДЛЯ РАБОТЫ ЧЕРЕЗ ИНТЕРФЕЙС ПОДКЛЮЧЕНИЯ
//  помечает сессию как отозванную
// Принимает jti, если сессия уже отозвана или не существует,
// возвращает error_type.NewUnauthorized
func (repo *AuthRepo) revokeSession(ctx context.Context, e executor, jti string) error {
    query := `
        UPDATE sessions
        SET revoked_at = NOW()
        WHERE jti = $1 AND revoked_at IS NULL
    `
    cmdTag, err := e.Exec(ctx, query, jti)
    if err != nil {
        return error_type.NewInternal(fmt.Errorf("revoke session by jti: %w", err))
    }
    if cmdTag.RowsAffected() == 0 { // если ничего не вернулось из exec
        return error_type.NewUnauthorized("Session not found") // сессия не найдена
    }
    return nil
}

// обертки для функции
func (repo *AuthRepo) RevokeSession(ctx context.Context, jti string) error {
	return repo.revokeSession(ctx, repo.pool, jti)
}
func (repo *AuthRepo) RevokeSessionTx(ctx context.Context, tx pgx.Tx, jti string) error {
	return repo.revokeSession(ctx, tx, jti)
}