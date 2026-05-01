package service

import (
	"accelerator/internal/core/config"
	"accelerator/internal/core/error_type"
	"accelerator/internal/domains"
	"accelerator/internal/features/auth/repository"
	"accelerator/internal/tools"
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type AuthService struct {
	repo *repository.AuthRepo
	cfg  *config.Config
}

func NewAuthService(repo *repository.AuthRepo, cfg *config.Config) *AuthService {
	return &AuthService{
		repo: repo,
		cfg:  cfg,
	}
}

// получает почту и пароль
// генерирует хэш, сохраняет пользователя, создает сессию, обрабатывает ошибки
// возвращает токены и время жизни
func (serv *AuthService) RegisterUserService(
	ctx context.Context,
	email string,
	password string,
) (*domains.ReturnCreateTokensInfo, error) {
	// ошибки от репозитория обрабатываются в нем же через стандартизированные ошибки и сразу передаются на уровень выше
	// остальные ошибки из сторонных файлов обрабатываются уже здесь

	// нужно реализовать механизм транзакции, чтобы не было сценария, когда на одном из этапов произошла ошибка, а данные в бд уже изменены
	// и например у нас получается зарегистированный пользователь без сессии, которому потом надо логиниться чтобы получить токены
	tx, err := serv.repo.BeginTx(ctx, pgx.TxOptions{}) // создаем транзакцию
	if err != nil {
		return nil, error_type.NewInternal(fmt.Errorf("begin tx: %w", err))
	}
	defer func() { // при какой либо ошибке перед завершением функции откатываем изменения базы данных
		_ = tx.Rollback(ctx)
	}()

	// -----------------------------------------> ОСНОВНАЯ ЛОГИКА <----------------------------------------------
	// 1) получаем хэш пароля
	passwordHash, err := tools.GeneratePasswordHash(password) // оборачиваем в errorf чтобы сохранить доп информацию для логов в ошибке также сохраняем возможность проверить ее тип через Is
	if err != nil {
		return nil, error_type.NewInternal(fmt.Errorf("hash password: %w", err))
	}

	// 2) создаем пользователя, получаем id
	userID, err := serv.repo.CreateUserTx(ctx, tx, email, passwordHash)
	if err != nil {
		return nil, err
	}

	// 3) генерируем jwt токены, принимаем всю информацию о них
	tokensInfo, err := tools.GenerateJWTToken(userID, serv.cfg)
	if err != nil {
		return nil, error_type.NewInternal(fmt.Errorf("generate tokens: %w", err))
	}

	// 4) получаем хэш refresh токена для хранения в базе данных сессий пользователя
	refreshTokenHash := tools.GenerateTokenHash(tokensInfo.RefreshToken)


	// 5) создаем сессию пользователя через транзакцию
	err = serv.repo.CreateSessionTx(
		ctx, 
		tx, 
		userID, 
		refreshTokenHash, 
		tokensInfo.RefreshJTI, 
		tokensInfo.CreateTime, 
		tokensInfo.RefreshExpireTime,
	)
	if err != nil {
		return nil, err
	}
	// --------------------------------------------------------------------------------------------

	// коммитим изменения, если мы не вышли на предыдущем моменте,
	// также обрабатываем саму ошибку при неудачном коммите
	if err := tx.Commit(ctx); err != nil {
		return nil, error_type.NewInternal(fmt.Errorf("commit tx: %w", err))
	}

	// получаем нужную информацию из модели создания токена и возвращаем токены на клиент
	return tokensInfo, nil

}

// принимает почту и пароль
// проверяет, есть ли пользователь с такими данными в таблице users, создает новую сессию
// возвращает ошибки с разных уровней и информацию о токенах
func (serv *AuthService) LoginUserService(ctx context.Context, email, password string) (*domains.ReturnCreateTokensInfo, error) {
	// 1) получаем хэш пароля для дальнейшей проверки и ID пользователя для генерации токенов
	userAuthInfo, err := serv.repo.GetAuthCredentials(ctx, email)
	if err != nil {
		return nil, err
	}

	// 2) проверяем хэш переданного пароля с той же солью, что и в полученном userAuthInfo.PasswordHash
	isComparePasswords := tools.ComparePasswordHash(userAuthInfo.PasswordHash, password)
	if !isComparePasswords { // если не совпадают, возвращаем ошибку
		return nil, error_type.NewUnauthorized("Invalid login or password")
	}

	// 3) генерируем jwt токены, принимаем всю информацию о них
	tokensInfo, err := tools.GenerateJWTToken(userAuthInfo.ID, serv.cfg)
	if err != nil {
		return nil, error_type.NewInternal(fmt.Errorf("generate tokens: %w", err))
	}

	// 4) получаем хэш refresh токена для хранения в базе данных сессий пользователя
	refreshTokenHash := tools.GenerateTokenHash(tokensInfo.RefreshToken)

	// 5) создаем сессию пользователя
	err = serv.repo.CreateSession(
		ctx, 
		userAuthInfo.ID, 
		refreshTokenHash, 
		tokensInfo.RefreshJTI, 
		tokensInfo.CreateTime, 
		tokensInfo.RefreshExpireTime,
	)
	if err != nil {
		return nil, err
	}

	return tokensInfo, nil
}

// принимает refresh токен
// проверяет его валидность, срок действия, подпись
// возвращает новую пару токенов c информацией о них
func (serv *AuthService) RefreshUserService(ctx context.Context, refreshToken string) (*domains.ReturnCreateTokensInfo, error) {
	// создаем транзакцию, так как у нас 2 операции, заблокировать предыдущую сессию и создать новую
	tx, err := serv.repo.BeginTx(ctx, pgx.TxOptions{}) // создаем транзакцию
	if err != nil {
		return nil, error_type.NewInternal(fmt.Errorf("begin tx: %w", err))
	}
	defer func() { // при какой либо ошибке перед завершением функции откатываем изменения базы данных
		_ = tx.Rollback(ctx)
	}()

	// -----------------------------------------> ОСНОВНАЯ ЛОГИКА <----------------------------------------------
	// 1) валидируем json рефреш токен, получаем id пользователя и уникальный jti токена для поиска сессии
	userIDFromToken, jti, err := tools.ParseRefreshToken(refreshToken, serv.cfg)
	if err != nil {
		return nil, error_type.NewUnauthorized("Invalid token")
	}

	// 2) получаем сессию по jti
	sessionInfo, err := serv.repo.GetSessionByJTI(ctx, jti)
	if err != nil {
		return nil, err
	}

	// 3) Проверяем, что сессия активна
	if sessionInfo.RevokedAt != nil {
		return nil, error_type.NewUnauthorized("Token revoked")
	}



	// 4????) Проверяем, что токен еще не истек, спользуя информацию из бд
	// хз на всякий случай, вроде сюда оно никогда не дойдет, но на всякий случай пусть будет
	if sessionInfo.ExpiresAt.Before(time.Now()) {
		return nil, error_type.NewUnauthorized("Token expired")
	}
	// 4) Сравниваем userID из токена с userID из сессии
	// тоже при каких-то случаях лучше проверять, но возможно избыточно, будет как доп уровень защиты
	if sessionInfo.UserID != userIDFromToken {
		// возможно, подмена. Отзываем все сессии пользователя
		_ = serv.repo.RevokeAllUserSessions(ctx, sessionInfo.UserID)
		return nil, error_type.NewUnauthorized("Invalid token")
	}
	// 4?????) ну и напоследок разгоняем еще больше защиты, проверю хеши токенов, пусть будут
	if !tools.CompareTokenHash(sessionInfo.TokenHash, refreshToken) {
		// Отзываем все сессии пользователя
		_ = serv.repo.RevokeAllUserSessions(ctx, sessionInfo.UserID)
		return nil, error_type.NewUnauthorized("Invalid token")
	}




	// 5) отзываем старую сессию через транзакцию
	if err := serv.repo.RevokeSessionTx(ctx, tx, jti); err != nil {
		return nil, err
	}

	// 6) генерируем jwt токены, принимаем всю информацию о них, используя userID
	tokensInfo, err := tools.GenerateJWTToken(sessionInfo.UserID, serv.cfg)
	if err != nil {
		return nil, error_type.NewInternal(fmt.Errorf("generate tokens: %w", err))
	}

	// 7) получаем хэш refresh токена для хранения в базе данных сессий пользователя
	refreshTokenHash := tools.GenerateTokenHash(tokensInfo.RefreshToken)


	// 8) создаем новую сессию с новым рефреш токеном
	err = serv.repo.CreateSessionTx(
		ctx, 
		tx, 
		sessionInfo.UserID, 
		refreshTokenHash, 
		tokensInfo.RefreshJTI, 
		tokensInfo.CreateTime, 
		tokensInfo.RefreshExpireTime,
	)
	if err != nil {
		return nil, err
	}
	// --------------------------------------------------------------------------------------------

	if err := tx.Commit(ctx); err != nil {
		return nil, error_type.NewInternal(fmt.Errorf("commit tx: %w", err))
	}

	return tokensInfo, nil
}
