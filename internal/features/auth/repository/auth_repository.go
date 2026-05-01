package repository

import (
	"accelerator/internal/core/error_type"
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


// НАРУШАЕТ ИНКАПСУЛЯЦИЮ СЕРВИСА, ТАК КАК ДЛЯ РАБОТЫ В СЕРВИСЕ НУЖНО ЭКСПОРТИРОВАТЬ ИНТЕРФЕЙС EXECUTER
// создаем метод обратного вызова для работы с транзакциями, чтобы не создавать и не работать с транзакциями напрямую из сервиса
// в него передается контекст для создания транзакции и сама функция, для которой необходима транзакция,
// которую потом этот метод вызовет с уже созданной транзакцией, сам обработает ошибку и если что, то осуществит rollback и вернет ошибку
// а если все прошло успешно, то закоммитит изменения
func (repo *AuthRepo) WithTransaction(ctx context.Context, funcWithTx func(e executor) error) error {
	tx, err := repo.pool.BeginTx(ctx, pgx.TxOptions{}) // создаем транзакцию
	if err != nil {
		return error_type.NewInternal(fmt.Errorf("begin tx: %w", err))
	}
	defer func() { // при какой либо ошибке перед завершением функции откатываем изменения базы данных
		_ = tx.Rollback(ctx)
	}()

	// вызываем переданную функцию с параметром транзакции, созданным ранее, если она вернет какую-нибудь ошибку,
	// то произойдет rollback и ошибка передастся дальше в сервис для обработки
	if err := funcWithTx(tx); err != nil {
		return err
	}

	// коммитим изменения, если мы не вышли на предыдущем моменте, 
	// также обрабатываем саму ошибку при неудачном коммите
	if err := tx.Commit(ctx); err != nil {
		return error_type.NewInternal(fmt.Errorf("commit tx: %w", err))
	}

	// Иначе никаких ошибок не возвращается, потому что ни в переданной функции их не было
	// ни в этой не было создано новых, значит все ок
	return nil
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
		return "", error_type.NewConflict("Пользователь с такой почтой уже существует")
	} else if err != nil { // в id может вернуться значение по умолчанию или остаться пустая строка
		return "", error_type.NewInternal(fmt.Errorf("create user: %w", err)) // иначе другая любая ошибка базы данных
	}

	return id, nil
}

// публичная обертка для добавления пользователя через pool
// так как мы не сможем передать в параметр executor - pool из сервиса, потому что он инкапсулирован внутри репозитория
func (repo *AuthRepo) CreateUser(ctx context.Context, email, password_hash string) (string, error) {
	return repo.createUser(ctx, repo.pool, email, password_hash)
}
// публичная обертка для добавления пользователя через tx
// сам tx уже передается на уровне бизнес логики, чтобы управлять коммитами и ролбеками в случае ошибок уровня сервиса
func (repo *AuthRepo) CreateUserTx(ctx context.Context, tx pgx.Tx,  email, password_hash string) (string, error) {
	return repo.createUser(ctx, tx, email, password_hash)
}



// АНОНИМНАЯ ФУНКЦИЯ ДЛЯ РАБОТЫ ЧЕРЕЗ ИНТЕРФЕЙС ПОДКЛЮЧЕНИЯ
// принимает хеш, время создания и время исхода рефреш токена
// создает новую сессию в бд
// возвращает ошибки
func (repo *AuthRepo) сreateSession(ctx context.Context, e executor, refreshHash string, refreshCreate, refreshExpire time.Time) error {
	sqlQuery := `
	INSERT INTO sessions (token_hash, created_at, expires_at) 
	VALUES ($1, $2, $3);
	`
	_, err := e.Exec(ctx, sqlQuery, refreshHash, refreshCreate, refreshExpire) // тоже самое, унивесальная функция
	if err != nil {
		return error_type.NewInternal(fmt.Errorf("create session: %w", err))
	}

	return nil
}

// публичные обертки
func (repo *AuthRepo) CreateSession(ctx context.Context, refreshHash string, refreshCreate, refreshExpire time.Time) error {
	return repo.сreateSession(ctx, repo.pool, refreshHash, refreshCreate, refreshExpire)
}

func (repo *AuthRepo) CreateSessionTx(ctx context.Context, tx pgx.Tx,  refreshHash string, refreshCreate, refreshExpire time.Time) error {
	return repo.сreateSession(ctx, tx, refreshHash, refreshCreate, refreshExpire)
}